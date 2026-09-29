package tui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/grubless/grubless-cli/internal/api"
)

// The Cypherpunk theme. The terminal theme is pinned byte for byte by the
// golden render tests; these check what Cypherpunk must do on top.

func cypher(st State) State {
	st.Theme, st.TrueColour = ThemeCypher, true
	return st
}

// The web app's tokens, as the SGR codes they must come out as.
var (
	cyBackground = "\x1b[48;2;10;14;10m"
	cyForeground = "\x1b[38;2;212;252;210m"
	cyPrimaryBg  = "\x1b[48;2;196;77;255m"
	cyValue      = "\x1b[38;2;196;77;255m"
	cyIncome     = "\x1b[38;2;10;173;149m"
	cyMuted      = "\x1b[38;2;134;196;140m"
	cyCard       = "\x1b[48;2;15;23;18m"
)

func TestThemeNames(t *testing.T) {
	// "" is no choice at all; the app falls back to DefaultTheme for it.
	for in, want := range map[string]string{"": "", "terminal": ThemeTerminal, "cypher": ThemeCypher, "Cypherpunk": ThemeCypher, " CYPHER ": ThemeCypher} {
		if got, ok := NormaliseTheme(in); !ok || got != want {
			t.Errorf("NormaliseTheme(%q) = %q, %v", in, got, ok)
		}
	}
	if _, ok := NormaliseTheme("solarized"); ok {
		t.Error("an unknown theme should be refused")
	}
}

func TestThemeKey(t *testing.T) {
	st, action := Reduce(opened(nil), key("t"), 10)
	if st.Theme != ThemeCypher || action != ActTheme || st.Message == nil || *st.Message != "Theme: Cypherpunk" {
		t.Errorf("t: theme %q action %s", st.Theme, action)
	}
	if st, _ = Reduce(st, key("t"), 10); st.Theme != ThemeTerminal {
		t.Errorf("t again should wrap to Terminal, got %q", st.Theme)
	}
	// On the picker too.
	if st, _ := Reduce(withEntities(2), key("t"), 10); st.Theme != ThemeCypher {
		t.Error("t should work on the picker")
	}
	// Help is modal: t dismisses it like any key, and does nothing else.
	help := press(opened(nil), "?")
	if st, action := Reduce(help, key("t"), 10); st.Theme != "" || action != ActNone || st.ShowHelp {
		t.Error("t on the help screen should only close it")
	}
}

