package scenarios

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/grubless/grubless-cli/internal/jsstr"
)

// The stub API the scenarios run against: every route the CLI calls, with
// fixtures chosen for the things a client gets subtly wrong (a BOM, quoting,
// integer-like keys, dust quantities, exponent notation, hostile filenames),
// and token-selected failure modes a real server can't be made to produce on
// demand. Ported from the parity harness that compared the Node and Go
// builds; the scenarios' recordings were checked against both.

const (
	acme = "7f0c2a52-1d7e-4d0a-9d1b-3c1f2b8e9a01"
	soc  = "0b6e8a3c-55f1-4c2e-8f7a-9e2d1c4b6a02"
)

// obj is an ordered JSON object, so a fixture serialises with its keys in the
// order written — which is the order a real server sends them.
type obj []any

func (o obj) MarshalJSON() ([]byte, error) {
	var b strings.Builder
	b.WriteByte('{')
	for i := 0; i+1 < len(o); i += 2 {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(o[i].(string))
		v, err := marshal(o[i+1])
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return []byte(b.String()), nil
}

func marshal(v any) ([]byte, error) {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return []byte(strings.TrimSuffix(b.String(), "\n")), nil
}

var entities = []any{
	obj{"id", acme, "name", "Acme Trading Pty Ltd", "entityType", "company", "role", "owner", "createdAt", "2025-07-01T00:00:00.000Z", "extraServerField", obj{"nested", []int{1, 2}, "10", "int-like key"}},
	obj{"id", soc, "name", "Société Générale <Test> & Co / 2025", "entityType", "trust", "role", "preparer", "createdAt", "2025-07-02T00:00:00.000Z"},
}

var dotsEntity = obj{"id", "d0d0d0d0-0000-4000-8000-000000000000", "name", "..", "entityType", "trust", "role", "viewer", "createdAt", "2025-07-03T00:00:00.000Z"}

var sources = map[string][]any{
	acme: {
		obj{"id", "src-kraken", "entityId", acme, "sourceType", "exchange_api", "adapterKey", "kraken", "label", "Kraken", "config", obj{}, "lastSyncedAt", "2026-08-01T10:00:00.000Z", "syncStatus", "idle", "syncError", nil, "syncEnabled", true, "createdAt", "2025-07-01T00:00:00.000Z", "transactionCount", 17342, "lastTransactionAt", nil},
		obj{"id", "src-evm", "entityId", acme, "sourceType", "on_chain", "adapterKey", "evm", "label", "evm-wallet-a (Ethereum)", "config", obj{"address", "0xabc"}, "lastSyncedAt", nil, "syncStatus", "error", "syncError", "RPC rate limited", "syncEnabled", true, "createdAt", "2025-07-01T00:00:00.000Z", "transactionCount", 12, "lastTransactionAt", nil},
		obj{"id", "src-csv", "entityId", acme, "sourceType", "csv_import", "adapterKey", "csv", "label", "Old exchange CSV", "config", obj{}, "lastSyncedAt", "2025-01-01T00:00:00+10:00", "syncStatus", "idle", "syncError", nil, "syncEnabled", false, "createdAt", "2025-07-01T00:00:00.000Z", "transactionCount", 0, "lastTransactionAt", nil},
	},
	soc: {},
}

var holdings = []any{
	obj{"assetId", "a1", "symbol", "BTC", "chain", "bitcoin", "imageUrl", nil, "quantity", "1.234500000000000000", "value", "154321.987654321", "hasMismatch", false, "isSpam", false, "sources", []any{obj{"sourceId", "src-kraken", "label", "Kraken", "calculatedQuantity", "1.2345", "reportedQuantity", "1.2345", "mismatch", false}}},
	obj{"assetId", "a2", "symbol", "USDC", "chain", nil, "imageUrl", nil, "quantity", "2500.5", "value", "2500.50", "hasMismatch", true, "isSpam", false, "sources", []any{obj{"label", "hyperliquid"}, obj{"label", "Kraken"}}},
	obj{"assetId", "a3", "symbol", "0xccef6bdd7534f750eb1f494367493b5fd65c905d", "chain", nil, "imageUrl", nil, "quantity", "2e-9", "value", nil, "hasMismatch", false, "isSpam", false, "sources", []any{obj{"label", "evm-wallet-a (Ethereum)"}, obj{"label", "evm-wallet-b (Ethereum)"}, obj{"label", "evm-wallet-c (Ethereum)"}}},
	obj{"assetId", "a4", "symbol", "SCAM", "chain", "ethereum", "imageUrl", nil, "quantity", "1000000", "value", "0", "hasMismatch", false, "isSpam", true, "sources", []any{}},
}

var taxSummary = []any{
	obj{"financialYear", "2024–25", "startYear", 2024, "startDate", "2024-07-01", "endDate", "2025-06-30", "currency", "aud", "incomeLabel", "Assessable income", "income", "1071559.886401792102100000", "rewardIncome", "0", "miningIncome", "0", "incomeByCategory", obj{"staking", "1.5", "2024", "odd key"}, "expenses", "1200", "netIncome", "0", "rawCapitalGainLoss", "0", "bridgingGainLoss", "0", "wrappingGainLoss", "0", "capitalGainLossByCategory", obj{}, "discountedCapitalGainLoss", "0", "netCapitalGainLoss", "43459.49", "feesPaid", "0", "feesByCategory", obj{}, "broughtForwardLoss", "0", "carriedForwardLoss", "-41434.915835700685932000", "taxableAmount", "1016568.12", "taxPayable", "254142.028769325140330000", "rate", "0.25", "applicable", true},
	obj{"financialYear", "2025–26", "startYear", 2025, "startDate", "2025-07-01", "endDate", "2026-06-30", "currency", "aud", "incomeLabel", "Assessable income", "income", "0", "rewardIncome", "0", "miningIncome", "0", "incomeByCategory", obj{}, "expenses", "0", "netIncome", "0", "rawCapitalGainLoss", "0", "bridgingGainLoss", "0", "wrappingGainLoss", "0", "capitalGainLossByCategory", obj{}, "discountedCapitalGainLoss", "0", "netCapitalGainLoss", "0", "feesPaid", "0", "feesByCategory", obj{}, "broughtForwardLoss", "0", "carriedForwardLoss", "0.000", "taxableAmount", "0", "taxPayable", "0", "rate", nil, "applicable", false, "notes", "pass-through"},
}

func warnings(category string) []any {
	switch category {
	case "zero-cost":
		var out []any
		for i := 0; i < 7; i++ {
			symbol := "BTC"
			if i%2 == 1 {
				symbol = "ETH"
			}
			out = append(out, obj{"disposalId", fmt.Sprintf("d%d", i), "txEventId", fmt.Sprintf("t%d", i), "eventType", "sell", "ts", fmt.Sprintf("2025-0%d-15T04:05:06.000Z", i%9+1), "assetSymbol", symbol, "assetChain", nil, "sourceLabel", "Kraken", "sourceAdapterKey", "kraken", "quantity", fmt.Sprintf("0.%d5", i), "proceedsAmount", fmt.Sprintf("%d.555", 1000*i), "gainLossAmount", "1", "currency", "aud"})
		}
		return out
	case "uncategorized-transfers":
		return []any{obj{"txEventId", "u1", "eventType", "transfer", "ts", "2025-03-01T00:00:00.000Z", "direction", "out", "assetSymbol", "USDC", "assetChain", "ethereum", "amount", "1e3"}}
	case "unpriced-assets":
		return []any{obj{"assetId", "x", "symbol", "OBSCURE", "chain", "solana", "contractOrMintAddress", nil, "legCount", 3, "incomeLegs", 1, "netQuantity", "5", "firstSeen", "2025-01-01", "lastSeen", "2025-02-01"}}
	}
	return []any{}
}

var history = func() []any {
	var out []any
	start := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 420; i++ {
		value := 100000 + math.Sin(float64(i)/20)*20000 + float64(i)*150
		out = append(out, obj{
			"date", start.AddDate(0, 0, i).Format("2006-01-02"),
			"value", jsstr.ToFixed(value, 6),
			"cumulativeIncome", jsstr.ToFixed(float64(i)*12.5, 2),
			"unrealizedPL", jsstr.ToFixed(math.Cos(float64(i)/15)*5000, 4),
		})
	}
	return out
}()

var historyDates = func() []string {
	var out []string
	for _, p := range history {
		out = append(out, p.(obj)[1].(string))
	}
	return out
}()

// txEvent is the fields the stub filters on, alongside the JSON it sends.
type txEvent struct {
	json      obj
	eventType string
	ts        string
	sourceID  string
	legs      []txLeg
}

type txLeg struct{ assetID, direction, role string }

var txAssets = []any{obj{"id", "asset-sol", "chain", "solana", "symbol", "SOL"}, obj{"id", "asset-usdc", "chain", "solana", "symbol", "USDC"}}

var txEvents = func() []txEvent {
	var out []txEvent
	base := time.Date(2026, 9, 29, 1, 30, 0, 0, time.UTC)
	for i := 0; i < 150; i++ {
		transfer := i%5 == 0
		eventType := "trade"
		if transfer {
			eventType = "transfer"
		}
		var notes, protocol any
		if i == 1 {
			notes = "Rebalance after the audit"
		}
		if i%2 == 1 {
			protocol = "jupiter"
		}
		treatment := "capital_gain_loss"
		var gain any = fmt.Sprintf("%d.125", (i+1)*50)
		if transfer {
			treatment, gain = "non_taxable", nil
		}
		legs := []any{obj{"assetId", "asset-sol", "direction", "out", "role", "primary", "amount", fmt.Sprintf("%d.5", i+1), "value", fmt.Sprintf("%d.125", (i+1)*250), "currency", "aud", "proceeds", fmt.Sprintf("%d.125", (i+1)*250), "costBasis", strconv.Itoa((i + 1) * 200), "gainLoss", gain}}
		parsed := []txLeg{{"asset-sol", "out", "primary"}}
		if !transfer {
			legs = append(legs, obj{"assetId", "asset-usdc", "direction", "in", "role", "primary", "amount", strconv.Itoa((i + 1) * 170), "value", strconv.Itoa((i + 1) * 250), "currency", "aud", "proceeds", nil, "costBasis", strconv.Itoa((i + 1) * 250), "gainLoss", nil})
			parsed = append(parsed, txLeg{"asset-usdc", "in", "primary"})
		}
		legs = append(legs, obj{"assetId", "asset-sol", "direction", "out", "role", "fee", "amount", "0.000005", "value", "0.001", "currency", "aud", "proceeds", "0.001", "costBasis", "0.0009", "gainLoss", "0.0001"})
		parsed = append(parsed, txLeg{"asset-sol", "out", "fee"})
		tags := []any{}
		if i%3 != 0 {
			tags = []any{obj{"label", "Swap"}}
		}
		ts := base.Add(-time.Duration(i) * time.Hour).Format("2006-01-02T15:04:05.000Z")
		out = append(out, txEvent{
			json: obj{
				"id", fmt.Sprintf("%08d-1d7e-4d0a-9d1b-3c1f2b8e9a01", i), "eventType", eventType,
				"isManuallyCategorized", i%7 == 0, "isInternalTransfer", transfer, "ts", ts,
				"description", nil, "notes", notes, "detectedProtocol", protocol, "taxTreatment", treatment,
				"legs", legs, "source", obj{"id", "src-kraken", "label", "Kraken"}, "tags", tags,
			},
			eventType: eventType, ts: ts, sourceID: "src-kraken", legs: parsed,
		})
	}
	return out
}()

// Report bodies: a BOM, quoting, a prose note, surplus fields, an
// integer-like column.
const (
	reportCSV = "\uFEFFDate,Asset,\"Proceeds, AUD\",2025\r\n2025-07-01,BTC,\"1,234.56\",x\r\n2025-07-02,\"He said \"\"hi\"\"\",7.89,y,surplus\r\n\"multi\nline\",ETH,0.000000010000000001,z\r\n"
	noteCSV   = "Field,Value\r\nNo cached tax summary found for this financial year — run a sync first.\r\n"
)

var binary = func() []byte {
	b := make([]byte, 4096)
	for i := range b {
		b[i] = byte((i * 7919) % 256)
	}
	return b
}()

// stub is the server, with the state a scenario can change: the activity
// feed advances per poll, and every POST is recorded.
type stub struct {
	mu            sync.Mutex
	activityPolls int
	syncStartedAt string
	posts         []string
}

func (s *stub) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activityPolls, s.syncStartedAt, s.posts = 0, "", nil
}

