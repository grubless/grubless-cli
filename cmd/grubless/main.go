// Command grubless is the Go port of the Grubless CLI.
//
// Dispatch order is pinned by the scenario tests (internal/scenarios): the
// credential is resolved BEFORE the command, so an unknown command while
// signed out exits 3, not 2.
package main

import (
	"errors"
	"fmt"
	"math"
	"os"
	"runtime/debug"
	"strings"

	"github.com/grubless/grubless-cli/internal/args"
	"github.com/grubless/grubless-cli/internal/client"
	"github.com/grubless/grubless-cli/internal/commands"
	"github.com/grubless/grubless-cli/internal/config"
	"github.com/grubless/grubless-cli/internal/jsstr"
	"github.com/grubless/grubless-cli/internal/output"
	"github.com/grubless/grubless-cli/internal/tui"
)

var reports = commands.Reports

const usageFormat = `grubless %s — crypto tax for entities

USAGE
  grubless <command> [options]

COMMANDS
  auth login [--token <t>]      Authenticate with an API token
  auth logout                   Forget the stored token
  auth whoami                   Show the current account and its entities

  entities list                 List entities this account can reach

  sources list                  List an entity's sources
  sources sync                  Queue a sync (--source <id> | --all)
  import <file.csv>             Upload a CSV into a csv_import source

  holdings                      Current positions, with reconciliation flags
  portfolio                     Value over time (a chart, or --json for the series)
  tax-summary                   Per-financial-year tax position
  warnings                      Data-quality issues blocking a clean filing

  transactions                  An entity's transactions, filtered, newest first

  report <name>                 Download a report (see REPORTS below)

COMMON OPTIONS
  --entity <id|name>            Target entity; name may be an unambiguous prefix
  --all-entities                Every entity this account can reach
  --json                        Machine-readable output on stdout
  --api-url <url>               Override the API endpoint
  -h, --help                    Show this help
  -v, --version                 Show the version

REPORT OPTIONS
  --year <startYear>            Financial year, e.g. 2025
  --out <path|dir>              Write to a file, or a directory with --all-entities
  --json                        Rows as JSON on stdout, including --all-entities
                                (not for bundle / ato-mytax / division-70: PDF and ZIP)

SYNC / IMPORT OPTIONS
  --wait                        Block until the queued work finishes
  --full                        Re-fetch entire history, ignoring last sync
  --timeout <minutes>           How long --wait waits (default 35)

TRANSACTIONS OPTIONS
  --category <a,b,…>            Event types, e.g. transfer,send (the API lists them all on a bad one)
  --direction <in|out>          Only transactions with a leg moving this way
  --from <date>, --to <date>    ISO date or timestamp bounds
  --source <id|label>           One source
  --asset <id|symbol>           One asset; a symbol must be one the entity holds
  --search <text>               Free-text search
  --sort <asc|desc>             Oldest or newest first (default desc)
  --limit <n|all>               How many to show (default 50)

PORTFOLIO OPTIONS
  --range <key>                 24h, 1w, 1m, 3m, 6m, 1y, fy, all (default all)

WARNINGS OPTIONS
  --fail-on-blocking            Exit 4 if blocking issues exist (CI gate)

REPORTS
  %s, bundle

ENVIRONMENT
  GRUBLESS_TOKEN                API token; takes precedence over stored config
  GRUBLESS_API_URL              Default API endpoint
  GRUBLESS_THEME                Interface theme: cypher (default) or terminal; t switches it
  NO_COLOR                      Disable colour

EXIT CODES
  0 ok   1 failed   2 usage   3 auth   4 blocking warnings   5 upgrade required

EXAMPLES
  # Every client's capital gains for FY2025, one directory per entity
  grubless report capital-gains --all-entities --year 2025 --out ./clients/

  # The same data as one JSON document on stdout — for jq, or an LLM tool
  grubless report capital-gains --all-entities --year 2025 --json | jq '.[].entity.name'

  # Pre-filing gate for CI
  grubless warnings --entity "Node Integration" --fail-on-blocking

  # Sync everything and wait for it
  grubless sources sync --entity acme --all --wait
`

