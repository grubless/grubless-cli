//go:build linux

package scenarios

import (
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// The CLI on a real terminal — what a pipe can't show: colour output, and
// the interactive interface driven key by key. Linux only, for the pty.

const ttyCols, ttyRows = 100, 30

// pty is a pseudo-terminal with the binary running on its far side.
type pty struct {
	t      *testing.T
	master *os.File
	cmd    *exec.Cmd
	output chan []byte
}

func openPty(t *testing.T, env []string, args ...string) *pty {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal here: %v", err)
	}
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	n, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	slave, err := os.OpenFile("/dev/pts/"+strconv.Itoa(n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer slave.Close()
	if err := unix.IoctlSetWinsize(int(slave.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: ttyRows, Col: ttyCols}); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin, args...)
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := &pty{t: t, master: master, cmd: cmd, output: make(chan []byte, 256)}
	go func() {
		buf := make([]byte, 65536)
		for {
			n, err := master.Read(buf)
			if n > 0 {
				p.output <- append([]byte(nil), buf[:n]...)
			}
			if err != nil {
				close(p.output)
				return
			}
		}
	}()
	return p
}

// read is everything written until it's been quiet for `idle`.
func (p *pty) read(idle time.Duration) []byte {
	var out []byte
	deadline := time.After(30 * time.Second)
	for {
		select {
		case b, ok := <-p.output:
			if !ok {
				return out
			}
			out = append(out, b...)
		case <-time.After(idle):
			return out
		case <-deadline:
			return out
		}
	}
}

func (p *pty) write(keys string) {
	if _, err := p.master.WriteString(keys); err != nil {
		p.t.Fatal(err)
	}
}

func (p *pty) wait() int {
	done := make(chan error, 1)
	go func() { done <- p.cmd.Wait() }()
	select {
	case err := <-done:
		p.master.Close()
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode()
		}
		return 0
	case <-time.After(10 * time.Second):
		p.cmd.Process.Kill()
		p.t.Fatal("the binary didn't exit")
		return -1
	}
}

// screen is just enough VT100 for the TUI, which only moves the cursor,
// clears lines and writes text.
type screen struct {
	grid     [ttyRows][ttyCols]rune
	row, col int
}

var csi = regexp.MustCompile(`^\x1b\[(\??)([0-9;]*)([A-Za-z])`)

func newScreen() *screen {
	s := &screen{}
	s.clear()
	return s
}

func (s *screen) clear() {
	for r := range s.grid {
		for c := range s.grid[r] {
			s.grid[r][c] = ' '
		}
	}
	s.row, s.col = 0, 0
}

func (s *screen) feed(b []byte) {
	text := string(b)
	for i := 0; i < len(text); {
		if m := csi.FindStringSubmatch(text[i:]); m != nil {
			switch {
			case m[3] == "H":
				parts := strings.Split(m[2]+";;", ";")
				r, _ := strconv.Atoi(parts[0])
				c, _ := strconv.Atoi(parts[1])
				s.row, s.col = max(r, 1)-1, max(c, 1)-1
			case m[3] == "K" && m[2] == "2" && s.row < ttyRows:
				for c := range s.grid[s.row] {
					s.grid[s.row][c] = ' '
				}
			case m[3] == "J" && m[2] == "2", m[3] == "h" && m[1] == "?" && m[2] == "1049":
				s.clear()
			}
			i += len(m[0])
			continue
		}
		r, size := []rune(text[i:])[0], len(string([]rune(text[i:])[0]))
		switch {
		case r == '\r':
			s.col = 0
		case r == '\n':
			s.row++
		case r >= ' ' && s.row < ttyRows && s.col < ttyCols:
			s.grid[s.row][s.col] = r
			s.col++
		}
		i += size
	}
}

func (s *screen) text() string {
	lines := make([]string, ttyRows)
	for r := range s.grid {
		lines[r] = strings.TrimRight(string(s.grid[r][:]), " ")
	}
	return strings.Join(lines, "\n")
}

func ttyEnv(home, api string, extra ...string) []string {
	return append([]string{"PATH=", "HOME=" + home, "XDG_CONFIG_HOME=" + home, "TZ=UTC", "TERM=xterm-256color",
		"GRUBLESS_TOKEN=grb_ok", "GRUBLESS_API_URL=" + api, "GRUBLESS_NOW=" + recordedNow}, extra...)
}

