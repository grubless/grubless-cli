package commands

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/grubless/grubless-cli/go/internal/api"
	"github.com/grubless/grubless-cli/go/internal/client"
	"github.com/grubless/grubless-cli/go/internal/csv"
	"github.com/grubless/grubless-cli/go/internal/jsonv"
	"github.com/grubless/grubless-cli/go/internal/jsstr"
	"github.com/grubless/grubless-cli/go/internal/output"
)

// `grubless report …` — the command the CLI exists for. See
// src/commands/reports.ts for the reasoning behind each rule below; the
// rules themselves are unchanged.

// Reports mirrors the report routes, 1:1.
var Reports = []string{
	"capital-gains", "income", "fees", "expenses", "buy-sell",
	"gifts-donations-lost", "other-gains", "transaction-history",
	"balances-per-source", "beginning-of-year-holdings", "end-of-year-holdings",
	"highest-balance", "division-70-trading-stock", "ato-mytax",
}

// bundle is the ZIP of every other report, built server-side.
const bundle = "bundle"

// yearless reports aren't year-scoped; the API takes no ?year= for them.
var yearless = map[string]bool{"balances-per-source": true}

// notTabular reports are rendered documents — two PDFs and the ZIP — with no
// rows for --json to re-frame.
var notTabular = map[string]bool{bundle: true, "ato-mytax": true, "division-70-trading-stock": true}

func reportPath(entityID, name, year string) string {
	route := name
	if name == bundle {
		route = "complete-tax"
	}
	qs := ""
	if !yearless[name] && year != "" {
		// encodeURIComponent; a year is digits in practice, but a flag value
		// lands in a URL here, so it's escaped regardless.
		qs = "?year=" + encodeURIComponent(year)
	}
	return "/entities/" + entityID + "/reports/" + route + qs
}

type ReportOptions struct {
	Scope
	Year string
	Out  string
}

func Report(c *client.Client, name string, o ReportOptions) (int, error) {
	if name != bundle && !slices.Contains(Reports, name) {
		return 0, output.Errorf(output.UsageError, "Unknown report \"%s\".\nAvailable: %s", name, strings.Join(append(append([]string{}, Reports...), bundle), ", "))
	}
	if o.Year == "" && !yearless[name] {
		return 0, output.Errorf(output.UsageError, "Specify --year <startYear>, e.g. --year 2025.")
	}
	if o.JSON && notTabular[name] {
		kind := "rendered PDF"
		if name == bundle {
			kind = "ZIP archive"
		}
		return 0, output.Errorf(output.UsageError,
			"\"%s\" is a %s, so there are no rows to emit as JSON.\nWrite it to a file instead: --out <path>.", name, kind)
	}
	if o.JSON && o.Out != "" {
		return 0, output.Errorf(output.UsageError, "--json writes to stdout; drop --out, or redirect it.")
	}
	// Keyed on the FLAG, not the entity count, so the same command behaves the
	// same for a firm's first client and its fortieth.
	if o.AllEntities && o.Out == "" && !o.JSON {
		return 0, output.Errorf(output.UsageError,
			"--all-entities needs somewhere to put the results: --out <dir> to write files, or --json for one document on stdout.")
	}

	entities, err := ResolveScope(c, o.Scope)
	if err != nil {
		return 0, err
	}

	var documents []jsonv.Value
	failures := 0
	for _, entity := range entities {
		var err error
		if o.JSON {
			var doc jsonv.Value
			doc, err = readOne(c, entity, name, o.Year)
			if err == nil {
				documents = append(documents, doc)
			}
		} else {
			err = writeOne(c, entity, name, o, o.AllEntities)
		}
		if err != nil {
			// One client's report failing must not abandon the other 39 —
			// except billing, which will fail every one of them identically.
			var cliErr *output.CliError
			if errors.As(err, &cliErr) && cliErr.ExitCode == output.UpgradeRequired {
				return 0, err
			}
			failures++
			output.Note(output.Red("✗") + " " + entity.Name + ": " + err.Error())
		}
	}

	// Printed even after failures, so a partial run still yields its data;
	// the exit code and stderr say it was partial.
	if o.JSON {
		output.JSON(pick(o.AllEntities, documents))
	}
	if failures > 0 {
		output.Note(output.Red(fmt.Sprintf("%d of %d entities failed.", failures, len(entities))))
		return output.Failure, nil
	}
	return output.Ok, nil
}

// readOne fetches one entity's report and re-frames it as rows.
func readOne(c *client.Client, entity api.Entity, name, year string) (jsonv.Value, error) {
	text, err := c.GetText(reportPath(entity.ID, name, year))
	if err != nil {
		return nil, err
	}
	table := csv.ToTable(text)

	columns := make([]jsonv.Value, len(table.Columns))
	for i, col := range table.Columns {
		columns[i] = col
	}
	rows := make([]jsonv.Value, len(table.Rows))
	for i, r := range table.Rows {
		rows[i] = r
	}
	var yearValue jsonv.Value
	if !yearless[name] && year != "" {
		yearValue = year
	}
	doc := jsonv.Obj(
		"entity", entityRef(entity),
		"report", name,
		"year", yearValue,
		"columns", columns,
		"rows", rows,
	)
	if len(table.Notes) > 0 {
		doc.Set("notes", table.Notes)
	}
	return doc, nil
}

