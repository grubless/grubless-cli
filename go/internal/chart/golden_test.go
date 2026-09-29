package chart

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/grubless/grubless-cli/go/internal/api"
	"github.com/grubless/grubless-cli/go/internal/golden"
)

// Expectations produced by src/tui/chart.ts over its own tests' series plus
// 150 seeded random ones, at random sizes. See go/scripts/goldens.ts.
func TestMatchesTS(t *testing.T) {
	var g struct {
		NiceCeil   [][2]float64         `json:"niceCeil"`
		CompactRaw [][2]json.RawMessage `json:"compactMoney"`
		Resample   [][3]json.RawMessage `json:"resample"`
		Render     []struct {
			History []api.PortfolioHistoryPoint `json:"history"`
			Width   int                         `json:"width"`
			Height  int                         `json:"height"`
			Colour  bool                        `json:"colour"`
			Lines   []string                    `json:"lines"`
		} `json:"render"`
	}
	golden.Load(t, "testdata/chart.json.gz", &g)

	for _, c := range g.NiceCeil {
		if got := NiceCeil(c[0]); got != c[1] {
			t.Errorf("niceCeil(%v) = %v, want %v", c[0], got, c[1])
		}
	}
	for _, c := range g.CompactRaw {
		var v float64
		var want string
		_ = json.Unmarshal(c[0], &v)
		_ = json.Unmarshal(c[1], &want)
		if got := CompactMoney(v); got != want {
			t.Errorf("compactMoney(%v) = %q, want %q", v, got, want)
		}
	}
	for _, c := range g.Resample {
		var n, count int
		var want []float64
		_ = json.Unmarshal(c[0], &n)
		_ = json.Unmarshal(c[1], &count)
		_ = json.Unmarshal(c[2], &want)
		points := make([]Point, n)
		for i := range points {
			points[i] = Point{Value: float64(i)}
		}
		var got []float64
		for _, p := range Resample(points, count) {
			got = append(got, p.Value)
		}
		if !slices.Equal(got, want) {
			t.Errorf("resample(%d, %d) = %v, want %v", n, count, got, want)
		}
	}
	for i, c := range g.Render {
		got := Render(FromHistory(c.History), Options{Width: c.Width, Height: c.Height, Colour: c.Colour})
		if !slices.Equal(got, c.Lines) {
			t.Errorf("render #%d (%d points, %dx%d, colour %v):", i, len(c.History), c.Width, c.Height, c.Colour)
			for j := 0; j < max(len(got), len(c.Lines)); j++ {
				var a, b string
				if j < len(got) {
					a = got[j]
				}
				if j < len(c.Lines) {
					b = c.Lines[j]
				}
				if a != b {
					t.Errorf("  line %d\n   got: %q\n  want: %q", j, a, b)
				}
			}
		}
	}
}
