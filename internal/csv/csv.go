// Package csv is a minimal RFC 4180 reader whose only job is to re-frame a
// report the server already produced — CSV in, the same strings out as JSON.
// Nothing here computes, rounds or coerces.
//
// Not encoding/csv: that rejects a bare quote mid-field and normalises
// `\r\n` inside quoted fields to `\n`, and this must be the exact inverse of
// the server's own quoting, byte for byte, as the TS reader is.
package csv

import (
	"strings"

	"github.com/grubless/grubless-cli/internal/jsonv"
	"github.com/grubless/grubless-cli/internal/jsstr"
)

// Parse splits CSV text into rows of raw fields: quoted commas, doubled
// quotes and newlines inside quotes all handled, CR dropped outside quotes.
func Parse(text string) [][]string {
	var rows [][]string
	var row []string
	var field strings.Builder
	quoted := false
	// Whether anything has been seen on this line — tells a trailing newline
	// apart from a genuine empty final field.
	started := false

	for i := 0; i < len(text); i++ {
		c := text[i]

		if quoted {
			if c == '"' {
				if i+1 < len(text) && text[i+1] == '"' {
					field.WriteByte('"')
					i++
				} else {
					quoted = false
				}
			} else {
				field.WriteByte(c)
			}
			continue
		}

		switch {
		case c == '"' && field.Len() == 0:
			quoted = true
			started = true
		case c == ',':
			row = append(row, field.String())
			field.Reset()
			started = true
		case c == '\r':
			// CRLF: the \n does the work.
		case c == '\n':
			if started || field.Len() > 0 || len(row) > 0 {
				rows = append(rows, append(row, field.String()))
			}
			row = nil
			field.Reset()
			started = false
		default:
			field.WriteByte(c)
			started = true
		}
	}
	if started || field.Len() > 0 || len(row) > 0 {
		rows = append(rows, append(row, field.String()))
	}
	return rows
}

// Table is a report re-framed: its columns, its rows keyed by column, and
// any prose lines ("No cached tax summary found…") that aren't rows.
type Table struct {
	Columns []string
	Rows    []*jsonv.Object
	Notes   []string
}

// ToTable keys data rows by header name. A surplus field is kept under
// `_extra` rather than dropped: this is someone's tax export.
func ToTable(text string) Table {
	raw := Parse(text)
	if len(raw) == 0 {
		return Table{Columns: []string{}, Rows: []*jsonv.Object{}, Notes: []string{}}
	}
	columns, body := raw[0], raw[1:]
	t := Table{Columns: columns, Rows: []*jsonv.Object{}, Notes: []string{}}

	for _, fields := range body {
		// A single-field line in a multi-column table is prose, not data.
		if len(columns) > 1 && len(fields) == 1 {
			if jsstr.Trim(fields[0]) != "" {
				t.Notes = append(t.Notes, fields[0])
			}
			continue
		}
		row := jsonv.NewObject()
		for i, column := range columns {
			value := ""
			if i < len(fields) {
				value = fields[i]
			}
			row.Set(column, value)
		}
		if len(fields) > len(columns) {
			extra := make([]jsonv.Value, 0, len(fields)-len(columns))
			for _, f := range fields[len(columns):] {
				extra = append(extra, f)
			}
			row.Set("_extra", extra)
		}
		t.Rows = append(t.Rows, row)
	}
	return t
}
