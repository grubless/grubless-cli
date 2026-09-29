// Package chart draws the portfolio line chart in Unicode braille — a port of
// src/tui/chart.ts. Pure: points in, lines out, no terminal and no state.
//
// Braille cells address 2×4 dots each, with a non-sequential bit layout:
//
//	bit0  bit3        (row 0)
//	bit1  bit4        (row 1)
//	bit2  bit5        (row 2)
//	bit6  bit7        (row 3)   <- appended later in Unicode history
//
// The arithmetic uses JS semantics where Go's differ (Math.round, toFixed),
// because a one-dot difference in where a line lands is a different picture.
package chart

import (
	"math"
	"strings"
	"unicode/utf16"

	"github.com/grubless/grubless-cli/go/internal/api"
	"github.com/grubless/grubless-cli/go/internal/jsstr"
)

const (
	brailleBase = 0x2800
	dotsX       = 2
	dotsY       = 4

	gutter = 9
	// Rows the unrealised-P&L strip gets when it's drawn at all.
	pnlRows = 3
	// Below this the strip would crowd out the main plot; it's dropped instead.
	minHeightForPnl = 10
)

// ANSI codes, as in src/tui/terminal.ts.
const (
	Reset  = "\x1b[0m"
	Dim    = "\x1b[2m"
	Red    = "\x1b[31m"
	Green  = "\x1b[32m"
	Yellow = "\x1b[33m"
	Cyan   = "\x1b[36m"
)

type canvas struct {
	cols, rows int
	cells      []uint8
}

func newCanvas(cols, rows int) *canvas {
	return &canvas{cols: cols, rows: rows, cells: make([]uint8, cols*rows)}
}

// set lights dot (x, y), y measured downward. Out-of-range dots are dropped.
func (c *canvas) set(x, y int) {
	if x < 0 || y < 0 {
		return
	}
	cx, cy := x/dotsX, y/dotsY
	if cx >= c.cols || cy >= c.rows {
		return
	}
	dx, dy := x%dotsX, y%dotsY
	var bit int
	switch {
	case dy == 3 && dx == 0:
		bit = 6
	case dy == 3:
		bit = 7
	default:
		bit = dx*3 + dy
	}
	c.cells[cy*c.cols+cx] |= 1 << bit
}

// line is Bresenham between two dots, so a volatile series stays connected.
func (c *canvas) line(x0, y0, x1, y1 int) {
	dx, dy := abs(x1-x0), abs(y1-y0)
	sx, sy := 1, 1
	if x0 >= x1 {
		sx = -1
	}
	if y0 >= y1 {
		sy = -1
	}
	err := dx - dy
	x, y := x0, y0
	for {
		c.set(x, y)
		if x == x1 && y == y1 {
			return
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x += sx
		}
		if e2 < dx {
			err += dx
			y += sy
		}
	}
}

func (c *canvas) maskAt(col, row int) uint8 { return c.cells[row*c.cols+col] }

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// Point is one day of the portfolio, as plot geometry. Floats only here: a
// braille dot is one of a couple of hundred columns, and every figure a
// person reads is formatted from the exact strings elsewhere.
type Point struct {
	Date   string
	Value  float64
	Income float64
	Pnl    float64
}

// FromHistory is the single place a portfolio figure becomes a float.
func FromHistory(points []api.PortfolioHistoryPoint) []Point {
	out := make([]Point, len(points))
	for i, p := range points {
		out[i] = Point{Date: p.Date, Value: number(p.Value), Income: number(p.CumulativeIncome), Pnl: number(p.UnrealizedPL)}
	}
	return out
}

// number is Number(x) for a string-or-null field: null is 0.
func number(s *string) float64 {
	if s == nil {
		return 0
	}
	return jsstr.ParseNumber(*s)
}

