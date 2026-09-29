// Package output holds exit codes and formatting.
//
// Two rules drive everything here, same as src/output.ts:
//
//  1. stdout is data, stderr is commentary. `grubless holdings --json | jq`
//     has to work, so progress, warnings and errors never touch stdout.
//  2. No colour when stdout isn't a TTY.
package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

// Exit codes. Documented in README.md § Exit codes; CI pins 2 and 3.
const (
	Ok               = 0
	Failure          = 1
	UsageError       = 2
	AuthFailure      = 3
	BlockingWarnings = 4
	UpgradeRequired  = 5
)

// CliError is an error already phrased for a user — printed as-is, with no
// stack trace.
type CliError struct {
	Message  string
	ExitCode int
}

func (e *CliError) Error() string { return e.Message }

func Errorf(code int, format string, a ...any) *CliError {
	return &CliError{Message: fmt.Sprintf(format, a...), ExitCode: code}
}

// IsTerminal reports whether f is a character device.
//
// Good enough on Unix without golang.org/x/term. It is NOT right on Windows,
// where NUL is also a character device — the full port would use x/term.
func IsTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// UseColour is rule 2 as a value. `NO_COLOR=` (empty) leaves colour on,
// matching `!process.env.NO_COLOR` in the TS.
var UseColour = IsTerminal(os.Stdout) && os.Getenv("NO_COLOR") == ""

func paint(code, text string) string {
	if !UseColour {
		return text
	}
	return code + text + "\x1b[0m"
}

func Dim(t string) string    { return paint("\x1b[2m", t) }
func Bold(t string) string   { return paint("\x1b[1m", t) }
func Red(t string) string    { return paint("\x1b[31m", t) }
func Yellow(t string) string { return paint("\x1b[33m", t) }
func Green(t string) string  { return paint("\x1b[32m", t) }

// Out writes data. Always stdout.
func Out(line string) { fmt.Fprintln(os.Stdout, line) }

// Note writes commentary. Always stderr.
func Note(line string) { fmt.Fprintln(os.Stderr, line) }

// RawJSON pretty-prints a JSON document without decoding it into a struct.
//
// This is the Go equivalent of `JSON.stringify(parsed, null, 2)` for data the
// server sent: decoding into a struct and re-encoding would silently drop any
// field this client doesn't declare and reorder the rest, so `--json` would
// stop being the server's own shape. Indenting the raw bytes keeps both.
func RawJSON(raw []byte) error {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return err
	}
	Out(buf.String())
	return nil
}

// JSON encodes a value this client built itself (not a server passthrough).
func JSON(v any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	// Go escapes <, > and & as < etc. by default, for embedding in HTML.
	// JSON.stringify doesn't, and a CLI has no HTML to protect.
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return err
	}
	fmt.Fprint(os.Stdout, buf.String())
	return nil
}

// Column is one column of a Table.
type Column[T any] struct {
	Header     string
	Value      func(T) string
	AlignRight bool
	// MaxWidth caps the column, ellipsizing anything longer. 0 means no cap.
	MaxWidth int
}

// width counts runes, not bytes — len() on a Go string is bytes, and a single
// "é" would push a column out by one. (The TS counts UTF-16 units; the two
// agree everywhere except astral characters like emoji.)
func width(s string) int { return utf8.RuneCountInString(s) }

// Table prints plain aligned columns, not box-drawing, so it can be piped into
// grep or awk.
func Table[T any](rows []T, columns []Column[T]) {
	if len(rows) == 0 {
		return
	}
	cells := make([][]string, len(rows))
	for r, row := range rows {
		cells[r] = make([]string, len(columns))
		for i, c := range columns {
			v := c.Value(row)
			if c.MaxWidth > 0 {
				v = Ellipsize(v, c.MaxWidth)
			}
			cells[r][i] = v
		}
	}
	widths := make([]int, len(columns))
	for i, c := range columns {
		w := width(c.Header)
		for _, row := range cells {
			w = max(w, width(row[i]))
		}
		if c.MaxWidth > 0 {
			w = min(w, c.MaxWidth)
		}
		widths[i] = w
	}

	line := func(texts []string) string {
		parts := make([]string, len(texts))
		for i, t := range texts {
			gap := strings.Repeat(" ", max(0, widths[i]-width(t)))
			if columns[i].AlignRight {
				parts[i] = gap + t
			} else {
				parts[i] = t + gap
			}
		}
		return strings.Join(parts, "  ")
	}

	headers := make([]string, len(columns))
	for i, c := range columns {
		headers[i] = c.Header
	}
	Out(Dim(line(headers)))
	for _, row := range cells {
		Out(line(row))
	}
}

// Ellipsize hard-truncates plain text so a following column stays aligned.
func Ellipsize(text string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxWidth {
		return text
	}
	return string(runes[:maxWidth-1]) + "…"
}
