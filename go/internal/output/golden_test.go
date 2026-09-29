package output

import (
	"testing"

	"github.com/grubless/grubless-cli/go/internal/golden"
)

// Every expectation here was produced by the TS formatters, over the TS
// tests' own inputs plus 1,500 seeded random decimals. See go/scripts/goldens.ts.
func TestFormattersMatchTS(t *testing.T) {
	var g struct {
		Money     [][2]string `json:"money"`
		Qty       [][2]string `json:"qty"`
		Signed    [][2]string `json:"signed"`
		Ellipsize []struct {
			Text  string
			Width int
			Want  string
		} `json:"-"`
		EllipsizeRaw [][3]any    `json:"ellipsize"`
		HeldInRaw    [][4]any    `json:"heldIn"`
		ShortDate    [][2]string `json:"shortDate"`
	}
	golden.Load(t, "testdata/format.json.gz", &g)

	check := func(name string, f func(*string) string, cases [][2]string) {
		for _, c := range cases {
			if got := f(Str(c[0])); got != c[1] {
				t.Errorf("%s(%q) = %q, want %q", name, c[0], got, c[1])
			}
		}
	}
	check("money", Money, g.Money)
	check("qty", Qty, g.Qty)
	check("signed", Signed, g.Signed)
	check("shortDate", ShortDate, g.ShortDate)

	for _, c := range g.EllipsizeRaw {
		text, width, want := c[0].(string), int(c[1].(float64)), c[2].(string)
		if got := Ellipsize(text, width); got != want {
			t.Errorf("ellipsize(%q, %d) = %q, want %q", text, width, got, want)
		}
	}
	for _, c := range g.HeldInRaw {
		var chain *string
		if s, ok := c[0].(string); ok {
			chain = &s
		}
		var labels []string
		for _, l := range c[1].([]any) {
			labels = append(labels, l.(string))
		}
		width, want := int(c[2].(float64)), c[3].(string)
		if got := HeldIn(chain, labels, width); got != want {
			t.Errorf("heldIn(%v, %q, %d) = %q, want %q", c[0], labels, width, got, want)
		}
	}
}

func TestAbsentValuesAreADashNotZero(t *testing.T) {
	// "No price cached yet" and "worth nothing" must not look identical.
	for name, f := range map[string]func(*string) string{"money": Money, "qty": Qty, "shortDate": ShortDate} {
		if got := f(nil); got != "—" {
			t.Errorf("%s(nil) = %q, want —", name, got)
		}
	}
}