// NiceCeil rounds an axis bound up to a clean 1/2/2.5/5 × 10^n.
func NiceCeil(value float64) float64 {
	if math.IsNaN(value) {
		return value
	}
	if value <= 0 {
		return 1
	}
	exp := floorLog10(value)
	base := math.Pow10(exp)
	for _, step := range []float64{1, 2, 2.5, 5, 10} {
		if value <= step*base {
			return step * base
		}
	}
	return 10 * base
}

// floorLog10 is Math.floor(Math.log10(v)), exactly. Go's math.Log10 is
// Log(x)/Ln10 and misses on exact powers — Log10(1000) is 2.9999999999999996
// — where V8's is exact, so the estimate is corrected against Pow10.
func floorLog10(v float64) int {
	e := int(math.Floor(math.Log10(v)))
	for math.Pow10(e+1) <= v {
		e++
	}
	for e > -323 && math.Pow10(e) > v {
		e--
	}
	return e
}

// CompactMoney is short currency for an axis gutter: 1.2M, 721.0K, 45.
func CompactMoney(value float64) string {
	a := math.Abs(value)
	sign := ""
	if value < 0 {
		sign = "-"
	}
	switch {
	case a == 0:
		return "0"
	case a >= 1e9:
		return sign + jsstr.ToFixed(a/1e9, 1) + "B"
	case a >= 1e6:
		return sign + jsstr.ToFixed(a/1e6, 1) + "M"
	case a >= 1e3:
		return sign + jsstr.ToFixed(a/1e3, 1) + "K"
	case a >= 1:
		return sign + jsstr.ToFixed(a, 0)
	}
	return sign + jsstr.ToFixed(a, 2)
}

// Resample picks count evenly spaced points, always keeping the last:
// nearest-sample, not averaging, so a real peak survives.
func Resample(points []Point, count int) []Point {
	if len(points) <= count || count <= 1 {
		return points
	}
	out := make([]Point, count)
	for i := 0; i < count; i++ {
		index := int(jsstr.Round(float64(i*(len(points)-1)) / float64(count-1)))
		out[i] = points[index]
	}
	return out
}

type layer struct {
	canvas *canvas
	colour string
}

type painter func(code, text string) string

func newPainter(colour bool, reset string) painter {
	return func(code, text string) string {
		if colour {
			return code + text + reset
		}
		return text
	}
}

// Palette is the escape codes each part of the chart is drawn in. A TUI theme
// supplies its own; the zero value is the terminal's own colours, below.
type Palette struct {
	Value, Income, Pnl string
	// Label is the axis text; Baseline the P&L strip's zero line.
	Label, Baseline string
	// Reset ends a run — for a theme with its own background, a reset that
	// puts that background back, rather than the terminal's.
	Reset string
}

// TerminalPalette is the terminal's own colours, and what every caller got
// before themes existed.
var TerminalPalette = Palette{Value: Cyan, Income: Green, Pnl: Yellow, Label: Dim, Baseline: Dim, Reset: Reset}

// Options for Render. Colour is on for the TUI; `grubless portfolio` turns
// it off when stdout isn't a terminal. A nil Palette is TerminalPalette.
type Options struct {
	Width, Height int
	Colour        bool
	Palette       *Palette
}

// Render draws value and income on one zero-based axis, unrealised P&L in a
// symmetric strip below, and a date axis — exactly Height lines. See
// src/tui/chart.ts renderPortfolio for why each series sits where it does.
func Render(points []Point, opts Options) []string {
	pal := TerminalPalette
	if opts.Palette != nil {
		pal = *opts.Palette
	}
	paint := newPainter(opts.Colour, pal.Reset)
	plotCols := max(1, opts.Width-gutter)

	if len(points) == 0 {
		lines := []string{paint(pal.Label, "No portfolio history yet.")}
		for i := 1; i < opts.Height; i++ {
			lines = append(lines, "")
		}
		return lines
	}

	hasIncome, hasPnl := false, false
	for _, p := range points {
		hasIncome = hasIncome || p.Income != 0
		hasPnl = hasPnl || p.Pnl != 0
	}

	strip := 0
	if hasPnl && opts.Height >= minHeightForPnl {
		strip = pnlRows
	}
	plotRows := max(1, opts.Height-1-strip)
	dotsWide := plotCols * dotsX
	sampled := Resample(points, dotsWide)

	lines := mainPlot(sampled, plotCols, plotRows, dotsWide, hasIncome, paint, pal)
	if strip > 0 {
		lines = append(lines, pnlStrip(sampled, plotCols, strip, dotsWide, paint, pal)...)
	}
	return append(lines, dateAxis(points, plotCols, paint, pal))
}

