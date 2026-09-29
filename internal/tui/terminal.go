// Package tui is the interactive interface — a port of src/tui/. Same split
// as the TS: state.go is a pure reducer, views.go is pure rendering, and only
// terminal.go and app.go touch the terminal or the network.
//
// Still hand-rolled against ANSI rather than a TUI framework, for the same
// reason as the TS: this binary holds a token that reaches a firm's whole
// client list. golang.org/x/term (the Go team's) supplies raw mode and the
// window size, which Node had built in; that's the only dependency.
package tui

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/grubless/grubless-cli/internal/jsstr"
	"golang.org/x/term"
)

const esc = "\x1b"

// ANSI sequences.
const (
	altScreenOn  = esc + "[?1049h"
	altScreenOff = esc + "[?1049l"
	hideCursor   = esc + "[?25l"
	showCursor   = esc + "[?25h"
	clearScreen  = esc + "[2J"
	reset        = esc + "[0m"
	bold         = esc + "[1m"
	dim          = esc + "[2m"
	reverse      = esc + "[7m"
	red          = esc + "[31m"
	green        = esc + "[32m"
	yellow       = esc + "[33m"
	cyan         = esc + "[36m"
	clearLine    = esc + "[2K"
)

func moveTo(row, col int) string { return fmt.Sprintf("%s[%d;%dH", esc, row, col) }

// Key is one decoded keypress.
type Key struct {
	Name string
	Ctrl bool
}

var (
	csiTerminator = regexp.MustCompile(`^\x1b\[[0-9;]*([~a-zA-Z])`)
	csiNumeric    = regexp.MustCompile(`^\x1b\[(\d+)~`)
	ansiEscape    = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)
)

var arrowKeys = map[byte]string{'A': "up", 'B': "down", 'C': "right", 'D': "left", 'H': "home", 'F': "end"}

// DecodeKeys turns a chunk of raw stdin into key events. A paste or a fast
// typist can deliver several at once, and an unrecognised escape sequence is
// consumed whole rather than read as the keys its bytes happen to spell.
//
// The decisions below follow the TS exactly, including its quirks — a bare
// `ESC [` decodes as escape-dropped then "[" — because a key that does
// something different in the two builds is a bug report waiting to happen.
func DecodeKeys(chunk string) []Key {
	var keys []Key
	for i := 0; i < len(chunk); {
		rest := chunk[i:]

		if strings.HasPrefix(rest, esc+"[") || strings.HasPrefix(rest, esc+"O") {
			if len(rest) > 2 {
				if name, ok := arrowKeys[rest[2]]; ok {
					keys = append(keys, Key{Name: name})
					i += 3
					continue
				}
			}
			// Sequences like ESC[5~: consume to the terminator.
			if m := csiTerminator.FindString(rest); m != "" {
				if n := csiNumeric.FindStringSubmatch(rest); n != nil {
					switch n[1] {
					case "5":
						keys = append(keys, Key{Name: "pageup"})
					case "6":
						keys = append(keys, Key{Name: "pagedown"})
					}
				}
				i += len(m)
				continue
			}
			i++
			continue
		}

		r, size := utf8.DecodeRuneInString(rest)
		switch {
		case r == 0x1b:
			keys = append(keys, Key{Name: "escape"})
		case r == '\r' || r == '\n':
			keys = append(keys, Key{Name: "return"})
		case r == '\t':
			keys = append(keys, Key{Name: "tab"})
		case r == 0x7f || r == '\b':
			keys = append(keys, Key{Name: "backspace"})
		case r == 0x03:
			keys = append(keys, Key{Name: "c", Ctrl: true})
		case r == 0x04:
			keys = append(keys, Key{Name: "d", Ctrl: true})
		case r == 0x15:
			// Ctrl-U: clear a field, as in a shell. Go only; the TS drops it.
			keys = append(keys, Key{Name: "u", Ctrl: true})
		case r >= ' ':
			keys = append(keys, Key{Name: string(r)})
		}
		i += size
	}
	return keys
}

// StripAnsi removes CSI escape sequences.
func StripAnsi(text string) string { return ansiEscape.ReplaceAllString(text, "") }

