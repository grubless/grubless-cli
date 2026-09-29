package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/grubless/grubless-cli/go/internal/client"
	"github.com/grubless/grubless-cli/go/internal/output"
)

// `grubless transactions` against a fake API shaped like the real one
// (probed from production): {events, assets, nextCursor}, limit ≤ 2000,
// cursor paging, uuid-only sourceId/assetId.

const entityID = "7f0c2a52-1d7e-4d0a-9d1b-3c1f2b8e9a01"

type fakeAPI struct {
	total int
	mu    sync.Mutex
	// queries are the tx-events query strings received, in order.
	queries []string
}

func (f *fakeAPI) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		switch r.URL.Path {
		case "/entities":
			fmt.Fprintf(w, `[{"id":%q,"name":"Acme Trading Pty Ltd","entityType":"company","role":"owner"}]`, entityID)
		case "/entities/" + entityID + "/sources":
			fmt.Fprint(w, `[{"id":"a1a1a1a1-0000-4000-8000-000000000001","label":"sol-flex"}]`)
		case "/entities/" + entityID + "/holdings":
			fmt.Fprint(w, `[{"assetId":"b2b2b2b2-0000-4000-8000-000000000001","symbol":"SOL","chain":"solana"},
				{"assetId":"b2b2b2b2-0000-4000-8000-000000000002","symbol":"USDC","chain":"solana"},
				{"assetId":"b2b2b2b2-0000-4000-8000-000000000003","symbol":"USDC","chain":"ethereum"}]`)
		case "/entities/" + entityID + "/tx-events":
			f.mu.Lock()
			f.queries = append(f.queries, r.URL.RawQuery)
			f.mu.Unlock()
			q := r.URL.Query()
			limit, err := strconv.Atoi(q.Get("limit"))
			if err != nil || limit < 1 || limit > 2000 {
				w.WriteHeader(400)
				fmt.Fprint(w, `{"error":{"formErrors":[],"fieldErrors":{"limit":["Number must be less than or equal to 2000"]}}}`)
				return
			}
			start := 0
			if c := q.Get("cursor"); c != "" {
				start, _ = strconv.Atoi(strings.TrimPrefix(c, "cur-"))
			}
			end := min(f.total, start+limit)
			var events []string
			for i := start; i < end; i++ {
				// Every page references the same two assets, so merging must
				// list each once.
				events = append(events, fmt.Sprintf(`{"id":"ev-%05d","eventType":"trade","ts":"2026-09-29T01:30:57.000Z","extraField":{"kept":true},
					"legs":[{"assetId":"sol","direction":"out","role":"primary","amount":"1.5","gainLoss":"10.005"},
					        {"assetId":"usdc","direction":"in","role":"primary","amount":"300","gainLoss":null}],
					"source":{"label":"sol-flex"},"tags":[{"label":"Swap"}],"taxTreatment":"capital_gain_loss"}`, i))
			}
			next := "null"
			if end < f.total {
				next = fmt.Sprintf(`"cur-%d"`, end)
			}
			fmt.Fprintf(w, `{"events":[%s],"assets":[{"id":"sol","symbol":"SOL"},{"id":"usdc","symbol":"USDC"}],"nextCursor":%s}`, strings.Join(events, ","), next)
		default:
			t.Errorf("unexpected request %s", r.URL)
			w.WriteHeader(404)
		}
	}
}

// capture runs f with stdout and stderr redirected, returning both.
func capture(t *testing.T, f func()) (string, string) {
	t.Helper()
	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	var stdout, stderr []byte
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { stdout, _ = io.ReadAll(outR); wg.Done() }()
	go func() { stderr, _ = io.ReadAll(errR); wg.Done() }()
	defer func() {
		os.Stdout, os.Stderr = oldOut, oldErr
	}()
	f()
	outW.Close()
	errW.Close()
	wg.Wait()
	return string(stdout), string(stderr)
}

func run(t *testing.T, total int, s Scope, f TxFilters) (*fakeAPI, string, string, int, error) {
	t.Helper()
	api := &fakeAPI{total: total}
	srv := httptest.NewServer(api.handler(t))
	defer srv.Close()
	var code int
	var err error
	stdout, stderr := capture(t, func() {
		code, err = Transactions(client.New(srv.URL, "grb_test"), s, f)
	})
	return api, stdout, stderr, code, err
}

