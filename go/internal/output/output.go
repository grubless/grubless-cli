// Package output holds exit codes and formatting.
//
// Two rules drive everything here, same as src/output.ts:
//
//  1. stdout is data, stderr is commentary. `grubless holdings --json | jq`
//     has to work, so progress, warnings and errors never touch stdout.
//  2. No colour when stdout isn't a TTY.
package output

import (
	"fmt"
	"os"
	"strings"

	"github.com/grubless/grubless-cli/go/internal/jsonv"
	"github.com/grubless/grubless-cli/go/internal/jsstr"
	"golang.org/x/term"
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

// IsTerminal is `stream.isTTY`.
func IsTerminal(f *os.File) bool { return term.IsTerminal(int(f.Fd())) }

// Columns is `process.stdout.columns`: the terminal's width, or ok=false when
// stdout isn't a terminal (where Node leaves it undefined).
func Columns() (int, bool) {
	if !IsTerminal(os.Stdout) {
		return 0, false
	}
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	return w, err == nil
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

// JSON prints a value exactly as `JSON.stringify(value, null, 2)` would.
// See internal/jsonv for why that can't be encoding/json.
func JSON(v jsonv.Value) { Out(jsonv.Stringify(v)) }

// Column is one column of a Table.
type Column[T any] struct {
	Header     string
	Value      func(T) string
	AlignRight bool
	// MaxWidth caps the column, ellipsizing anything longer. 0 means no cap.
	MaxWidth int
}

// width is JS `.length` — UTF-16 units, escape codes included, exactly as
// the TS measures a cell. len() on a Go string would count bytes, and a
// single "é" would push a column out by one.
func width(s string) int { return jsstr.Len(s) }

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
			if columns[i].AlignRight {
				parts[i] = jsstr.PadStart(t, widths[i])
			} else {
				parts[i] = jsstr.PadEnd(t, widths[i])
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
