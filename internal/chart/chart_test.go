package chart

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/grubless/grubless-cli/internal/api"
	"github.com/grubless/grubless-cli/internal/jsstr"
)

// A port of src/test/chart.test.ts. What's worth asserting is the frame
// contract — exactly `height` lines, never wider than `width` — because the
// TUI slots the result into a fixed viewport, and one extra line pushes the
// status bar off the screen.

var (
	escape  = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)
	braille = regexp.MustCompile(`[\x{2800}-\x{28FF}]`)
	inked   = regexp.MustCompile(`[\x{2801}-\x{28FF}]`)
)

func strip(s string) string { return escape.ReplaceAllString(s, "") }
func width(s string) int    { return jsstr.Len(strip(s)) }

// valueSeries is value-only: income and P&L stay zero, so neither is drawn.
func valueSeries(values ...float64) []Point {
	out := make([]Point, len(values))
	for i, v := range values {
		out[i] = Point{Date: fmt.Sprintf("2026-01-%02d", i+1), Value: v}
	}
	return out
}

func full(n int) []Point {
	out := make([]Point, n)
	for i := range out {
		out[i] = Point{Date: fmt.Sprintf("2026-01-%02d", i%28+1), Value: 100_000 + float64(i)*1000, Income: 5_000 + float64(i)*100, Pnl: float64(i)*300 - 6_000}
	}
	return out
}

func render(points []Point, w, h int, colour bool) []string {
	return Render(points, Options{Width: w, Height: h, Colour: colour})
}

func TestNiceCeil(t *testing.T) {
	for in, want := range map[float64]float64{721_022.17: 1_000_000, 180_000: 200_000, 21: 25, 7: 10, 500: 500, 1000: 1000, 0: 1, -5: 1} {
		if got := NiceCeil(in); got != want {
			t.Errorf("NiceCeil(%v) = %v, want %v", in, got, want)
		}
	}
}

