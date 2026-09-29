package timerange

import (
	"slices"
	"testing"
	"time"

	"github.com/grubless/grubless-cli/go/internal/golden"
)

// Expectations produced by src/range.ts. See go/scripts/goldens.ts.
func TestMatchesTS(t *testing.T) {
	var cases []struct {
		Now     string   `json:"now"`
		Month   int      `json:"month"`
		Range   Key      `json:"range"`
		Start   *string  `json:"start"`
		FYStart string   `json:"fyStart"`
		Points  []string `json:"points"`
	}
	golden.Load(t, "testdata/range.json.gz", &cases)

	var series []string
	for i := 0; i < 60; i++ {
		series = append(series, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(i*17*24)*time.Hour).Format("2006-01-02"))
	}
	iso := func(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

	for _, c := range cases {
		now, _ := time.Parse(time.RFC3339Nano, c.Now)
		start, ok := StartDate(c.Range, c.Month, now)
		switch {
		case c.Start == nil && ok:
			t.Errorf("%s %d %s: start = %s, want none", c.Now, c.Month, c.Range, iso(start))
		case c.Start != nil && (!ok || iso(start) != *c.Start):
			t.Errorf("%s %d %s: start = %s, want %s", c.Now, c.Month, c.Range, iso(start), *c.Start)
		}
		if got := iso(CurrentFinancialYearStart(c.Month, now)); got != c.FYStart {
			t.Errorf("%s %d: fy start = %s, want %s", c.Now, c.Month, got, c.FYStart)
		}
		got := Filter(series, func(s string) string { return s }, c.Range, c.Month, now)
		if !slices.Equal(got, c.Points) && !(len(got) == 0 && len(c.Points) == 0) {
			t.Errorf("%s %d %s: points = %v, want %v", c.Now, c.Month, c.Range, got, c.Points)
		}
	}
}