func send(w http.ResponseWriter, status int, body any, headers ...string) {
	w.Header().Set("content-type", "application/json")
	for i := 0; i+1 < len(headers); i += 2 {
		w.Header().Set(headers[i], headers[i+1])
	}
	w.WriteHeader(status)
	switch b := body.(type) {
	case string:
		io.WriteString(w, b)
	case []byte:
		w.Write(b)
	default:
		out, _ := marshal(b)
		w.Write(out)
	}
}

var entityPath = regexp.MustCompile(`^/entities/([^/]+)/(.+)$`)

func (s *stub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	token := strings.TrimPrefix(r.Header.Get("authorization"), "Bearer ")
	path := r.URL.Path

	failures := map[string][2]any{
		"grb_401":   {401, obj{"error", "Invalid or revoked token"}},
		"grb_402":   {402, obj{"error", "Reports require a paid plan."}},
		"grb_403ro": {403, obj{"error", "This token is read-only", "readOnlyToken", true}},
		"grb_403":   {403, obj{"error", "Forbidden"}},
		"grb_404":   {404, obj{"error", "Not found"}},
		"grb_400":   {400, obj{"error", obj{"formErrors", []any{}, "fieldErrors", obj{"year", []string{"Required"}, "name", []string{"Too short"}}}}},
	}
	if f, ok := failures[token]; ok {
		send(w, f[0].(int), f[1])
		return
	}
	if token == "grb_502" {
		send(w, 502, "<html><body>Bad Gateway</body></html>", "content-type", "text/html")
		return
	}
	if !strings.HasPrefix(token, "grb_ok") && token != "grb_empty" && token != "grb_oldcli" {
		send(w, 401, obj{"error", "Invalid token"})
		return
	}
	if token == "grb_oldcli" {
		w.Header().Set("x-grubless-min-cli-version", "99.0.0")
	}
	empty := token == "grb_empty"

	if r.Method == "POST" {
		body, _ := io.ReadAll(r.Body)
		s.posts = append(s.posts, "POST "+path+" "+canonical(body))
		if strings.HasSuffix(path, "/sync") || strings.HasSuffix(path, "/csv-import") {
			if s.syncStartedAt == "" {
				s.syncStartedAt = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
			}
			send(w, 202, obj{"queued", true})
			return
		}
		send(w, 404, obj{"error", "no such route"})
		return
	}

	switch path {
	case "/entities":
		switch {
		case empty:
			send(w, 200, []any{})
		case token == "grb_ok_dots":
			send(w, 200, append(append([]any{}, entities...), dotsEntity))
		default:
			send(w, 200, entities)
		}
		return
	case "/api-tokens":
		if empty {
			send(w, 500, obj{"error", "boom"})
		} else {
			send(w, 200, []any{obj{"id", "t1", "name", "laptop", "scope", "read", "lastUsedAt", nil, "expiresAt", nil, "createdAt", "2025-07-01T00:00:00.000Z"}})
		}
		return
	}

	m := entityPath.FindStringSubmatch(path)
	if m == nil {
		send(w, 404, obj{"error", "Not found"})
		return
	}
	id, rest := m[1], m[2]
	orEmpty := func(v []any) []any {
		if id == acme {
			return v
		}
		return []any{}
	}

	switch {
	case rest == "sources":
		v, ok := sources[id]
		if !ok {
			v = []any{}
		}
		send(w, 200, v)
	case rest == "holdings":
		send(w, 200, orEmpty(holdings))
	case rest == "tax-summary":
		send(w, 200, orEmpty(taxSummary))
	case strings.HasPrefix(rest, "warnings/"):
		send(w, 200, orEmpty(warnings(strings.TrimPrefix(rest, "warnings/"))))
	case rest == "portfolio-history":
		send(w, 200, orEmpty(history))
	case rest == "tax-settings":
		if id == acme {
			send(w, 200, obj{"entityId", id, "baseCurrency", "aud", "financialYearStartMonth", 7})
		} else {
			send(w, 404, obj{"error", "no settings"})
		}
	case rest == "activity-breakdown":
		s.breakdown(w, id)
	case rest == "price-coverage":
		if id == acme {
			send(w, 200, obj{"total", 415, "priced", 412, "missing", 3})
		} else {
			send(w, 200, obj{"total", 0, "priced", 0, "missing", 0})
		}
	case rest == "tx-events":
		s.txEvents(w, r, id)
	case rest == "activity":
		s.activity(w, id, token)
	case strings.HasPrefix(rest, "reports/"):
		s.report(w, r, id, strings.TrimPrefix(rest, "reports/"))
	default:
		send(w, 404, obj{"error", "Not found"})
	}
}