func mainPlot(points []Point, plotCols, plotRows, dotsWide int, hasIncome bool, paint painter, pal Palette) []string {
	var all []float64
	for _, p := range points {
		all = append(all, p.Value)
	}
	if hasIncome {
		for _, p := range points {
			all = append(all, p.Income)
		}
	}

	rawMin := jsstr.Min(all...)
	top := NiceCeil(jsstr.Max(append(all, 1)...))
	bottom := 0.0
	if rawMin < 0 {
		bottom = -NiceCeil(-rawMin)
	}
	span := top - bottom
	if span == 0 || math.IsNaN(span) {
		span = 1
	}

	dotsHigh := plotRows * dotsY
	toY := func(v float64) int {
		ratio := (v - bottom) / span
		return clampDot(jsstr.Round((1-ratio)*float64(dotsHigh-1)), dotsHigh)
	}

	// Value last so it wins a cell both series land in — then reversed, so
	// value is the first layer merge() consults.
	var layers []layer
	if hasIncome {
		layers = append(layers, layer{draw(series(points, func(p Point) float64 { return p.Income }), toY, plotCols, plotRows, dotsWide), pal.Income})
	}
	layers = append(layers, layer{draw(series(points, func(p Point) float64 { return p.Value }), toY, plotCols, plotRows, dotsWide), pal.Value})
	for i, j := 0, len(layers)-1; i < j; i, j = i+1, j-1 {
		layers[i], layers[j] = layers[j], layers[i]
	}

	plot := merge(layers, plotCols, plotRows, paint)
	for i, row := range plot {
		label := ""
		switch {
		case i == 0:
			label = CompactMoney(top)
		case i == plotRows-1:
			label = CompactMoney(bottom)
		case i == plotRows/2:
			label = CompactMoney(bottom + span/2)
		}
		plot[i] = paint(pal.Label, jsstr.PadStart(label, gutter-1)) + " " + row
	}
	return plot
}

func pnlStrip(points []Point, plotCols, plotRows, dotsWide int, paint painter, pal Palette) []string {
	values := series(points, func(p Point) float64 { return p.Pnl })
	abs := make([]float64, len(values), len(values)+1)
	for i, v := range values {
		abs[i] = math.Abs(v)
	}
	bound := NiceCeil(jsstr.Max(append(abs, 1)...))

	dotsHigh := plotRows * dotsY
	zeroDot := (dotsHigh - 1) / 2
	toY := func(v float64) int {
		offset := (v / bound) * float64(zeroDot)
		return clampDot(jsstr.Round(float64(zeroDot)-offset), dotsHigh)
	}

	baseline := newCanvas(plotCols, plotRows)
	for x := 0; x < dotsWide; x++ {
		baseline.set(x, zeroDot)
	}
	layers := []layer{
		{draw(values, toY, plotCols, plotRows, dotsWide), pal.Pnl},
		{baseline, pal.Baseline},
	}

	plot := merge(layers, plotCols, plotRows, paint)
	for i, row := range plot {
		label := "P/L"
		switch {
		case i == 0:
			label = CompactMoney(bound)
		case i == plotRows-1:
			label = CompactMoney(-bound)
		}
		plot[i] = paint(pal.Label, jsstr.PadStart(label, gutter-1)) + " " + row
	}
	return plot
}

