// Package e2e drives the built binary as a subprocess against a real,
// running Grubless API. The scenario tests (internal/scenarios) cover the CLI
// against a stub; this checks it against the API it's for, which is the
// only thing that catches a wire shape that moved.
//
// Skipped, loudly, unless GRUBLESS_E2E_API_URL is set:
//
//	GRUBLESS_E2E_API_URL=http://localhost:3000 go test ./internal/e2e -v
//
// It sets itself up through public routes only — register, create an
// entity, mint tokens — so any reachable deployment works. It leaves a
// throwaway account and one entity behind: there's no account-deletion route
// to clean up with. Point it at a stack you don't mind that happening to.
//
// The tests run in order in one function, because they share that account:
// the entity has no sources until the last test creates one, and several
// before it depend on that.
package e2e

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

type result struct {
	code           int
	stdout, stderr string
}

type env struct {
	t                *testing.T
	bin, api, home   string
	token, readToken string
	cookie, entityID string
}

func (e *env) run(args []string, extra ...string) result {
	e.t.Helper()
	cmd := exec.Command(e.bin, append(append([]string{}, args...), "--api-url", e.api)...)
	// Hermetic: the developer's own config is never read or written.
	cmd.Env = append(os.Environ(), "GRUBLESS_TOKEN="+e.token, "XDG_CONFIG_HOME="+e.home, "NO_COLOR=1")
	cmd.Env = append(cmd.Env, extra...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		e.t.Fatalf("running %v: %v", args, err)
	}
	return result{code, stdout.String(), stderr.String()}
}

// post sets up state as the throwaway user, failing with the server's own
// message: a setup step that silently didn't happen shows up thirty tests
// later as something unrelated.
func (e *env) post(path string, body any, into any) {
	e.t.Helper()
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", e.api+path, bytes.NewReader(raw))
	req.Header.Set("content-type", "application/json")
	if e.cookie != "" {
		req.Header.Set("cookie", e.cookie)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatalf("e2e setup: POST %s: %v", path, err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		var detail bytes.Buffer
		detail.ReadFrom(res.Body)
		hint := ""
		if res.StatusCode == 429 {
			// Auth routes are rate-limited per IP; a few runs in a row use
			// up the register budget.
			hint = " — auth rate limit hit; wait, or set AUTH_RATE_LIMIT_DISABLED=1 on the API"
		}
		e.t.Fatalf("e2e setup: POST %s → %d%s\n%s", path, res.StatusCode, hint, detail.String())
	}
	if path == "/auth/register" {
		e.cookie = strings.SplitN(res.Header.Get("set-cookie"), ";", 2)[0]
	}
	if into != nil {
		json.NewDecoder(res.Body).Decode(into)
	}
}

func jsonOf(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("stdout isn't JSON (%v):\n%s", err, s)
	}
	return v
}

func expect(t *testing.T, ok bool, format string, args ...any) {
	t.Helper()
	if !ok {
		t.Errorf(format, args...)
	}
}