func (s *stub) breakdown(w http.ResponseWriter, id string) {
	if id != acme {
		send(w, 200, obj{"currency", "aud", "totalEvents", 0, "labels", obj{}, "category", []any{}, "source", []any{}, "tag", []any{}})
		return
	}
	kinds := []string{"trade", "staking_reward", "transfer", "send", "receive", "fee", "airdrop", "income"}
	var category []any
	for i, day := range historyDates {
		for j, k := range kinds[:1+i%len(kinds)] {
			category = append(category, obj{"d", day, "k", k, "v", 1000/float64(j+1) + float64(i%7)*10})
		}
	}
	send(w, 200, obj{"currency", "aud", "totalEvents", 1234, "labels", obj{}, "category", category, "source", []any{}, "tag", []any{}})
}

// txEvents applies the filters the API applies, the way it applies them.
func (s *stub) txEvents(w http.ResponseWriter, r *http.Request, id string) {
	q := r.URL.Query()
	limit, err := strconv.Atoi(q.Get("limit"))
	if err != nil || limit < 1 || limit > 2000 {
		send(w, 400, obj{"error", obj{"formErrors", []any{}, "fieldErrors", obj{"limit", []string{"Expected number, received nan"}}}})
		return
	}
	start, _ := strconv.Atoi(strings.TrimPrefix(q.Get("cursor"), "cur-"))
	isoOf := func(v string) string {
		t, ok := jsstr.ParseDate(v)
		if !ok {
			return v
		}
		return t.UTC().Format("2006-01-02T15:04:05.000Z")
	}
	var categories []string
	if c := q.Get("category"); c != "" {
		categories = strings.Split(c, ",")
	}
	var all []any
	if id == acme {
		for _, e := range txEvents {
			leg := func(f func(txLeg) bool) bool {
				for _, l := range e.legs {
					if f(l) {
						return true
					}
				}
				return false
			}
			ok := (categories == nil || contains(categories, e.eventType)) &&
				(q.Get("direction") == "" || leg(func(l txLeg) bool { return l.role != "fee" && l.direction == q.Get("direction") })) &&
				(q.Get("sourceId") == "" || e.sourceID == q.Get("sourceId")) &&
				(q.Get("assetId") == "" || leg(func(l txLeg) bool { return l.assetID == q.Get("assetId") })) &&
				(q.Get("from") == "" || e.ts >= isoOf(q.Get("from"))) &&
				(q.Get("to") == "" || e.ts <= isoOf(q.Get("to")))
			if ok && q.Get("q") != "" {
				raw, _ := marshal(e.json)
				ok = strings.Contains(strings.ToLower(string(raw)), strings.ToLower(q.Get("q")))
			}
			if ok {
				all = append(all, e.json)
			}
		}
	}
	if q.Get("sort") == "asc" {
		for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 {
			all[i], all[j] = all[j], all[i]
		}
	}
	page := []any{}
	if start < len(all) {
		page = all[start:min(len(all), start+limit)]
	}
	var next any
	if start+limit < len(all) {
		next = fmt.Sprintf("cur-%d", start+limit)
	}
	send(w, 200, obj{"events", page, "assets", txAssets, "nextCursor", next})
}