func TestCompactMoney(t *testing.T) {
	for in, want := range map[float64]string{1_234_567: "1.2M", 721_022.17: "721.0K", 45: "45", 0.25: "0.25", -2_500_000: "-2.5M", 0: "0"} {
		if got := CompactMoney(in); got != want {
			t.Errorf("CompactMoney(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestResample(t *testing.T) {
	short := valueSeries(1, 2, 3)
	if got := Resample(short, 10); len(got) != 3 {
		t.Error("a short series should come back untouched")
	}
	long := make([]float64, 900)
	for i := range long {
		long[i] = float64(i)
	}
	out := Resample(valueSeries(long...), 50)
	// The last point is "what is it worth now" — never dropped.
	if len(out) != 50 || out[0].Value != 0 || out[49].Value != 899 {
		t.Errorf("resample kept %d points, first %v, last %v", len(out), out[0].Value, out[len(out)-1].Value)
	}
	peak := make([]float64, 101)
	for i := range peak {
		peak[i] = 10
	}
	peak[50] = 1000
	top := 0.0
	for _, p := range Resample(valueSeries(peak...), 101) {
		top = max(top, p.Value)
	}
	if top != 1000 {
		t.Error("a real peak must survive resampling, not be averaged away")
	}
}

func TestFrameContract(t *testing.T) {
	for _, h := range []int{3, 8, 16} {
		if got := render(valueSeries(1, 5, 3, 9), 60, h, true); len(got) != h {
			t.Errorf("height %d gave %d lines", h, len(got))
		}
	}
	// A brand-new entity: short-returning here shifts everything below up.
	lines := render(nil, 60, 10, true)
	if len(lines) != 10 || !strings.Contains(strip(lines[0]), "No portfolio history yet") {
		t.Errorf("empty chart = %q", lines)
	}
	for _, line := range render(valueSeries(0, 1_000_000, 250_000, 900_000), 40, 10, true) {
		if width(line) > 40 {
			t.Errorf("line wider than 40: %q", strip(line))
		}
	}
	for _, h := range []int{10, 16, 30} {
		if got := render(full(40), 100, h, true); len(got) != h {
			t.Errorf("three series, height %d gave %d lines", h, len(got))
		}
	}
	for _, line := range render(full(400), 60, 20, true) {
		if width(line) > 60 {
			t.Errorf("three series: line wider than 60: %q", strip(line))
		}
	}
	if got := render(valueSeries(42), 40, 8, true); len(got) != 8 || strings.Contains(strings.Join(got, ""), "NaN") {
		t.Error("a single point should still fill the frame, without NaN")
	}
}

func TestGeometry(t *testing.T) {
	if !braille.MatchString(strings.Join(render(valueSeries(1, 2, 3, 4, 5), 40, 8, true), "")) {
		t.Error("a moving series should draw something")
	}

	// Rising series: ink on the top row sits right of ink on the bottom row.
	// An inverted y-axis would report a gain as a loss.
	plot := render(valueSeries(0, 100), 40, 9, true)[:8]
	top, bottom := -1, -1
	for i, l := range plot {
		if inked.MatchString(strip(l)) {
			if top < 0 {
				top = i
			}
			bottom = i
		}
	}
	firstInk := func(l string) int {
		for i, r := range []rune(strip(l)) {
			if r >= 0x2801 && r <= 0x28FF {
				return i
			}
		}
		return -1
	}
	if firstInk(plot[top]) <= firstInk(plot[bottom]) {
		t.Error("a rising series should draw low-left to high-right")
	}

	// Zero-based, not the running minimum: noise must not read as a cliff.
	lines := render(valueSeries(900_000, 950_000, 1_000_000), 60, 10, true)
	if !strings.Contains(strip(lines[len(lines)-2]), "0") || !strings.Contains(strip(lines[0]), "1.0M") {
		t.Error("axis should run from 0 to 1.0M")
	}
	// Below zero only when the series goes there.
	lines = render(valueSeries(-500, 200), 60, 10, true)
	if !strings.Contains(strip(lines[len(lines)-2]), "-500") {
		t.Errorf("negative axis = %q", strip(lines[len(lines)-2]))
	}

	for _, vals := range [][]float64{{100, 100, 100}, {0, 0}} {
		if strings.Contains(strings.Join(render(valueSeries(vals...), 40, 8, true), ""), "NaN") {
			t.Errorf("flat series %v produced NaN", vals)
		}
	}
}

func TestDateAxis(t *testing.T) {
	lines := render(valueSeries(1, 2, 3), 80, 8, true)
	axis := strip(lines[len(lines)-1])
	if !strings.Contains(axis, "2026-01-01") || !strings.Contains(axis, "2026-01-03") {
		t.Errorf("axis = %q", axis)
	}
	// Too narrow for three dates: the span only.
	lines = render(valueSeries(1, 2, 3), 34, 6, true)
	axis = strip(lines[len(lines)-1])
	if !strings.Contains(axis, "→") || jsstr.Len(axis) > 34 {
		t.Errorf("narrow axis = %q", axis)
	}
}

func TestColour(t *testing.T) {
	// `grubless portfolio` into a file must carry no escapes, on any line.
	for _, line := range render(valueSeries(1, 5, 3), 60, 8, false) {
		if strings.Contains(line, "\x1b") {
			t.Errorf("escape under colour off: %q", line)
		}
	}
	if got := render(nil, 60, 6, false); got[0] != "No portfolio history yet." {
		t.Errorf("empty, colour off = %q", got[0])
	}
	if !strings.Contains(strings.Join(render(valueSeries(1, 2), 60, 8, true), ""), "\x1b") {
		t.Error("colour on should colour")
	}

	out := strings.Join(render(full(40), 80, 16, true), "\n")
	if !strings.Contains(out, Cyan) || !strings.Contains(out, Green) {
		t.Error("value and income should be different colours")
	}
	// No income: no flat line pinned to zero pretending to be data.
	if strings.Contains(strings.Join(render(valueSeries(1, 5, 3, 9), 80, 16, true), ""), Green) {
		t.Error("no income should mean no income line")
	}
}

func TestPnlStrip(t *testing.T) {
	// Small P&L next to a large value: on a shared axis it would be a flat
	// line on the floor; on its own symmetric strip it's legible.
	points := make([]Point, 30)
	for i := range points {
		pnl := 500.0
		if i%2 == 1 {
			pnl = -500
		}
		points[i] = Point{Date: fmt.Sprintf("2026-01-%02d", i+1), Value: 1_000_000, Pnl: pnl}
	}
	lines := render(points, 80, 16, true)
	if !strings.Contains(strings.Join(lines, "\n"), Yellow) {
		t.Error("P&L strip should be drawn in yellow")
	}
	own := false
	for _, l := range lines {
		s := strip(l)
		own = own || (strings.Contains(s, "500") && !strings.Contains(s, "1.0M"))
	}
	if !own {
		t.Error("the strip should be labelled from its own magnitude")
	}
	// Dropped, not squeezed, on a short plot.
	short := render(full(40), 80, 6, true)
	if len(short) != 6 || strings.Contains(strings.Join(short, ""), Yellow) {
		t.Error("a short plot should drop the P&L strip")
	}
}

func TestFromHistory(t *testing.T) {
	v, inc, pl := "721022.17", "4321.5", "-987.65"
	p := FromHistory([]api.PortfolioHistoryPoint{{Date: "2026-08-07", Value: &v, CumulativeIncome: &inc, UnrealizedPL: &pl}})[0]
	if p != (Point{Date: "2026-08-07", Value: 721022.17, Income: 4321.5, Pnl: -987.65}) {
		t.Errorf("FromHistory = %+v", p)
	}
}
