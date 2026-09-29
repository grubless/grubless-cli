package csv

import (
	"reflect"
	"testing"

	"github.com/grubless/grubless-cli/internal/jsonv"
)

// A port of src/test/csv.test.ts: this reader must be the exact inverse of
// the server's own quoting, since it re-frames tax exports.

func TestParse(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want [][]string
	}{
		{"plain CRLF", "a,b\r\n1,2\r\n", [][]string{{"a", "b"}, {"1", "2"}}},
		{"LF only", "a,b\n1,2\n", [][]string{{"a", "b"}, {"1", "2"}}},
		// A split(",") would shear this row, shifting every later column.
		{"quoted comma", "name,amount\r\n\"Smith, Robert\",1000\r\n", [][]string{{"name", "amount"}, {"Smith, Robert", "1000"}}},
		{"doubled quote", "note\r\n\"he said \"\"no\"\"\"\r\n", [][]string{{"note"}, {`he said "no"`}}},
		{"newline in quotes", "note,amount\r\n\"line one\nline two\",5\r\n", [][]string{{"note", "amount"}, {"line one\nline two", "5"}}},
		{"empty fields, including trailing", "a,b,c\r\n1,,\r\n", [][]string{{"a", "b", "c"}, {"1", "", ""}}},
		{"no row from the trailing newline", "a\r\n1\r\n", [][]string{{"a"}, {"1"}}},
		{"empty body", "", nil},
	}
	for _, c := range cases {
		if got := Parse(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: Parse(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func row(t *testing.T, o *jsonv.Object) string { t.Helper(); return jsonv.Stringify(o) }

func TestToTable(t *testing.T) {
	table := ToTable("Asset,Proceeds\r\nBTC,1234.56\r\nETH,7.89\r\n")
	if !reflect.DeepEqual(table.Columns, []string{"Asset", "Proceeds"}) || len(table.Rows) != 2 {
		t.Fatalf("columns %q, %d rows", table.Columns, len(table.Rows))
	}
	if v, _ := table.Rows[0].Str("Asset"); v != "BTC" {
		t.Errorf("rows keyed by column: got %s", row(t, table.Rows[0]))
	}

	// The server's exact string — never through a number.
	table = ToTable("Asset,Proceeds\r\nBTC,0.000000010000000001\r\n")
	if v, _ := table.Rows[0].Str("Proceeds"); v != "0.000000010000000001" {
		t.Errorf("amount = %q", v)
	}

	// A prose line is surfaced, not dropped: swallowing it would turn "we
	// couldn't compute this" into what looks like a clean, empty year.
	table = ToTable("Field,Value\r\nNo cached tax summary found for this financial year.\r\n")
	if len(table.Rows) != 0 || !reflect.DeepEqual(table.Notes, []string{"No cached tax summary found for this financial year."}) {
		t.Errorf("prose: rows %d, notes %q", len(table.Rows), table.Notes)
	}

	// A surplus field is kept, not discarded: this is someone's tax export.
	if got := row(t, ToTable("a,b\r\n1,2,3\r\n").Rows[0]); got != "{\n  \"a\": \"1\",\n  \"b\": \"2\",\n  \"_extra\": [\n    \"3\"\n  ]\n}" {
		t.Errorf("surplus = %s", got)
	}
	// A short row is filled, not shifted.
	if got := row(t, ToTable("a,b,c\r\n1,2\r\n").Rows[0]); got != "{\n  \"a\": \"1\",\n  \"b\": \"2\",\n  \"c\": \"\"\n}" {
		t.Errorf("short = %s", got)
	}

	if table := ToTable(""); len(table.Columns)+len(table.Rows)+len(table.Notes) != 0 {
		t.Error("an empty body should give an empty table")
	}
	// A clean FY: headers, no rows — the columns still say what the report is.
	if table := ToTable("Asset,Proceeds,Cost Basis\r\n"); len(table.Columns) != 3 || len(table.Rows) != 0 {
		t.Errorf("header only: %q, %d rows", table.Columns, len(table.Rows))
	}
}