// activity advances per poll: queued, running (two messages), then done.
// "grb_ok_fail" ends in an error, "grb_ok_degraded" in a caveat. A stale job
// from 2020 is always there too, which a wait must ignore.
func (s *stub) activity(w http.ResponseWriter, id, token string) {
	s.activityPolls++
	started := s.syncStartedAt
	if started == "" {
		started = "2020-01-01T00:00:00.000Z"
	}
	status := "success"
	switch {
	case s.activityPolls == 1:
		status = "queued"
	case s.activityPolls < 4:
		status = "running"
	case token == "grb_ok_fail":
		status = "error"
	case token == "grb_ok_degraded":
		status = "degraded"
	}
	var message, errText any
	switch {
	case s.activityPolls == 1:
	case s.activityPolls < 4:
		message = fmt.Sprintf("Fetching page %d…", s.activityPolls-1)
	case status == "degraded":
		message = "Price provider budget exhausted"
	}
	if status == "error" {
		errText = "Kraken returned 500"
	}
	send(w, 200, []any{
		obj{"id", "act1", "entityId", id, "sourceId", "src-kraken", "jobType", "sync_source", "status", status, "message", message, "error", errText, "startedAt", started, "updatedAt", started, "finishedAt", nil, "source", obj{"label", "Kraken"}},
		obj{"id", "old", "entityId", id, "sourceId", nil, "jobType", "recalculate", "status", "running", "message", "ancient", "error", nil, "startedAt", "2020-01-01T00:00:00.000Z", "updatedAt", "2020-01-01T00:00:00.000Z", "finishedAt", nil, "source", nil},
	})
}

