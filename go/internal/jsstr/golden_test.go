package jsstr

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/grubless/grubless-cli/go/internal/golden"
)

// Every expectation here was produced by Node. See go/scripts/goldens.ts.
func TestNumbersMatchNode(t *testing.T) {
	var g struct {
		String  [][2]json.RawMessage `json:"string"`
		ToFixed [][3]json.RawMessage `json:"toFixed"`
		Round   [][2]json.RawMessage `json:"round"`
		Parse   [][2]json.RawMessage `json:"parse"`
	}
	golden.Load(t, "testdata/numbers.json.gz", &g)

	num := func(raw json.RawMessage) float64 {
		var n golden.Num
		_ = json.Unmarshal(raw, &n)
		if n.IsNaN {
			return math.NaN()
		}
		return n.V
	}
	str := func(raw json.RawMessage) string {
		var s string
		_ = json.Unmarshal(raw, &s)
		return s
	}

	for _, c := range g.String {
		if got, want := Number(num(c[0])), str(c[1]); got != want {
			t.Errorf("String(%s) = %q, want %q", c[0], got, want)
		}
	}
	for _, c := range g.ToFixed {
		var digits int
		_ = json.Unmarshal(c[1], &digits)
		if got, want := ToFixed(num(c[0]), digits), str(c[2]); got != want {
			t.Errorf("(%s).toFixed(%d) = %q, want %q", c[0], digits, got, want)
		}
	}
	for _, c := range g.Round {
		if got, want := Round(num(c[0])), num(c[1]); got != want {
			t.Errorf("Math.round(%s) = %v, want %v", c[0], got, want)
		}
	}
	for _, c := range g.Parse {
		want := num(c[1])
		switch str(c[1]) {
		case "Infinity":
			want = math.Inf(1)
		case "-Infinity":
			want = math.Inf(-1)
		case "NaN":
			want = math.NaN()
		}
		got := ParseNumber(str(c[0]))
		if !(got == want || (math.IsNaN(got) && math.IsNaN(want))) {
			t.Errorf("Number(%s) = %v, want %v", c[0], got, want)
		}
	}
}
