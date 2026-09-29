package tui

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/grubless/grubless-cli/internal/chart"
)

// Themes, after the web app's (apps/web/lib/themes.ts in the main repo).
//
// "cypher" is the web's Cypherpunk — a phosphor terminal on an 8-bit console
// — with its colours taken exactly from `.cypher` in apps/web/app/globals.css.
// Those were measured there for contrast and colour-blind separation, so they
// are copied, not re-picked. What a terminal can't draw (scanlines, pixel
// shadows, the pixel font) is left out rather than imitated. It's the default
// wherever the terminal can show it; see DefaultTheme.
//
// "terminal" is the TUI as it first looked: the terminal's own sixteen
// colours, on its own background. The web's light and dark themes have no
// counterpart here because a terminal already is one or the other.
const (
	ThemeTerminal = "terminal"
	ThemeCypher   = "cypher"
)

var Themes = []string{ThemeCypher, ThemeTerminal}

var ThemeLabel = map[string]string{ThemeTerminal: "Terminal", ThemeCypher: "Cypherpunk"}

// NormaliseTheme accepts a theme's id or the web's label for it. "" means no
// choice was made (the caller falls back to DefaultTheme); ok is false for a
// name that isn't a theme.
func NormaliseTheme(name string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "":
		return "", true
	case ThemeTerminal:
		return ThemeTerminal, true
	case ThemeCypher, "cypherpunk":
		return ThemeCypher, true
	}
	return "", false
}

// DefaultTheme is the theme when nobody has chosen one: Cypherpunk, unless
// the terminal can't show it or the person has asked for no colour.
//
//   - NO_COLOR is a request not to be shown colour an app picked. Cypherpunk
//     paints its own background and text; the terminal theme uses only the
//     terminal's own palette, which is the closest thing to honouring it in
//     an interface that needs *some* highlighting to work at all.
//   - Cypherpunk needs at least 256 colours. The Linux console and plain
//     xterm/vt100 entries have sixteen, where its codes would come out as
//     the wrong colours rather than degrading.
//
// Choosing Cypherpunk explicitly (`t`, GRUBLESS_THEME) overrides both: that's
// someone who knows their terminal.
func DefaultTheme() string {
	if os.Getenv("NO_COLOR") != "" || !Colour256() {
		return ThemeTerminal
	}
	return ThemeCypher
}

// Colour256 is whether the terminal says it has at least 256 colours.
func Colour256() bool {
	if TrueColour() {
		return true
	}
	t := os.Getenv("TERM")
	return strings.Contains(t, "256color") || strings.Contains(t, "direct")
}

// The Cypherpunk tokens, from globals.css.
const (
	cypherBackground  = "#0a0e0a"
	cypherForeground  = "#d4fcd2"
	cypherMuted       = "#86c48c" // --muted-foreground
	cypherPrimary     = "#c44dff"
	cypherPrimaryFg   = "#0a0e0a"
	cypherDestructive = "#ff5c5c"
	cypherSuccess     = "#39ff88"
	cypherBorder      = "#3f7a4b"
	cypherRing        = "#00e5ff"
	cypherCatAqua     = "#0aad95"
	cypherCatYellow   = "#a69306"
	cypherCatBlue     = "#1874ed"
	cypherCatViolet   = "#8442ee"
	cypherCatMagenta  = "#fc21a2"
	cypherCatOrange   = "#d86105"
	cypherCard        = "#0f1712"
	cypherSecondary   = "#152219"
	cypherShadow      = "#1f3d27" // --pixel-shadow
)

// palette is every escape code the views use, for one theme.
type palette struct {
	// base is the theme's own background and text colour, set before a line
	// is cleared so the whole row takes it. Empty for the terminal theme.
	base string
	// reset ends a styled run and returns to base: for a theme with its own
	// background, a bare reset would drop to the terminal's.
	reset string

	bold, dim, red, green, yellow, cyan string
	// The three dashboard figures match their lines on the chart.
	value, income, pnl string

	chart chart.Palette

	// Cards: corner style, border and title colours, the card's own
	// surface (empty to use the page's), and the offset shadow (empty for
	// none) as a foreground for its half-blocks and a background for its
	// column.
	square                          bool
	cardBorder, cardTitle, cardBase string
	shadow, shadowBg                string

	// cat is the categorical palette for charts with several series, in
	// the web's order; the muted "Other" is dim.
	cat []string

	// tabs draws the tab bar: the web's segmented control.
	tabs func(labels []string, active int) string

	// selected is a highlighted row.
	selected func(string) string
	// title is the app's name in the picker's heading.
	title func() string
}