// clampDot is Math.min(dotsHigh - 1, Math.max(0, y)). A NaN y (a value the
// server sent that isn't a number) is placed off the canvas, where set()
// drops it — the TS draws nothing for it too, but then loops forever in
// Bresenham trying to reach NaN; this doesn't.
func clampDot(y float64, dotsHigh int) int {
	if math.IsNaN(y) {
		return -1
	}
	return int(math.Min(float64(dotsHigh-1), math.Max(0, y)))
}

func series(points []Point, f func(Point) float64) []float64 {
	out := make([]float64, len(points))
	for i, p := range points {
		out[i] = f(p)
	}
	return out
}

func draw(values []float64, toY func(float64) int, plotCols, plotRows, dotsWide int) *canvas {
	c := newCanvas(plotCols, plotRows)
	prevX, prevY := 0, toY(values[0])
	c.set(prevX, prevY)
	for i := 1; i < len(values); i++ {
		x := int(jsstr.Round(float64(i*(dotsWide-1)) / float64(len(values)-1)))
		y := toY(values[i])
		if y < 0 || prevY < 0 {
			// A NaN point: nothing to join to. See clampDot.
			c.set(x, y)
		} else {
			c.line(prevX, prevY, x, y)
		}
		prevX, prevY = x, y
	}
	return c
}

// merge flattens layers into coloured rows. A braille cell carries one
// colour, so where series share a cell only the first layer's dots are
// drawn, and runs of one colour share one escape pair.
func merge(layers []layer, cols, rows int, paint painter) []string {
	out := make([]string, rows)
	for row := 0; row < rows; row++ {
		var line, run strings.Builder
		runColour := ""
		hasRunColour := false
		flush := func() {
			if run.Len() > 0 {
				if hasRunColour {
					line.WriteString(paint(runColour, run.String()))
				} else {
					line.WriteString(run.String())
				}
			}
			run.Reset()
		}
		for col := 0; col < cols; col++ {
			var owner *layer
			for i := range layers {
				if layers[i].canvas.maskAt(col, row) != 0 {
					owner = &layers[i]
					break
				}
			}
			colour, has := "", false
			char := ' '
			if owner != nil {
				colour, has = owner.colour, true
				char = rune(brailleBase + int(owner.canvas.maskAt(col, row)))
			}
			if has != hasRunColour || colour != runColour {
				flush()
				runColour, hasRunColour = colour, has
			}
			run.WriteRune(char)
		}
		flush()
		out[row] = jsstr.TrimEnd(line.String())
	}
	return out
}

// dateAxis puts the first, middle and last dates under the points they
// describe, or just the span when three won't fit.
func dateAxis(points []Point, plotCols int, paint painter, pal Palette) string {
	first := points[0].Date
	last := points[len(points)-1].Date
	mid := points[len(points)/2].Date

	if plotCols < jsstr.Len(first)*3+4 {
		text := first + " → " + last
		return strings.Repeat(" ", gutter) + paint(pal.Label, jsstr.Slice(text, 0, plotCols))
	}

	// Slots are UTF-16 units, as the TS's `line[start + i] = text[i]` is, so
	// an astral character placed whole survives and one cut in half prints
	// as U+FFFD — utf16.Decode's treatment of a lone surrogate.
	line := make([]uint16, plotCols)
	for i := range line {
		line[i] = ' '
	}
	place := func(text string, start int) {
		units := utf16.Encode([]rune(text))
		for i := 0; i < len(units) && start+i < plotCols; i++ {
			line[start+i] = units[i]
		}
	}
	place(first, 0)
	// On a two- or three-day series the middle sample IS the first or last.
	if mid != first && mid != last {
		place(mid, (plotCols-jsstr.Len(mid))/2)
	}
	place(last, plotCols-jsstr.Len(last))
	return strings.Repeat(" ", gutter) + paint(pal.Label, string(utf16.Decode(line)))
}
