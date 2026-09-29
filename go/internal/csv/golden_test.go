package csv

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/grubless/grubless-cli/go/internal/golden"
	"github.com/grubless/grubless-cli/go/internal/jsonv"
)

// Expectations produced by src/csv.ts. See go/scripts/goldens.ts.
func TestMatchesTS(t *testing.T) {
	var cases [][3]json.RawMessage
	golden.Load(t, "testdata/csv.json.gz", &cases)

	for _, c := range cases {
		var text, wantTable string
		var wantRows [][]string
		_ = json.Unmarshal(c[0], &text)
		_ = json.Unmarshal(c[1], &wantRows)
		_ = json.Unmarshal(c[2], &wantTable)

		if got := Parse(text); !reflect.DeepEqual(nonNil(got), nonNil(wantRows)) {
			t.Errorf("Parse(%q)\n got: %q\nwant: %q", text, got, wantRows)
		}
		table := ToTable(text)
		columns := make([]jsonv.Value, len(table.Columns))
		for i, col := range table.Columns {
			columns[i] = col
		}
		rows := make([]jsonv.Value, len(table.Rows))
		for i, r := range table.Rows {
			rows[i] = r
		}
		got := jsonv.Stringify(jsonv.Obj("columns", columns, "rows", rows, "notes", table.Notes))
		if got != wantTable {
			t.Errorf("ToTable(%q)\n got: %s\nwant: %s", text, got, wantTable)
		}
	}
}

func nonNil(rows [][]string) [][]string {
	if rows == nil {
		return [][]string{}
	}
	return rows
}