func TestAgainstARealAPI(t *testing.T) {
	api := strings.TrimRight(os.Getenv("GRUBLESS_E2E_API_URL"), "/")
	if api == "" {
		t.Skip("GRUBLESS_E2E_API_URL is unset — skipping the tests that drive the built binary against a real API.\n" +
			"Run them with: GRUBLESS_E2E_API_URL=http://localhost:3000 go test ./internal/e2e -v")
	}

	dir := t.TempDir()
	bin := filepath.Join(dir, "grubless")
	build := exec.Command("go", "build", "-o", bin, "github.com/grubless/grubless-cli/cmd/grubless")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, bin: bin, api: api, home: filepath.Join(dir, "config")}

	// A fresh account per run, so the tests can assume an empty world.
	suffix := make([]byte, 6)
	rand.Read(suffix)
	e.post("/auth/register", map[string]string{"email": "cli-e2e-" + hex.EncodeToString(suffix) + "@example.com", "password": "test-password-12345"}, nil)
	var entity, write, read struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	e.post("/entities", map[string]string{"name": "Acme Trading Pty Ltd", "entityType": "company", "jurisdictionCode": "AU_ATO"}, &entity)
	e.post("/api-tokens", map[string]string{"name": "cli e2e", "scope": "write"}, &write)
	e.post("/api-tokens", map[string]string{"name": "cli e2e ro", "scope": "read"}, &read)
	e.entityID, e.token, e.readToken = entity.ID, write.Token, read.Token
	id := e.entityID

	t.Run("the binary", func(t *testing.T) {
		r := e.run([]string{"--version"})
		expect(t, r.code == 0 && regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(strings.TrimSpace(r.stdout)), "--version: %+v", r)
		r = e.run([]string{"--help"})
		expect(t, r.code == 0 && strings.Contains(r.stdout, "USAGE") && strings.Contains(r.stdout, "--fail-on-blocking"), "--help: %+v", r)
		expect(t, e.run([]string{"nonsense"}).code == 2, "unknown command should exit 2")
		expect(t, e.run([]string{"entities", "list", "--nope"}).code == 2, "unknown flag should exit 2")
	})

	t.Run("auth", func(t *testing.T) {
		r := e.run([]string{"entities", "list"}, "GRUBLESS_TOKEN=")
		expect(t, r.code == 3 && strings.Contains(r.stderr, "Not signed in"), "no credential: %+v", r)
		r = e.run([]string{"entities", "list"}, "GRUBLESS_TOKEN=grb_wrong")
		expect(t, r.code == 3 && strings.Contains(r.stderr, "auth login"), "rejected token: %+v", r)
	})

	t.Run("entities", func(t *testing.T) {
		r := e.run([]string{"entities", "list"})
		expect(t, r.code == 0 && strings.Contains(r.stdout, "Acme Trading Pty Ltd") && strings.Contains(r.stdout, "company"), "list: %+v", r)
		r = e.run([]string{"entities", "list", "--json"})
		list, _ := jsonOf(t, r.stdout).([]any)
		expect(t, r.code == 0 && len(list) == 1 && list[0].(map[string]any)["name"] == "Acme Trading Pty Ltd", "--json: %+v", r)
		expect(t, e.run([]string{"holdings", "--entity", "Acme"}).code == 0, "a name prefix should resolve")
		r = e.run([]string{"holdings", "--entity", "Nonexistent Ltd"})
		expect(t, r.code == 2 && strings.Contains(r.stderr, "No entity matching"), "unknown entity: %+v", r)
		r = e.run([]string{"holdings"})
		expect(t, r.code == 2 && strings.Contains(r.stderr, "--all-entities"), "no scope: %+v", r)
	})

	t.Run("warnings", func(t *testing.T) {
		// A new entity has no data, so nothing can block: a gate that fails
		// on an empty entity would be untrustworthy from day one.
		r := e.run([]string{"warnings", "--entity", id, "--fail-on-blocking"})
		expect(t, r.code == 0 && strings.Contains(r.stderr, "No blocking issues"), "gate: %+v", r)
		r = e.run([]string{"warnings", "--entity", id, "--json"})
		doc, _ := jsonOf(t, r.stdout).(map[string]any)
		var keys []string
		for k := range doc["categories"].(map[string]any) {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		expect(t, doc["blockingCount"] == 0.0 && strings.Join(keys, ",") == "unbalanced-transfers,uncategorized-transfers,unpriced-assets,zero-cost", "--json: %v", doc)
	})

	t.Run("reports", func(t *testing.T) {
		r := e.run([]string{"report", "made-up", "--entity", id, "--year", "2025"})
		expect(t, r.code == 2 && strings.Contains(r.stderr, "capital-gains"), "unknown report: %+v", r)
		r = e.run([]string{"report", "capital-gains", "--entity", id})
		expect(t, r.code == 2 && strings.Contains(r.stderr, "--year"), "no year: %+v", r)
		r = e.run([]string{"report", "capital-gains", "--all-entities", "--year", "2025"})
		expect(t, r.code == 2 && strings.Contains(r.stderr, "--out") && strings.Contains(r.stderr, "--json"), "--all-entities with no target: %+v", r)

		r = e.run([]string{"report", "capital-gains", "--entity", id, "--year", "2025"})
		expect(t, r.code == 0 && len(r.stdout) > 0 && strings.Contains(strings.SplitN(r.stdout, "\n", 2)[0], ","), "CSV to stdout: %+v", r)

		r = e.run([]string{"report", "capital-gains", "--entity", id, "--year", "2025", "--json"})
		doc, _ := jsonOf(t, r.stdout).(map[string]any)
		columns, _ := doc["columns"].([]any)
		_, rowsOK := doc["rows"].([]any)
		expect(t, r.code == 0 && doc["entity"].(map[string]any)["id"] == id && doc["report"] == "capital-gains" && doc["year"] == "2025" && len(columns) > 0 && rowsOK,
			"one document: %v", doc)

		r = e.run([]string{"report", "capital-gains", "--all-entities", "--year", "2025", "--json"})
		docs, isArray := jsonOf(t, r.stdout).([]any)
		expect(t, r.code == 0 && isArray && len(docs) > 0 && docs[0].(map[string]any)["entity"].(map[string]any)["id"] == id, "an array for --all-entities: %+v", r)

		// Keyed on the flag, not the count: a script mustn't break the day a
		// firm signs its second client.
		_, singleIsArray := jsonOf(t, e.run([]string{"report", "capital-gains", "--entity", id, "--year", "2025", "--json"}).stdout).([]any)
		expect(t, !singleIsArray, "--entity should give an object, not an array")

		for _, name := range []string{"bundle", "ato-mytax", "division-70-trading-stock"} {
			r := e.run([]string{"report", name, "--entity", id, "--year", "2025", "--json"})
			expect(t, r.code == 2 && strings.Contains(r.stderr, "--out"), "--json on %s: %+v", name, r)
		}
		r = e.run([]string{"report", "capital-gains", "--entity", id, "--year", "2025", "--json", "--out", "x.json"})
		expect(t, r.code == 2 && strings.Contains(r.stderr, "stdout"), "--json with --out: %+v", r)
	})

	t.Run("portfolio", func(t *testing.T) {
		r := e.run([]string{"portfolio", "--entity", id})
		expect(t, r.code == 0 && strings.Contains(r.stdout, "No portfolio history yet") && !strings.Contains(r.stdout, "\x1b"), "chart into a pipe: %+v", r)
		r = e.run([]string{"portfolio", "--entity", id, "--json"})
		doc, _ := jsonOf(t, r.stdout).(map[string]any)
		_, pointsOK := doc["points"].([]any)
		expect(t, r.code == 0 && doc["entity"].(map[string]any)["id"] == id && pointsOK && doc["range"] == "all", "--json: %v", doc)
		_, allIsArray := jsonOf(t, e.run([]string{"portfolio", "--all-entities", "--json"}).stdout).([]any)
		expect(t, allIsArray, "--all-entities --json should be an array")
		r = e.run([]string{"--help"})
		expect(t, strings.Contains(r.stdout, "portfolio") && strings.Contains(r.stdout, "--range"), "help should list portfolio")
		doc, _ = jsonOf(t, e.run([]string{"portfolio", "--entity", id, "--range", "1m", "--json"}).stdout).(map[string]any)
		expect(t, doc["range"] == "1m", "the range is echoed back: %v", doc["range"])
		r = e.run([]string{"portfolio", "--entity", id, "--range", "2w"})
		expect(t, r.code == 2 && strings.Contains(r.stderr, "24h"), "unknown range: %+v", r)
	})

	t.Run("transactions", func(t *testing.T) {
		// The new entity has none, which is its own answer.
		r := e.run([]string{"transactions", "--entity", id})
		expect(t, r.code == 0 && r.stdout == "" && strings.Contains(r.stderr, "No transactions yet."), "empty: %+v", r)
		r = e.run([]string{"transactions", "--entity", id, "--category", "transfer", "--json"})
		doc, _ := jsonOf(t, r.stdout).(map[string]any)
		events, eventsOK := doc["events"].([]any)
		expect(t, r.code == 0 && eventsOK && len(events) == 0 && doc["filters"].(map[string]any)["category"] == "transfer", "--json: %v", doc)
		// A category the API doesn't have is the API's to refuse.
		r = e.run([]string{"transactions", "--entity", id, "--category", "not-a-category"})
		expect(t, r.code == 2 && strings.Contains(r.stderr, "category"), "bad category: %+v", r)
	})

	t.Run("sources sync --json", func(t *testing.T) {
		r := e.run([]string{"sources", "sync", "--entity", id, "--all", "--json"})
		doc, _ := jsonOf(t, r.stdout).(map[string]any)
		queued, _ := doc["queued"].([]any)
		expect(t, r.code == 0 && doc["status"] == "skipped" && len(queued) == 0, "nothing to sync is recorded, not omitted: %v", doc)
		r = e.run([]string{"sources", "sync", "--all-entities", "--all", "--json"})
		_, isArray := jsonOf(t, r.stdout).([]any)
		expect(t, r.code == 0 && isArray, "--all-entities should be an array")
	})

	t.Run("sources", func(t *testing.T) {
		expect(t, e.run([]string{"sources", "list", "--entity", id}).code == 0, "an empty list isn't a failure")
		r := e.run([]string{"sources", "sync", "--entity", id})
		expect(t, r.code == 2 && strings.Contains(r.stderr, "--all"), "no target: %+v", r)
		r = e.run([]string{"sources", "sync", "--entity", id, "--all", "--timeout", "abc"})
		expect(t, r.code == 2 && strings.Contains(r.stderr, "--timeout"), "bad timeout: %+v", r)
	})

	t.Run("a read-only token", func(t *testing.T) {
		// Created here, last: a source would invalidate "nothing to sync"
		// above.
		var source struct {
			ID any `json:"id"`
		}
		e.post("/entities/"+id+"/sources", map[string]string{"adapterKey": "csv_generic", "label": "Test CSV"}, &source)
		r := e.run([]string{"sources", "sync", "--entity", id, "--source", fmt.Sprint(source.ID)}, "GRUBLESS_TOKEN="+e.readToken)
		expect(t, r.code == 3 && strings.Contains(r.stderr, "token-scope problem"), "read-only token: %+v", r)
	})
}