func TestCypherScreens(t *testing.T) {
	Now = func() time.Time { return time.Date(2026, 8, 9, 4, 0, 0, 0, time.UTC) }
	defer func() { Now = time.Now }()

	chart := cypher(opened(func(st *State) {
		st.Tab, st.Range = "chart", "all"
		st.Data.History = history([2]string{"2026-01-01", "1000"}, [2]string{"2026-08-01", "50000"})
		st.Data.History[1].CumulativeIncome = s("400")
		st.Data.Holdings = []api.Holding{holding("SOL", s("solana"), "900")}
	}))
	screens := map[string]State{
		"picker":       cypher(withEntities(3)),
		"chart":        chart,
		"holdings":     cypher(opened(func(st *State) { st.Data.Holdings = []api.Holding{holding("SOL", s("solana"), "900")} })),
		"help":         cypher(press(opened(nil), "?")),
		"transactions": cypher(onTransactions(3, true)),
		"detail":       cypher(press(onTransactions(3, true), "return")),
	}
	bareReset := regexp.MustCompile(`\x1b\[0m`)
	for name, st := range screens {
		lines := Render(st, 100, 30)
		if len(lines) != 30 {
			t.Errorf("%s: %d lines", name, len(lines))
		}
		for i, line := range lines {
			if VisibleWidth(line) > 100 {
				t.Errorf("%s line %d wider than 100", name, i)
			}
			// Every reset must be followed straight away by a background —
			// the page's, a card's, the tab strip's — or the rest of the row
			// drops to the terminal's own. Truncate's closing reset ends the
			// line, so it's the one exception.
			body := strings.TrimSuffix(line, "…\x1b[0m")
			for _, m := range bareReset.FindAllStringIndex(body, -1) {
				rest := strings.TrimLeft(strings.ReplaceAll(body[m[1]:], "\x1b[0m", "\x00"), "\x00")
				if rest != "" && !strings.HasPrefix(rest, "\x1b[48;") {
					t.Errorf("%s line %d: a reset with no background after it: %q", name, i, line)
					break
				}
			}
			// The terminal theme's codes must not leak in.
			for _, code := range []string{reverse, dim, cyan, green, yellow, red} {
				if strings.Contains(line, code) {
					t.Errorf("%s line %d: terminal-theme code %q in Cypherpunk", name, i, code)
				}
			}
		}
	}

	picker := strings.Join(Render(screens["picker"], 100, 30), "\n")
	if !strings.Contains(StripAnsi(picker), "GRUBLESS█") {
		t.Error("the title is uppercase with a block cursor, as the web's h1")
	}
	if !strings.Contains(picker, cyPrimaryBg+"\x1b[38;2;10;14;10m") || !strings.Contains(picker, cyCard) {
		t.Error("the selected row should be primary purple with dark text, the web's ::selection")
	}
	c := strings.Join(Render(chart, 100, 30), "\n")
	if !regexp.MustCompile(regexp.QuoteMeta(cyValue)+`\d`).MatchString(c) || !strings.Contains(c, cyIncome+"income") {
		t.Error("dashboard figures take the web chart's series colours")
	}
	if !strings.Contains(c, cyValue+"⠀") && !regexp.MustCompile(regexp.QuoteMeta(cyValue)+`[\x{2801}-\x{28FF}]`).MatchString(c) {
		t.Error("the value line should be drawn in primary purple")
	}
	if !strings.Contains(c, cyMuted) {
		t.Error("dim text should be the muted foreground, not SGR faint")
	}
}

func TestCypherWithout24BitColour(t *testing.T) {
	st := cypher(withEntities(2))
	st.TrueColour = false
	out := strings.Join(Render(st, 80, 24), "\n")
	if strings.Contains(out, ";2;") {
		t.Error("without 24-bit colour, no 24-bit codes")
	}
	if !strings.Contains(out, "\x1b[48;5;") {
		t.Error("the 256-colour fallback should still set the background")
	}
	// Spot-check the fallback lands on sensible neighbours.
	for hex, want := range map[string]int{"#000000": 16, "#ffffff": 231, "#39ff88": 84, "#c44dff": 171, "#0a0e0a": 232} {
		r, g, b := rgb(hex)
		if got := nearest256(r, g, b); got != want {
			t.Errorf("nearest256(%s) = %d, want %d", hex, got, want)
		}
	}
	_ = cyForeground
}

func TestDefaultTheme(t *testing.T) {
	cases := []struct {
		name                         string
		term, colorterm, noColor, wt string
		want                         string
	}{
		{"a 256-colour terminal", "xterm-256color", "", "", "", ThemeCypher},
		{"24-bit colour", "xterm", "truecolor", "", "", ThemeCypher},
		{"Windows Terminal", "", "", "", "1", ThemeCypher},
		{"macOS Terminal.app", "xterm-256color", "", "", "", ThemeCypher},
		// Sixteen colours: Cypherpunk's codes would come out wrong.
		{"the Linux console", "linux", "", "", "", ThemeTerminal},
		{"plain xterm", "xterm", "", "", "", ThemeTerminal},
		// Someone who asked for no colour doesn't get a theme's.
		{"NO_COLOR", "xterm-256color", "truecolor", "1", "", ThemeTerminal},
	}
	for _, c := range cases {
		t.Setenv("TERM", c.term)
		t.Setenv("COLORTERM", c.colorterm)
		t.Setenv("NO_COLOR", c.noColor)
		t.Setenv("WT_SESSION", c.wt)
		if got := DefaultTheme(); got != c.want {
			t.Errorf("%s: DefaultTheme() = %q, want %q", c.name, got, c.want)
		}
	}
}