// paletteFor builds a theme's palette. trueColour says the terminal takes
// 24-bit colour; without it Cypherpunk falls back to the nearest of the 256
// standard colours.
func paletteFor(theme string, trueColour bool) palette {
	if theme != ThemeCypher {
		p := palette{
			reset: reset, bold: bold, dim: dim, red: red, green: green, yellow: yellow, cyan: cyan,
			value: cyan, income: green, pnl: yellow,
			chart: chart.TerminalPalette,
		}
		p.selected = func(s string) string { return reverse + s + reset }
		p.title = func() string { return bold + "Grubless" + reset }
		p.cardBorder, p.cardTitle = dim, dim
		// Blue, cyan, yellow, magenta, bright magenta, bright red: the web's
		// blue/aqua/yellow/violet/magenta/orange, in sixteen colours.
		p.cat = []string{esc + "[34m", cyan, yellow, esc + "[35m", esc + "[95m", esc + "[91m"}
		p.tabs = func(labels []string, active int) string {
			var b strings.Builder
			for i, l := range labels {
				if i == active {
					b.WriteString(reverse + " " + l + " " + reset)
				} else {
					b.WriteString(dim + " " + l + " " + reset)
				}
			}
			return b.String()
		}
		return p
	}

	fg := func(hex string) string { return colour(38, hex, trueColour) }
	bg := func(hex string) string { return colour(48, hex, trueColour) }
	base := bg(cypherBackground) + fg(cypherForeground)
	r := reset + base

	p := palette{
		base: base, reset: r,
		// Headings in the success green, as the web's glow does.
		bold: bold + fg(cypherSuccess),
		// The muted foreground rather than SGR "faint", which would dim an
		// already-measured colour below the contrast it was chosen for.
		dim:    fg(cypherMuted),
		red:    fg(cypherDestructive),
		green:  fg(cypherSuccess),
		yellow: fg(cypherCatYellow),
		cyan:   fg(cypherRing),
		// The web portfolio chart's own series colours: value in the primary
		// purple, income in aqua. (Purple, not pink, for the reason in
		// globals.css: pink and aqua are one colour to a deuteranope.)
		value:  fg(cypherPrimary),
		income: fg(cypherCatAqua),
		pnl:    fg(cypherCatYellow),
	}
	p.chart = chart.Palette{Value: p.value, Income: p.income, Pnl: p.pnl, Label: p.dim, Baseline: fg(cypherBorder), Reset: r}

	// Cards as the web's: square, the card surface a shade off the page,
	// and the hard pixel shadow.
	p.square = true
	p.cardBorder, p.cardTitle = fg(cypherBorder), fg(cypherMuted)
	p.cardBase = bg(cypherCard) + fg(cypherForeground)
	p.shadow, p.shadowBg = fg(cypherShadow), bg(cypherShadow)
	p.cat = []string{fg(cypherCatBlue), fg(cypherCatAqua), fg(cypherCatYellow), fg(cypherCatViolet), fg(cypherCatMagenta), fg(cypherCatOrange)}

	// The segmented control: a muted strip, the active tab raised out of it
	// in the page colour.
	strip := bg(cypherSecondary) + fg(cypherMuted)
	p.tabs = func(labels []string, active int) string {
		var b strings.Builder
		b.WriteString(strip + " ")
		for i, l := range labels {
			if i == active {
				b.WriteString(bg(cypherBackground) + bold + fg(cypherForeground) + " " + l + " " + reset + strip)
			} else {
				b.WriteString(" " + l + " ")
			}
		}
		return b.String() + " " + r
	}
	// The web's ::selection: primary behind, primary-foreground on it.
	p.selected = func(s string) string { return bg(cypherPrimary) + fg(cypherPrimaryFg) + s + r }
	// An uppercase title ending in a block cursor, as the web's h1 does. The
	// cursor blinks only if the terminal lets things blink.
	p.title = func() string { return p.bold + "GRUBLESS" + r + fg(cypherSuccess) + esc + "[5m█" + r }
	return p
}

// colour is an SGR colour for a hex value: `layer` 38 for text, 48 for
// background.
func colour(layer int, hex string, trueColour bool) string {
	r, g, b := rgb(hex)
	if trueColour {
		return fmt.Sprintf("%s[%d;2;%d;%d;%dm", esc, layer, r, g, b)
	}
	return fmt.Sprintf("%s[%d;5;%dm", esc, layer, nearest256(r, g, b))
}

func rgb(hex string) (int, int, int) {
	n, _ := strconv.ParseUint(strings.TrimPrefix(hex, "#"), 16, 32)
	return int(n >> 16 & 0xff), int(n >> 8 & 0xff), int(n & 0xff)
}

// nearest256 is the closest xterm-256 colour: the 6×6×6 cube or the 24-step
// grey ramp, whichever lands nearer. The first sixteen are skipped, since
// terminals redefine them freely.
func nearest256(r, g, b int) int {
	levels := []int{0, 95, 135, 175, 215, 255}
	closest := func(v int) int {
		best := 0
		for i, l := range levels {
			if abs(l-v) < abs(levels[best]-v) {
				best = i
			}
		}
		return best
	}
	dist := func(r2, g2, b2 int) int { return (r-r2)*(r-r2) + (g-g2)*(g-g2) + (b-b2)*(b-b2) }

	ci, cj, ck := closest(r), closest(g), closest(b)
	cube := 16 + 36*ci + 6*cj + ck
	cubeDist := dist(levels[ci], levels[cj], levels[ck])

	grey := min(23, max(0, ((r+g+b)/3-8+5)/10))
	level := 8 + 10*grey
	if dist(level, level, level) < cubeDist {
		return 232 + grey
	}
	return cube
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// TrueColour is whether the terminal says it takes 24-bit colour. COLORTERM
// is how terminals advertise it; Windows Terminal sets WT_SESSION instead.
func TrueColour() bool {
	switch strings.ToLower(os.Getenv("COLORTERM")) {
	case "truecolor", "24bit":
		return true
	}
	return os.Getenv("WT_SESSION") != ""
}

// nextTheme is the theme after this one, wrapping.
func nextTheme(current string) string {
	for i, t := range Themes {
		if t == current {
			return Themes[(i+1)%len(Themes)]
		}
	}
	return Themes[0]
}