func TestTransactionsPagesPastTheAPICap(t *testing.T) {
	api, stdout, _, code, err := run(t, 2600, Scope{Entity: "acme", JSON: true}, TxFilters{Limit: "2500"})
	if err != nil || code != output.Ok {
		t.Fatalf("code %d, err %v", code, err)
	}
	// 2000 is the API's ceiling; the rest comes from the cursor, and exactly
	// as many as asked for.
	if len(api.queries) != 2 || api.queries[0] != "limit=2000" || api.queries[1] != "cursor=cur-2000&limit=500" {
		t.Errorf("queries = %q", api.queries)
	}
	var doc struct {
		Events     []map[string]any `json:"events"`
		Assets     []map[string]any `json:"assets"`
		NextCursor *string          `json:"nextCursor"`
		Filters    map[string]any   `json:"filters"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Events) != 2500 || doc.Events[2499]["id"] != "ev-02499" {
		t.Errorf("%d events", len(doc.Events))
	}
	// Server fields this client doesn't declare survive into --json.
	if doc.Events[0]["extraField"] == nil {
		t.Error("undeclared server fields must pass through")
	}
	if len(doc.Assets) != 2 {
		t.Errorf("assets should be merged across pages, got %d", len(doc.Assets))
	}
	// More remain, and where they start is handed back.
	if doc.NextCursor == nil || *doc.NextCursor != "cur-2500" {
		t.Errorf("nextCursor = %v", doc.NextCursor)
	}
	if doc.Filters["limit"] != 2500.0 || doc.Filters["category"] != nil {
		t.Errorf("filters echo = %v", doc.Filters)
	}
}

func TestTransactionsLimitAllStopsAtTheEnd(t *testing.T) {
	api, stdout, _, _, err := run(t, 4100, Scope{Entity: "acme", JSON: true}, TxFilters{Limit: "all"})
	if err != nil {
		t.Fatal(err)
	}
	if len(api.queries) != 3 {
		t.Errorf("queries = %q", api.queries)
	}
	if !strings.Contains(stdout, `"nextCursor": null`) || !strings.Contains(stdout, `"limit": "all"`) || strings.Count(stdout, `"eventType"`) != 4100 {
		t.Error("--limit all should fetch everything and end with a null cursor")
	}
}

func TestTransactionsFiltersAndNames(t *testing.T) {
	api, _, _, _, err := run(t, 3, Scope{Entity: "acme", JSON: true}, TxFilters{
		Category: "transfer,send", Direction: "out", From: "2025-07-01", To: "2026-06-30",
		Search: "jup", Source: "sol-flex", Asset: "SOL", Sort: "asc",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Names become the uuids the API wants; the rest pass straight through.
	want := "assetId=b2b2b2b2-0000-4000-8000-000000000001&category=transfer%2Csend&direction=out&from=2025-07-01&limit=50&q=jup&sort=asc&sourceId=a1a1a1a1-0000-4000-8000-000000000001&to=2026-06-30"
	if len(api.queries) != 1 || api.queries[0] != want {
		t.Errorf("query\n got: %q\nwant: %q", api.queries, want)
	}

	// A symbol held on two chains is refused, not guessed.
	_, _, _, _, err = run(t, 3, Scope{Entity: "acme"}, TxFilters{Asset: "usdc"})
	var cliErr *output.CliError
	if !errors.As(err, &cliErr) || cliErr.ExitCode != output.UsageError || !strings.Contains(err.Error(), "matches 2 assets") {
		t.Errorf("ambiguous symbol: %v", err)
	}
	// A uuid is taken as-is, without a holdings lookup.
	if api, _, _, _, err := run(t, 1, Scope{Entity: "acme"}, TxFilters{Asset: "b2b2b2b2-0000-4000-8000-000000000003"}); err != nil || !strings.Contains(api.queries[0], "assetId=b2b2b2b2-0000-4000-8000-000000000003") {
		t.Errorf("uuid asset: %v %q", err, api.queries)
	}
	for name, f := range map[string]TxFilters{
		"unknown source": {Source: "nope"},
		"bad direction":  {Direction: "sideways"},
		"bad sort":       {Sort: "up"},
		"zero limit":     {Limit: "0"},
		"word limit":     {Limit: "lots"},
	} {
		if _, _, _, _, err := run(t, 1, Scope{Entity: "acme"}, f); !errors.As(err, &cliErr) || cliErr.ExitCode != output.UsageError {
			t.Errorf("%s: want a usage error, got %v", name, err)
		}
	}
}

func TestTransactionsTable(t *testing.T) {
	_, stdout, stderr, _, err := run(t, 60, Scope{Entity: "acme"}, TxFilters{})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	// Header plus the default 50.
	if len(lines) != 51 {
		t.Errorf("%d lines", len(lines))
	}
	for _, want := range []string{"DATE (UTC)", "ev-00000", "2026-09-29 01:30", "1.5 SOL", "300 USDC", "+10.01", "sol-flex", "Swap"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("table should contain %q", want)
		}
	}
	if !strings.Contains(stderr, "Showing the first 50; more match.") {
		t.Errorf("stderr = %q", stderr)
	}

	_, stdout, stderr, _, _ = run(t, 0, Scope{Entity: "acme"}, TxFilters{})
	if stdout != "" || !strings.Contains(stderr, "No transactions yet.") {
		t.Errorf("empty: stdout %q stderr %q", stdout, stderr)
	}
	_, _, stderr, _, _ = run(t, 0, Scope{Entity: "acme"}, TxFilters{Category: "send"})
	if !strings.Contains(stderr, "No transactions match those filters.") {
		t.Errorf("filtered empty: %q", stderr)
	}
}