// TestColourOutput records the commands' output on a terminal, where they
// colour it — and where the TS once counted those escape codes in its column
// widths, a quirk the tables still reproduce.
func TestColourOutput(t *testing.T) {
	api := &stub{}
	srv := httptest.NewServer(api)
	defer srv.Close()

	for _, args := range [][]string{
		{"holdings", "--entity", "acme"},
		{"sources", "list", "--all-entities"},
		{"warnings", "--entity", "acme"},
		{"tax-summary", "--all-entities"},
		{"portfolio", "--entity", "acme", "--range", "1y"},
		{"entities", "list"},
		{"auth", "whoami"},
	} {
		name := "tty " + strings.Join(args, " ")
		t.Run(name, func(t *testing.T) {
			api.reset()
			home := t.TempDir()
			p := openPty(t, ttyEnv(home, srv.URL), args...)
			out := p.read(time.Second)
			code := p.wait()
			var b strings.Builder
			fmt.Fprintf(&b, "exit %d\n", code)
			// Quoted line by line, so every escape code is in the recording.
			for _, line := range strings.SplitAfter(strings.ReplaceAll(string(out), home, "<home>"), "\n") {
				if line != "" {
					b.WriteString(strconv.Quote(strings.ReplaceAll(line, srv.URL, "<api>")) + "\n")
				}
			}
			check(t, name, b.String())
		})
	}
}

// TestTUISession drives the interface key by key and checks what each screen
// shows, then that the terminal is restored and the theme switch persists.
func TestTUISession(t *testing.T) {
	if testing.Short() {
		t.Skip("the TUI session waits for each screen to settle; skipped under -short")
	}
	api := &stub{}
	srv := httptest.NewServer(api)
	defer srv.Close()
	home := t.TempDir()

	steps := []struct {
		name   string
		keys   string
		settle time.Duration
		expect []string
	}{
		// Cypherpunk by default, on a 256-colour terminal with no choice made.
		{"load", "", 1500 * time.Millisecond, []string{"GRUBLESS█", "Entities", "Acme Trading Pty Ltd"}},
		// The overview's cards. At 100 columns the breakdown stands down and
		// the stat cards stay; the extras load after the rest.
		{"open Acme", "\r", 2 * time.Second, []string{"1 Overview", "Portfolio value", "Connected sources", "Transactions", "1,234", "Price coverage", "412 / 415", "3 missing"}},
		{"transactions tab", "6", 800 * time.Millisecond, []string{"┌─ Transactions ─", "6 Transactions", "DATE (UTC)", "2026-09-29 01:30", "1.5 SOL", "340 USDC", "+100.13", "transfer (internal)*", "100+ rows"}},
		{"second row", "j", 500 * time.Millisecond, nil},
		{"open it", "\r", 800 * time.Millisecond, []string{"00000001-1d7e-4d0a-9d1b-3c1f2b8e9a01", "Rebalance after the audit", "jupiter", "COST BASIS", "fee", "2 of 100+"}},
		{"next one", "j", 500 * time.Millisecond, []string{"00000002-1d7e-4d0a-9d1b-3c1f2b8e9a01", "3 of 100+"}},
		{"close", "\x1b", 500 * time.Millisecond, []string{"DATE (UTC)"}},
		{"open the filter", "f", 500 * time.Millisecond, []string{"Filter transactions", "Clear all filters", "type to search"}},
		{"type a category", "tran", 500 * time.Millisecond, []string{"transfer█"}},
		{"Enter without completing: refused", "\r", 500 * time.Millisecond, []string{"did you mean transfer?"}},
		{"→ completes, then apply", "\x1b[C\r", time.Second, []string{"1 filter (f)", "30 rows", "transfer (internal)"}},
		{"clear all", "f" + strings.Repeat("\x1b[B", 8) + "\r", time.Second, []string{"100+ rows · f filter"}},
		{"to the end: pages in the rest", "\x1b[F", 1500 * time.Millisecond, []string{"150 rows"}},
		{"back", "q", 500 * time.Millisecond, []string{"select an entity"}},
		{"switch theme", "t", 500 * time.Millisecond, []string{"Grubless", "Theme: Terminal"}},
		{"quit", "q", 500 * time.Millisecond, nil},
	}

	p := openPty(t, ttyEnv(home, srv.URL))
	sc := newScreen()
	var raw []byte
	for _, step := range steps {
		if step.keys != "" {
			p.write(step.keys)
		}
		out := p.read(step.settle)
		raw = append(raw, out...)
		sc.feed(out)
		text := sc.text()
		for _, want := range step.expect {
			if !strings.Contains(text, want) {
				t.Errorf("%s: screen lacks %q:\n%s", step.name, want, text)
				break
			}
		}
	}
	raw = append(raw, p.read(300*time.Millisecond)...)
	if code := p.wait(); code != 0 {
		t.Errorf("exit %d", code)
	}
	tail := string(raw[max(0, len(raw)-40):])
	if !strings.Contains(tail, "\x1b[?25h") || !strings.Contains(tail, "\x1b[?1049l") {
		t.Errorf("the terminal wasn't restored: %q", tail)
	}

	// The switch was saved, and the next launch opens in it.
	config, _ := os.ReadFile(filepath.Join(home, "grubless", "config.json"))
	if !strings.Contains(string(config), `"theme": "terminal"`) {
		t.Errorf("theme not saved: %s", config)
	}
	again := openPty(t, ttyEnv(home, srv.URL))
	first := string(again.read(time.Second))
	again.write("q")
	again.wait()
	if !strings.Contains(first, "Grubless") || strings.Contains(first, "GRUBLESS") {
		t.Error("the next launch should open in the saved Terminal theme")
	}
}
