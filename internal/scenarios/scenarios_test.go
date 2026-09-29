// Package scenarios runs the built binary end to end against a stub API and
// compares everything a user or a script can observe — stdout, stderr, the
// exit code, the requests sent, the config file, every file --out writes —
// with a recording in testdata/.
//
// The recordings began as the Node build's behaviour: the Go port was held
// byte for byte to it by a harness that ran both builds side by side, and
// before the Node build was removed this package was run against both and
// both matched every recording. They're the CLI's behaviour now; change one
// only on purpose, with
//
//	go test ./internal/scenarios -update
//
// and read the diff before committing it.
//
// GRUBLESS_CLI_BIN runs the scenarios against another build of the CLI
// instead of building this one. The --wait scenarios take a few seconds
// each; -short skips them.
package scenarios

import (
	"crypto/sha256"
	"flag"
	"fmt"
	"io/fs"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

var update = flag.Bool("update", false, "rewrite the recordings in testdata/ from this build")

// recordedNow pins the clock (see internal/clock) for anything date-relative,
// such as `portfolio --range 1w`. It's the day the recordings were checked
// against the Node build, which had no way to be pinned.
const recordedNow = "2026-09-29T12:00:00Z"

var bin string

func TestMain(m *testing.M) {
	flag.Parse()
	if b := os.Getenv("GRUBLESS_CLI_BIN"); b != "" {
		bin = b
		os.Exit(m.Run())
	}
	dir, err := os.MkdirTemp("", "grubless-scenarios-")
	if err != nil {
		panic(err)
	}
	bin = filepath.Join(dir, "grubless")
	build := exec.Command("go", "build", "-o", bin, "github.com/grubless/grubless-cli/cmd/grubless")
	build.Stdout, build.Stderr = os.Stderr, os.Stderr
	if err := build.Run(); err != nil {
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type scenario struct {
	name string
	args []string
	env  map[string]string
	// out gives the run a fresh --out directory, substituted for {out}, and
	// records what's written under it.
	out  bool
	slow bool
}

var ok = map[string]string{"GRUBLESS_TOKEN": "grb_ok"}

func token(t string) map[string]string { return map[string]string{"GRUBLESS_TOKEN": t} }

func scenarios(csvFile, dir string) []scenario {
	return []scenario{
		// Basics and argument handling
		{name: "version", args: []string{"--version"}},
		{name: "help", args: []string{"--help"}},
		{name: "no command, stdin not a tty"},
		{name: "unknown command, signed in", args: []string{"nonsense"}, env: ok},
		{name: "unknown command, signed out", args: []string{"nonsense"}},
		{name: "unknown flag", args: []string{"entities", "list", "--nope"}, env: ok},
		{name: "flag missing its value", args: []string{"holdings", "--entity"}, env: ok},
		{name: "flag value looks like a flag", args: []string{"holdings", "--entity", "--json"}, env: ok},
		{name: "boolean given a value", args: []string{"entities", "list", "--json=yes"}, env: ok},
		{name: "bad --timeout", args: []string{"sources", "sync", "--timeout", "soon"}, env: ok},
		{name: "hex --timeout accepted", args: []string{"sources", "list", "--entity", "acme", "--timeout", "0x10"}, env: ok},
		{name: "no credential", args: []string{"entities", "list"}},
		{name: "401", args: []string{"entities", "list"}, env: token("grb_401")},
		{name: "402", args: []string{"entities", "list"}, env: token("grb_402")},
		{name: "403 read-only", args: []string{"entities", "list"}, env: token("grb_403ro")},
		{name: "403 role", args: []string{"entities", "list"}, env: token("grb_403")},
		{name: "404", args: []string{"entities", "list"}, env: token("grb_404")},
		{name: "400 zod", args: []string{"entities", "list"}, env: token("grb_400")},
		{name: "502 html", args: []string{"entities", "list"}, env: token("grb_502")},
		{name: "server requires newer cli", args: []string{"entities", "list"}, env: token("grb_oldcli")},
		{name: "unreachable host", args: []string{"entities", "list", "--api-url", "http://127.0.0.1:1"}, env: ok},

		// entities / auth
		{name: "entities list", args: []string{"entities", "list"}, env: ok},
		{name: "entities list --json", args: []string{"entities", "list", "--json"}, env: ok},
		{name: "entities list, empty", args: []string{"entities", "list"}, env: token("grb_empty")},
		{name: "entities unknown sub", args: []string{"entities", "delete"}, env: ok},
		{name: "whoami", args: []string{"auth", "whoami"}, env: ok},
		{name: "whoami --json", args: []string{"auth", "whoami", "--json"}, env: ok},
		{name: "whoami, token listing fails", args: []string{"auth", "whoami", "--json"}, env: token("grb_empty")},
		{name: "login without a tty", args: []string{"auth", "login"}},
		{name: "login with a rejected token", args: []string{"auth", "login", "--token", "grb_401"}},

		// entity resolution
		{name: "resolve by prefix", args: []string{"holdings", "--entity", "acme"}, env: ok},
		{name: "resolve by exact name, case-insensitive", args: []string{"holdings", "--entity", "ACME TRADING PTY LTD"}, env: ok},
		{name: "resolve by uuid", args: []string{"holdings", "--entity", soc}, env: ok},
		{name: "resolve unknown", args: []string{"holdings", "--entity", "Nonexistent"}, env: ok},
		{name: "resolve ambiguous", args: []string{"holdings", "--entity", ""}, env: ok},
		{name: "scope required", args: []string{"holdings"}, env: ok},
		{name: "all-entities with none", args: []string{"holdings", "--all-entities"}, env: token("grb_empty")},

		// sources
		{name: "sources list", args: []string{"sources", "list", "--entity", "acme"}, env: ok},
		{name: "sources (no sub)", args: []string{"sources", "--entity", "acme"}, env: ok},
		{name: "sources list --all-entities", args: []string{"sources", "list", "--all-entities"}, env: ok},
		{name: "sources list --json", args: []string{"sources", "list", "--all-entities", "--json"}, env: ok},
		{name: "sources list, none", args: []string{"sources", "list", "--entity", "soc"}, env: ok},
		{name: "sources unknown sub", args: []string{"sources", "frob", "--entity", "acme"}, env: ok},
		{name: "sync needs a target", args: []string{"sources", "sync", "--entity", "acme"}, env: ok},
		{name: "sync unknown source", args: []string{"sources", "sync", "--entity", "acme", "--source", "nope"}, env: ok},
		{name: "sync one source by label", args: []string{"sources", "sync", "--entity", "acme", "--source", "Kraken"}, env: ok},
		{name: "sync --all --full --json", args: []string{"sources", "sync", "--entity", "acme", "--all", "--full", "--json"}, env: ok},
		{name: "sync --all-entities --all --json", args: []string{"sources", "sync", "--all-entities", "--all", "--json"}, env: ok},
		{name: "sync --wait", args: []string{"sources", "sync", "--entity", "acme", "--source", "src-kraken", "--wait"}, env: ok, slow: true},
		{name: "sync --wait --json", args: []string{"sources", "sync", "--entity", "acme", "--all", "--wait", "--json"}, env: ok, slow: true},
		{name: "sync --wait, job fails", args: []string{"sources", "sync", "--entity", "acme", "--all", "--wait"}, env: token("grb_ok_fail"), slow: true},
		{name: "sync --wait, job degraded", args: []string{"sources", "sync", "--entity", "acme", "--all", "--wait"}, env: token("grb_ok_degraded"), slow: true},
		{name: "sync --wait times out", args: []string{"sources", "sync", "--entity", "acme", "--all", "--wait", "--timeout", "0.03"}, env: ok, slow: true},

		// import
		{name: "import needs a file", args: []string{"import"}, env: ok},
		{name: "import needs --source", args: []string{"import", csvFile, "--entity", "acme"}, env: ok},
		{name: "import needs --entity", args: []string{"import", csvFile, "--source", "src-csv"}, env: ok},
		{name: "import missing file", args: []string{"import", "/nonexistent/x.csv", "--entity", "acme", "--source", "src-csv"}, env: ok},
		{name: "import a directory", args: []string{"import", dir, "--entity", "acme", "--source", "src-csv"}, env: ok},
		{name: "import", args: []string{"import", csvFile, "--entity", "acme", "--source", "src-csv", "--json"}, env: ok},
		{name: "import --wait --json", args: []string{"import", csvFile, "--entity", "acme", "--source", "src-csv", "--wait", "--json"}, env: ok, slow: true},

		// holdings / tax / warnings
		{name: "holdings", args: []string{"holdings", "--entity", "acme"}, env: ok},
		{name: "holdings --all-entities", args: []string{"holdings", "--all-entities"}, env: ok},
		{name: "holdings --json", args: []string{"holdings", "--all-entities", "--json"}, env: ok},
		{name: "tax-summary", args: []string{"tax-summary", "--entity", "acme"}, env: ok},
		{name: "tax-summary --year", args: []string{"tax-summary", "--entity", "acme", "--year", "2024"}, env: ok},
		{name: "tax-summary unknown year", args: []string{"tax-summary", "--entity", "acme", "--year", "1999"}, env: ok},
		{name: "tax-summary --all-entities", args: []string{"tax-summary", "--all-entities"}, env: ok},
		{name: "tax-summary --json", args: []string{"tax-summary", "--entity", "acme", "--json"}, env: ok},
		{name: "warnings", args: []string{"warnings", "--entity", "acme"}, env: ok},
		{name: "warnings --fail-on-blocking", args: []string{"warnings", "--entity", "acme", "--fail-on-blocking"}, env: ok},
		{name: "warnings clean entity gate", args: []string{"warnings", "--entity", "soc", "--fail-on-blocking"}, env: ok},
		{name: "warnings --all-entities", args: []string{"warnings", "--all-entities"}, env: ok},
		{name: "warnings --json (one)", args: []string{"warnings", "--entity", "acme", "--json"}, env: ok},
		{name: "warnings --json (all)", args: []string{"warnings", "--all-entities", "--json"}, env: ok},

		// portfolio
		{name: "portfolio", args: []string{"portfolio", "--entity", "acme"}, env: ok},
		{name: "portfolio --range fy", args: []string{"portfolio", "--entity", "acme", "--range", "fy"}, env: ok},
		{name: "portfolio --range 1w", args: []string{"portfolio", "--entity", "acme", "--range", "1w"}, env: ok},
		{name: "portfolio no history", args: []string{"portfolio", "--entity", "soc"}, env: ok},
		{name: "portfolio --range fy, no settings", args: []string{"portfolio", "--entity", "soc", "--range", "fy"}, env: ok},
		{name: "portfolio bad range", args: []string{"portfolio", "--entity", "acme", "--range", "2w"}, env: ok},
		{name: "portfolio --all-entities", args: []string{"portfolio", "--all-entities"}, env: ok},
		{name: "portfolio --json", args: []string{"portfolio", "--entity", "acme", "--range", "1m", "--json"}, env: ok},
		{name: "portfolio --json --all-entities", args: []string{"portfolio", "--all-entities", "--range", "3m", "--json"}, env: ok},

		// reports
		{name: "report, none named", args: []string{"report"}, env: ok},
		{name: "report, two named", args: []string{"report", "fees", "income"}, env: ok},
		{name: "report unknown", args: []string{"report", "nope", "--entity", "acme", "--year", "2025"}, env: ok},
		{name: "report needs --year", args: []string{"report", "income", "--entity", "acme"}, env: ok},
		{name: "report yearless", args: []string{"report", "balances-per-source", "--entity", "acme"}, env: ok},
		{name: "report --json on a PDF", args: []string{"report", "ato-mytax", "--entity", "acme", "--year", "2025", "--json"}, env: ok},
		{name: "report --json on the bundle", args: []string{"report", "bundle", "--entity", "acme", "--year", "2025", "--json"}, env: ok},
		{name: "report --json with --out", args: []string{"report", "income", "--entity", "acme", "--year", "2025", "--json", "--out", "x"}, env: ok},
		{name: "report --all-entities needs a target", args: []string{"report", "income", "--all-entities", "--year", "2025"}, env: ok},
		{name: "report to stdout", args: []string{"report", "capital-gains", "--entity", "acme", "--year", "2025"}, env: ok},
		{name: "report binary to stdout", args: []string{"report", "bundle", "--entity", "acme", "--year", "2025"}, env: ok},
		{name: "report --json", args: []string{"report", "capital-gains", "--entity", "acme", "--year", "2025", "--json"}, env: ok},
		{name: "report --json, prose note", args: []string{"report", "gifts-donations-lost", "--entity", "acme", "--year", "2025", "--json"}, env: ok},
		{name: "report --json, empty body", args: []string{"report", "other-gains", "--entity", "acme", "--year", "2025", "--json"}, env: ok},
		{name: "report --json yearless", args: []string{"report", "balances-per-source", "--entity", "acme", "--year", "2025", "--json"}, env: ok},
		{name: "report --json --all-entities, one fails", args: []string{"report", "income", "--all-entities", "--year", "2025", "--json"}, env: ok},
		{name: "report --out file", args: []string{"report", "capital-gains", "--entity", "acme", "--year", "2025", "--out", "{out}/cg.csv"}, env: ok, out: true},
		{name: "report --out missing dir", args: []string{"report", "capital-gains", "--entity", "acme", "--year", "2025", "--out", "{out}/nope/cg.csv"}, env: ok, out: true},
		{name: "report --out is a directory", args: []string{"report", "capital-gains", "--entity", "acme", "--year", "2025", "--out", "{out}"}, env: ok, out: true},
		{name: "report --out --all-entities", args: []string{"report", "capital-gains", "--all-entities", "--year", "2025", "--out", "{out}/clients"}, env: ok, out: true},
		{name: "report bundle --out --all-entities", args: []string{"report", "bundle", "--all-entities", "--year", "2025", "--out", "{out}"}, env: ok, out: true},
		{name: "report, percent-encoded filename", args: []string{"report", "fees", "--all-entities", "--year", "2025", "--out", "{out}"}, env: ok, out: true},
		{name: "report, malformed filename", args: []string{"report", "expenses", "--all-entities", "--year", "2025", "--out", "{out}"}, env: ok, out: true},
		// Path traversal: none of these may write outside --out.
		{name: "report, server filename climbs out", args: []string{"report", "highest-balance", "--all-entities", "--year", "2025", "--out", "{out}"}, env: ok, out: true},
		{name: "report, server filename climbs out, encoded", args: []string{"report", "end-of-year-holdings", "--all-entities", "--year", "2025", "--out", "{out}"}, env: ok, out: true},
		{name: "report, server filename climbs out, backslashes", args: []string{"report", "beginning-of-year-holdings", "--all-entities", "--year", "2025", "--out", "{out}"}, env: ok, out: true},
		{name: "report, server filename is ..", args: []string{"report", "division-70-trading-stock", "--all-entities", "--year", "2025", "--out", "{out}"}, env: ok, out: true},
		{name: "report, entity named ..", args: []string{"report", "capital-gains", "--all-entities", "--year", "2025", "--out", "{out}"}, env: token("grb_ok_dots"), out: true},
		{name: "report, one entity fails", args: []string{"report", "income", "--all-entities", "--year", "2025", "--out", "{out}"}, env: ok, out: true},
		{name: "report, 402 stops the run", args: []string{"report", "income", "--all-entities", "--year", "2025", "--out", "{out}"}, env: token("grb_402"), out: true},
		{name: "report, 401 is per-entity", args: []string{"report", "income", "--entity", "acme", "--year", "2025"}, env: token("grb_401")},

		// transactions (the Go build's own; recorded from it alone)
		{name: "transactions", args: []string{"transactions", "--entity", "acme", "--limit", "5"}, env: ok},
		{name: "transactions filtered", args: []string{"transactions", "--entity", "acme", "--category", "transfer", "--direction", "out", "--limit", "all"}, env: ok},
		{name: "transactions --json paged", args: []string{"transactions", "--entity", "acme", "--limit", "120", "--sort", "asc", "--json"}, env: ok},
		{name: "transactions bad direction", args: []string{"transactions", "--entity", "acme", "--direction", "up"}, env: ok},

		// interactive, without a terminal
		{name: "tui refuses a pipe", env: ok},
	}
}

// result is everything a scenario can observe.
type result struct {
	code           int
	stdout, stderr []byte
	files          map[string][]byte
	requests       []string
}

func run(t *testing.T, api *stub, url string, args []string, env map[string]string, home string) result {
	t.Helper()
	needsAPI := true
	for _, a := range args {
		if strings.HasPrefix(a, "--api-url") {
			needsAPI = false
		}
	}
	if needsAPI {
		args = append(append([]string{}, args...), "--api-url", url)
	}
	cmd := exec.Command(bin, args...)
	// PATH emptied: no keychain tool can be reached, so a login can never
	// touch the developer's own.
	cmd.Env = []string{"PATH=", "HOME=" + home, "XDG_CONFIG_HOME=" + home, "NO_COLOR=1", "TZ=UTC", "GRUBLESS_NOW=" + recordedNow}
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatalf("running %v: %v", args, err)
	}
	api.mu.Lock()
	requests := append([]string{}, api.posts...)
	api.mu.Unlock()
	return result{code: code, stdout: []byte(stdout.String()), stderr: []byte(stderr.String()), requests: requests}
}

var transportError = regexp.MustCompile(`(Could not reach \S+): .*`)

// render is a result as its recording: stable across runs and machines.
func render(r result, replace [][2]string) string {
	norm := func(b []byte) string {
		if !utf8.Valid(b) {
			return fmt.Sprintf("<%d bytes, sha256 %x>\n", len(b), sha256.Sum256(b))
		}
		s := string(b)
		for _, pair := range replace {
			if pair[0] != "" {
				s = strings.ReplaceAll(s, pair[0], pair[1])
			}
		}
		// Node said "fetch failed" and Go names the syscall; both name the
		// host, which is what the message is for.
		return transportError.ReplaceAllString(s, "$1: <transport error>")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "exit %d\n--- stdout\n%s--- stderr\n%s", r.code, norm(r.stdout), norm(r.stderr))
	if len(r.requests) > 0 {
		b.WriteString("--- requests\n")
		for _, req := range r.requests {
			b.WriteString(norm([]byte(req)) + "\n")
		}
	}
	if r.files != nil {
		b.WriteString("--- files\n")
		var names []string
		for name := range r.files {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			fmt.Fprintf(&b, "%s  %d bytes  sha256 %x\n", name, len(r.files[name]), sha256.Sum256(r.files[name]))
		}
	}
	return b.String()
}

func slug(name string) string {
	s := strings.ToLower(regexp.MustCompile(`[^A-Za-z0-9]+`).ReplaceAllString(name, "-"))
	return strings.Trim(s, "-")
}

// check compares a rendering with its recording, or rewrites it.
func check(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", slug(name)+".txt")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no recording (%v) — run with -update and review it", err)
	}
	if string(want) != got {
		t.Errorf("differs from %s\n--- got\n%s\n--- want\n%s", path, got, want)
	}
}