func usage() string {
	return fmt.Sprintf(usageFormat, client.Version, strings.Join(reports, ", "))
}

var options = map[string]args.Option{
	"entity":           {Kind: args.String},
	"all-entities":     {Kind: args.Bool},
	"source":           {Kind: args.String},
	"all":              {Kind: args.Bool},
	"full":             {Kind: args.Bool},
	"wait":             {Kind: args.Bool},
	"timeout":          {Kind: args.String},
	"year":             {Kind: args.String},
	"range":            {Kind: args.String},
	"out":              {Kind: args.String},
	"token":            {Kind: args.String},
	"api-url":          {Kind: args.String},
	"json":             {Kind: args.Bool},
	"fail-on-blocking": {Kind: args.Bool},
	"category":         {Kind: args.String},
	"direction":        {Kind: args.String},
	"from":             {Kind: args.String},
	"to":               {Kind: args.String},
	"search":           {Kind: args.String},
	"asset":            {Kind: args.String},
	"sort":             {Kind: args.String},
	"limit":            {Kind: args.String},
	"help":             {Kind: args.Bool, Short: 'h'},
	"version":          {Kind: args.Bool, Short: 'v'},
}

func run(argv []string) (int, error) {
	p, err := args.Parse(argv, options)
	if err != nil {
		return 0, output.Errorf(output.UsageError, "%v\n\nRun `grubless --help`.", err)
	}

	if p.Bools["version"] {
		output.Out(client.Version)
		return output.Ok, nil
	}
	if p.Bools["help"] {
		output.Out(usage())
		return output.Ok, nil
	}

	// No command: the TUI on a terminal, usage otherwise.
	if len(p.Positionals) == 0 && !output.IsTerminal(os.Stdin) {
		output.Note(usage())
		return output.UsageError, nil
	}

	// `values.timeout ? Number(values.timeout) : undefined`, then finite and
	// positive — JS Number() semantics, so " 5 " and "0x10" parse as they do there.
	var timeoutMinutes float64
	if t, ok := p.Str("timeout"); ok && t != "" {
		timeoutMinutes = jsstr.ParseNumber(t)
		if math.IsNaN(timeoutMinutes) || math.IsInf(timeoutMinutes, 0) || timeoutMinutes <= 0 {
			return 0, output.Errorf(output.UsageError, "--timeout must be a positive number of minutes, got \"%s\".", t)
		}
	}
	timeoutMs := commands.TimeoutMs(timeoutMinutes)

	var command, sub string
	if len(p.Positionals) > 0 {
		command = p.Positionals[0]
	}
	if len(p.Positionals) > 1 {
		sub = p.Positionals[1]
	}
	apiURL, hasAPIURL := p.Str("api-url")
	asJSON := p.Bools["json"]

	// The only commands that work without an existing credential.
	if command == "auth" && sub == "login" {
		token, _ := p.Str("token")
		return commands.AuthLogin(token, apiURL, hasAPIURL)
	}
	if command == "auth" && sub == "logout" {
		return commands.AuthLogout()
	}

	cfg := config.Load(apiURL, hasAPIURL)
	if cfg.Token == "" {
		return 0, output.Errorf(output.AuthFailure, "Not signed in.\nRun `grubless auth login`, or set GRUBLESS_TOKEN.")
	}
	c := client.New(cfg.APIURL, cfg.Token)

	if len(p.Positionals) == 0 {
		return tui.Run(c)
	}

	entity, _ := p.Str("entity")
	source, _ := p.Str("source")
	scope := commands.Scope{Entity: entity, AllEntities: p.Bools["all-entities"], JSON: asJSON}
	var rest []string
	if len(p.Positionals) > 2 {
		rest = p.Positionals[2:]
	}

	switch command {
	case "auth":
		if sub == "whoami" {
			return commands.AuthWhoami(c, asJSON)
		}
		return 0, output.Errorf(output.UsageError, "Unknown: auth %s. Try login, logout or whoami.", sub)

	case "entities":
		if sub == "" || sub == "list" {
			return commands.EntitiesList(c, asJSON)
		}
		return 0, output.Errorf(output.UsageError, "Unknown: entities %s. Only `list` exists today.", sub)

	case "sources":
		if sub == "" || sub == "list" {
			return commands.SourcesList(c, scope)
		}
		if sub == "sync" {
			return commands.SourcesSync(c, commands.SyncOptions{
				Scope:     scope,
				Source:    source,
				All:       p.Bools["all"],
				Full:      p.Bools["full"],
				Wait:      p.Bools["wait"],
				TimeoutMs: timeoutMs,
			})
		}
		return 0, output.Errorf(output.UsageError, "Unknown: sources %s. Try list or sync.", sub)

	case "import":
		if sub == "" {
			return 0, output.Errorf(output.UsageError, "Specify the file to import: `grubless import <file.csv>`.")
		}
		return commands.SourcesImport(c, sub, commands.ImportOptions{
			Entity:    entity,
			Source:    source,
			Wait:      p.Bools["wait"],
			TimeoutMs: timeoutMs,
			JSON:      asJSON,
		})

	case "holdings":
		return commands.Holdings(c, scope)

	case "portfolio":
		r, _ := p.Str("range")
		return commands.Portfolio(c, scope, r)

	case "tax-summary":
		year, _ := p.Str("year")
		return commands.TaxSummary(c, scope, year)

	case "warnings":
		return commands.Warnings(c, scope, p.Bools["fail-on-blocking"])

	case "transactions":
		if sub != "" {
			return 0, output.Errorf(output.UsageError, "Unexpected argument \"%s\". Filters are flags: see `grubless --help`.", sub)
		}
		str := func(name string) string { v, _ := p.Str(name); return v }
		return commands.Transactions(c, scope, commands.TxFilters{
			Category:  str("category"),
			Direction: str("direction"),
			From:      str("from"),
			To:        str("to"),
			Search:    str("search"),
			Source:    source,
			Asset:     str("asset"),
			Sort:      str("sort"),
			Limit:     str("limit"),
		})

	case "report":
		if sub == "" {
			return 0, output.Errorf(output.UsageError, "Specify a report: %s", strings.Join(append(append([]string{}, reports...), "bundle"), ", "))
		}
		if len(rest) > 0 {
			return 0, output.Errorf(output.UsageError, "Unexpected argument \"%s\". One report at a time.", rest[0])
		}
		year, _ := p.Str("year")
		out, _ := p.Str("out")
		return commands.Report(c, sub, commands.ReportOptions{Scope: scope, Year: year, Out: out})
	}
	return 0, output.Errorf(output.UsageError, "Unknown command \"%s\".\n\nRun `grubless --help`.", command)
}

func main() {
	os.Exit(exitCode())
}

func exitCode() (code int) {
	// A Go panic exits 2 by default — which is this CLI's *usage error* code.
	// A crash that tells CI "you passed a bad flag" is exactly the kind of
	// lie README § Exit codes exists to prevent, so map it to 1.
	defer func() {
		if r := recover(); r != nil {
			output.Note(fmt.Sprintf("panic: %v\n%s", r, debug.Stack()))
			code = output.Failure
		}
	}()

	code, err := run(os.Args[1:])
	if err == nil {
		return code
	}
	var cliErr *output.CliError
	if errors.As(err, &cliErr) {
		// Already phrased for a human — no stack trace.
		output.Note(cliErr.Message)
		return cliErr.ExitCode
	}
	output.Note(err.Error())
	return output.Failure
}