func (s *stub) report(w http.ResponseWriter, r *http.Request, id, name string) {
	year := r.URL.Query().Get("year")
	csv := func(body, disposition string) {
		headers := []string{"content-type", "text/csv"}
		if disposition != "" {
			headers = append(headers, "content-disposition", disposition)
		}
		send(w, 200, body, headers...)
	}
	// Hostile filenames: each tries to climb out of --out.
	hostile := map[string]string{
		"highest-balance":            `attachment; filename="../../escape.csv"`,
		"end-of-year-holdings":       "attachment; filename*=UTF-8''..%2F..%2Fencoded.csv",
		"beginning-of-year-holdings": `attachment; filename="..\..\windows.csv"`,
		"division-70-trading-stock":  `attachment; filename=".."`,
	}
	switch {
	case id == soc && name == "income":
		send(w, 500, obj{"error", "Report generation failed"})
	case name == "complete-tax":
		send(w, 200, binary, "content-type", "application/zip", "content-disposition", fmt.Sprintf(`attachment; filename="complete-tax-FY%s.zip"`, year))
	case name == "ato-mytax":
		send(w, 200, binary, "content-type", "application/pdf", "content-disposition", `attachment; filename="ato-mytax.pdf"`)
	case name == "fees":
		csv(reportCSV, "attachment; filename*=UTF-8''fees%E2%82%AC-FY.csv")
	case name == "expenses":
		csv(reportCSV, "attachment; filename*=UTF-8''bad%E2.csv")
	case name == "gifts-donations-lost":
		csv(noteCSV, "")
	case hostile[name] != "":
		csv(reportCSV, hostile[name])
	case name == "other-gains":
		csv("", "")
	default:
		suffix := ""
		if year != "" {
			suffix = "-FY" + year
		}
		csv(reportCSV, fmt.Sprintf(`attachment; filename="%s%s.csv"`, name, suffix))
	}
}

// canonical re-encodes a request body with sorted keys, so two clients that
// send the same fields in a different order record the same request.
func canonical(body []byte) string {
	var v any
	if json.Unmarshal(body, &v) != nil {
		return string(body)
	}
	return canonicalValue(v)
}

func canonicalValue(v any) string {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			kb, _ := json.Marshal(k)
			parts[i] = string(kb) + ":" + canonicalValue(x[k])
		}
		return "{" + strings.Join(parts, ",") + "}"
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = canonicalValue(e)
		}
		return "[" + strings.Join(parts, ",") + "]"
	}
	out, _ := marshal(v)
	return string(out)
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