func TestScenarios(t *testing.T) {
	api := &stub{}
	srv := httptest.NewServer(api)
	defer srv.Close()

	in := t.TempDir()
	csvFile := filepath.Join(in, "trades.csv")
	// A BOM, a non-ASCII name, and a byte that isn't UTF-8.
	if err := os.WriteFile(csvFile, []byte("\uFEFFdate,amount\n2025-01-01,1.5\nSociété,\xff\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()

	for _, sc := range scenarios(csvFile, dir) {
		t.Run(sc.name, func(t *testing.T) {
			if sc.slow && testing.Short() {
				t.Skip("--wait scenarios poll for a few seconds; skipped under -short")
			}
			api.reset()
			home := t.TempDir()
			// --out sits three levels inside a sandbox, and the whole
			// sandbox is collected: a file that climbs out of --out lands
			// where the check below sees it.
			sandbox := t.TempDir()
			out := filepath.Join(sandbox, "a", "b", "out")
			if err := os.MkdirAll(out, 0o755); err != nil {
				t.Fatal(err)
			}
			args := make([]string, len(sc.args))
			for i, a := range sc.args {
				args[i] = strings.ReplaceAll(a, "{out}", out)
			}
			r := run(t, api, srv.URL, args, sc.env, home)
			if sc.out {
				r.files = map[string][]byte{}
				filepath.WalkDir(sandbox, func(p string, d fs.DirEntry, err error) error {
					if err == nil && !d.IsDir() {
						rel, _ := filepath.Rel(sandbox, p)
						r.files[filepath.ToSlash(rel)], _ = os.ReadFile(p)
					}
					return nil
				})
				for name := range r.files {
					if !strings.HasPrefix(name, "a/b/out/") {
						t.Errorf("wrote outside --out: %s", name)
					}
				}
			}
			api.mu.Lock()
			queuedAt := api.syncStartedAt
			api.mu.Unlock()
			check(t, sc.name, render(r, [][2]string{
				{out, "<out>"}, {home, "<home>"}, {in, "<in>"}, {dir, "<dir>"},
				{srv.URL, "<api>"}, {queuedAt, "<queued-at>"},
			}))
		})
	}
}

// TestSignInSequence is state carried between runs: a login writes the
// config file, later commands read it, a logout clears it.
func TestSignInSequence(t *testing.T) {
	api := &stub{}
	srv := httptest.NewServer(api)
	defer srv.Close()
	home := t.TempDir()
	config := filepath.Join(home, "grubless", "config.json")

	steps := []struct {
		name string
		args []string
		// before runs first: the sequence's one outside change.
		before func()
	}{
		{name: "login", args: []string{"auth", "login", "--token", "grb_ok"}},
		{name: "entities list from the file", args: []string{"entities", "list"}},
		{name: "whoami from the file", args: []string{"auth", "whoami", "--json"}},
		{name: "world-readable config warns", args: []string{"entities", "list"}, before: func() { os.Chmod(config, 0o644) }},
		{name: "logout", args: []string{"auth", "logout"}},
		{name: "signed out after logout", args: []string{"entities", "list"}},
	}
	var b strings.Builder
	for _, step := range steps {
		if step.before != nil {
			step.before()
		}
		api.reset()
		r := run(t, api, srv.URL, step.args, nil, home)
		file, err := os.ReadFile(config)
		if err != nil {
			file = []byte("<none>\n")
		}
		fmt.Fprintf(&b, "=== %s\n%s--- config.json\n%s\n", step.name,
			render(r, [][2]string{{home, "<home>"}, {srv.URL, "<api>"}}),
			strings.ReplaceAll(string(file), srv.URL, "<api>"))
	}
	check(t, "sign-in sequence", b.String())
}