// VisibleWidth ignores escapes, which occupy no columns, and counts UTF-16
// units as the TS does.
func VisibleWidth(text string) int { return jsstr.Len(StripAnsi(text)) }

// Truncate cuts to `width` visible columns without splitting an escape
// sequence, which would spill raw bytes and stick the terminal's colour.
func Truncate(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if VisibleWidth(text) <= width {
		return text
	}
	var out strings.Builder
	visible := 0
	for i := 0; i < len(text) && visible < width-1; {
		if m := ansiEscape.FindStringIndex(text[i:]); m != nil && m[0] == 0 {
			out.WriteString(text[i : i+m[1]])
			i += m[1]
			continue
		}
		r, size := utf8.DecodeRuneInString(text[i:])
		if utf16.RuneLen(r) == 2 && visible+2 > width-1 {
			// The TS appends one UTF-16 unit at a time, so only the high
			// surrogate fits — which Node prints as U+FFFD.
			out.WriteRune(utf8.RuneError)
			visible++
			break
		}
		out.WriteString(text[i : i+size])
		visible += utf16.RuneLen(r)
		i += size
	}
	return out.String() + "…" + reset
}

// Pad right-pads to a visible width.
func Pad(text string, width int) string {
	if gap := width - VisibleWidth(text); gap > 0 {
		return text + strings.Repeat(" ", gap)
	}
	return text
}

// Terminal owns raw mode, the alternate screen and painting. Its one hard
// requirement: restore the terminal however the program exits.
type Terminal struct {
	in, out       *os.File
	saved         *term.State
	closed        bool
	previousFrame []string
	// base is the theme's background and text colour, applied before each
	// row is cleared so the clear paints the whole row in it. Empty for the
	// terminal's own colours.
	base string
}

func NewTerminal() *Terminal { return &Terminal{in: os.Stdin, out: os.Stdout} }

// Width and Height have floors, so a zero-size report can't make every
// truncate() return nothing.
func (t *Terminal) Width() int {
	w, _, err := term.GetSize(int(t.out.Fd()))
	if err != nil {
		w = 80
	}
	return max(w, 20)
}

func (t *Terminal) Height() int {
	_, h, err := term.GetSize(int(t.out.Fd()))
	if err != nil {
		h = 24
	}
	return max(h, 6)
}

// Open enters raw mode and the alternate screen.
//
// x/term's raw mode also turns off output post-processing, which Node's
// doesn't. Nothing here relies on it: every line is placed with an explicit
// cursor move, never a newline.
func (t *Terminal) Open(base string) error {
	t.base = base
	t.out.WriteString(altScreenOn + hideCursor + base + clearScreen)
	state, err := term.MakeRaw(int(t.in.Fd()))
	if err != nil {
		t.out.WriteString(showCursor + altScreenOff + reset)
		return err
	}
	t.saved = state
	return nil
}

// Close restores the terminal. Safe to call more than once, and from any
// exit path.
func (t *Terminal) Close() {
	if t.closed {
		return
	}
	t.closed = true
	if t.saved != nil {
		_ = term.Restore(int(t.in.Fd()), t.saved)
	}
	t.out.WriteString(showCursor + altScreenOff + reset)
}

// SetBase switches the theme's background: the whole screen is cleared to
// it and the next frame is painted in full.
func (t *Terminal) SetBase(base string) {
	t.base = base
	t.out.WriteString(reset + base + clearScreen)
	t.previousFrame = nil
}

// InvalidateFrame forces a full repaint — after a resize, the previous
// frame's line positions no longer mean anything.
func (t *Terminal) InvalidateFrame() { t.previousFrame = nil }

// Render paints a frame, writing only the lines that changed: a full redraw
// flickers, and over slow SSH it visibly lags.
func (t *Terminal) Render(lines []string) {
	if t.closed {
		return
	}
	height := t.Height()
	frame := make([]string, height)
	copy(frame, lines)

	var out strings.Builder
	for row, line := range frame {
		if row < len(t.previousFrame) && t.previousFrame[row] == line {
			continue
		}
		out.WriteString(moveTo(row+1, 1) + t.base + clearLine + line + reset)
	}
	if out.Len() > 0 {
		t.out.WriteString(out.String())
	}
	t.previousFrame = frame
}