func writeOne(c *client.Client, entity api.Entity, name string, o ReportOptions, multi bool) error {
	dl, err := c.GetStream(reportPath(entity.ID, name, o.Year))
	if err != nil {
		return err
	}
	defer dl.Body.Close()

	// No --out: straight to stdout, so `> x.csv` and pipes both work.
	if o.Out == "" {
		_, err := io.Copy(os.Stdout, dl.Body)
		return err
	}

	// A directory per entity under --all-entities, otherwise the literal path.
	// The server's filename already encodes the FY the report was built for,
	// so it's used — but only as a name, never as a path: see safeFilename.
	target := o.Out
	if multi {
		dir := filepath.Join(o.Out, safeDirName(entity.Name))
		file := defaultFilename(name, o.Year)
		if dl.Filename != "" {
			safe, ok := safeFilename(dl.Filename)
			switch {
			case !ok:
				output.Note(output.Yellow("!") + " " + entity.Name + ": ignored an unusable filename from the server (" + jsonv.Stringify(dl.Filename) + "); using " + file)
			default:
				// A path in it is dropped either way, but a server naming
				// somewhere outside --out is worth someone knowing about.
				if safe != dl.Filename {
					output.Note(output.Yellow("!") + " " + entity.Name + ": the server's filename had a path in it (" + jsonv.Stringify(dl.Filename) + "); saved as " + safe)
				}
				file = safe
			}
		}
		target = filepath.Join(dir, file)
		// The two guards above should make this unreachable. It stays
		// because a write landing outside --out is invisible until it has
		// overwritten something.
		if !isInside(o.Out, target) {
			return output.Errorf(output.Failure, "Refusing to write %s: it is outside %s.", target, o.Out)
		}
		if err := os.MkdirAll(dir, 0o777); err != nil {
			return errors.New(output.NodeFSError(err, "mkdir", dir))
		}
	}

	f, err := os.Create(target)
	if err != nil {
		return errors.New(output.NodeFSError(err, "open", target))
	}
	if _, err := io.Copy(f, dl.Body); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	output.Note(output.Green("✓") + " " + entity.Name + " → " + target)
	return nil
}

func defaultFilename(name, year string) string {
	ext := "csv"
	if name == bundle {
		ext = "zip"
	}
	if year != "" {
		return name + "-" + year + "." + ext
	}
	return name + "." + ext
}

var (
	unsafeRun   = regexp.MustCompile(`[^\w.-]+`)
	edgeHyphens = regexp.MustCompile(`^-+|-+$`)
)

// safeDirName keeps a user-supplied entity name from creating surprise
// nesting ("Smith & Co (Trust) / 2025") or failing outright.
//
// A name of only dots is refused too: "." and ".." survive the character
// filter, and ".." as a directory is the parent of --out. Entity names are
// chosen by whoever created the entity, which for a shared one isn't the
// person running the command.
func safeDirName(name string) string {
	s := edgeHyphens.ReplaceAllString(unsafeRun.ReplaceAllString(name, "-"), "")
	if s == "" || allDots.MatchString(s) {
		return "entity"
	}
	return s
}

var (
	allDots      = regexp.MustCompile(`^\.+$`)
	controlChars = regexp.MustCompile(`[\x00-\x1f\x7f]`)
)

// safeFilename reduces the server's suggested filename to a bare file name,
// or reports that nothing usable is left.
//
// It arrives in a Content-Disposition header, URL-decoded, and was once
// joined onto the output directory as-is: "../../.bashrc", or
// "..%2F..%2F.bashrc" once decoded, or "..\\x" on Windows, wrote outside
// --out. Only the last path component is kept, and a name that's empty, all
// dots, or has control characters in it is refused.
func safeFilename(filename string) (string, bool) {
	// Everything after the last separator of either kind; empty if the
	// name ends in one.
	base := filename[strings.LastIndexAny(filename, `/\`)+1:]
	if jsstr.Trim(base) == "" || allDots.MatchString(base) || controlChars.MatchString(base) {
		return "", false
	}
	return base, true
}

// isInside is whether target resolves to somewhere under dir.
func isInside(dir, target string) bool {
	absDir, err1 := filepath.Abs(dir)
	absTarget, err2 := filepath.Abs(target)
	if err1 != nil || err2 != nil {
		return false
	}
	rel, err := filepath.Rel(absDir, absTarget)
	if err != nil {
		return false
	}
	// ".." itself or ".." then a separator — not a name that happens to
	// start with two dots, like an entity called "..Holdings".
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// encodeURIComponent: everything but A–Z a–z 0–9 - _ . ! ~ * ' ( ) escaped
// as UTF-8 percent-encoding.
func encodeURIComponent(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || strings.IndexByte("-_.!~*'()", c) >= 0 {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}
