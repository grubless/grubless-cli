package tui

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/grubless/grubless-cli/go/internal/api"
	"github.com/grubless/grubless-cli/go/internal/timerange"
)

// A port of src/test/tui.test.ts. Everything worth testing is pure — the
// reducer and the views — so these run with no TTY, no server and no timers.

func key(name string) Key { return Key{Name: name} }

func s(v string) *string { return &v }

func entity(i int) api.Entity {
	return api.Entity{ID: fmt.Sprintf("id-%d", i), Name: fmt.Sprintf("Entity %d", i), EntityType: "company", Role: "owner"}
}

func withEntities(n int) State {
	st := InitialState()
	st.Loading = false
	for i := 0; i < n; i++ {
		st.Entities = append(st.Entities, entity(i))
	}
	return st
}

// opened lands on Holdings, not the real landing tab: most of these are
// about list behaviour, and the chart deliberately isn't a list.
func opened(mod func(*State)) State {
	st := withEntities(3)
	e := st.Entities[0]
	st.SelectedEntity = &e
	st.Tab = "holdings"
	if mod != nil {
		mod(&st)
	}
	return st
}

func press(st State, names ...string) State {
	for _, n := range names {
		st, _ = Reduce(st, key(n), 10)
	}
	return st
}

func text(st State, w, h int) string { return StripAnsi(strings.Join(Render(st, w, h), "\n")) }

func holding(symbol string, chain *string, value string, labels ...string) api.Holding {
	h := api.Holding{AssetID: symbol, Symbol: symbol, Chain: chain, Quantity: s("100"), Value: s(value)}
	for _, l := range labels {
		h.Sources = append(h.Sources, api.HoldingSource{Label: l})
	}
	return h
}

func history(points ...[2]string) []api.PortfolioHistoryPoint {
	var out []api.PortfolioHistoryPoint
	for _, p := range points {
		out = append(out, api.PortfolioHistoryPoint{Date: p[0], Value: s(p[1]), CumulativeIncome: s("0"), UnrealizedPL: s("0")})
	}
	return out
}

func TestKeyDecoding(t *testing.T) {
	cases := map[string][]Key{
		"\x1b[A": {key("up")}, "\x1b[B": {key("down")}, "\x1b[C": {key("right")}, "\x1b[D": {key("left")},
		// A paste or a fast typist delivers several at once.
		"\x1b[Bj\x1b[B": {key("down"), key("j"), key("down")},
		"\x03":          {{Name: "c", Ctrl: true}}, "\r": {key("return")}, "\t": {key("tab")},
		// Bracketed paste must not read as "2", "0", "0", "~".
		"\x1b[200~": nil,
		"\x1b[5~":   {key("pageup")}, "\x1b[6~": {key("pagedown")},
	}
	for chunk, want := range cases {
		if got := DecodeKeys(chunk); !reflect.DeepEqual(got, want) {
			t.Errorf("DecodeKeys(%q) = %v, want %v", chunk, got, want)
		}
	}
}

func TestWidths(t *testing.T) {
	if VisibleWidth("\x1b[1mabc\x1b[0m") != 3 {
		t.Error("escapes should have no width")
	}
	// A cut escape spills raw bytes and sticks the terminal's colour.
	out := Truncate("\x1b[31mabcdefghij\x1b[0m", 5)
	if VisibleWidth(out) > 5 || StripAnsi(out) != "abcd…" {
		t.Errorf("Truncate = %q", out)
	}
	if Truncate("abc", 10) != "abc" || Truncate("abc", 0) != "" {
		t.Error("short strings untouched; zero width gives nothing")
	}
}

func TestNavigation(t *testing.T) {
	st := press(withEntities(3), "down")
	if st.EntityIndex != 1 {
		t.Errorf("down: %d", st.EntityIndex)
	}
	// Clamped, not wrapped.
	if st = press(st, "down", "down"); st.EntityIndex != 2 {
		t.Errorf("clamped: %d", st.EntityIndex)
	}
	if st = press(st, "up"); st.EntityIndex != 1 {
		t.Errorf("up: %d", st.EntityIndex)
	}
	if st = press(withEntities(3), "j", "k"); st.EntityIndex != 0 {
		t.Error("vim keys should move too")
	}

	// Scrolls only once the cursor leaves the window, by exactly one.
	st = withEntities(20)
	for i := 0; i < 4; i++ {
		st, _ = Reduce(st, key("down"), 5)
	}
	if st.EntityIndex != 4 || st.Offset != 0 {
		t.Errorf("in window: index %d offset %d", st.EntityIndex, st.Offset)
	}
	if st, _ = Reduce(st, key("down"), 5); st.EntityIndex != 5 || st.Offset != 1 {
		t.Errorf("scrolled: index %d offset %d", st.EntityIndex, st.Offset)
	}

	// A one-row viewport neither loops nor divides by zero.
	if st, _ = Reduce(withEntities(5), key("down"), 1); st.EntityIndex != 1 || st.Offset != 1 {
		t.Errorf("one row: index %d offset %d", st.EntityIndex, st.Offset)
	}
	if st, _ = Reduce(withEntities(50), key("end"), 10); st.EntityIndex != 49 || st.Offset != 40 {
		t.Errorf("end: index %d offset %d", st.EntityIndex, st.Offset)
	}
}

func TestOpeningAnEntity(t *testing.T) {
	st := withEntities(3)
	st.EntityIndex = 1
	next, action := Reduce(st, key("return"), 10)
	if action != ActOpenEntity || next.SelectedEntity == nil || next.SelectedEntity.Name != "Entity 1" || !next.Loading {
		t.Errorf("open: action %s, selected %v, loading %v", action, next.SelectedEntity, next.Loading)
	}
	if _, action := Reduce(InitialState(), key("return"), 10); action != ActNone {
		t.Error("return on an empty list should do nothing")
	}
}

func TestTabs(t *testing.T) {
	st := opened(nil)
	var seen []Tab
	for i := 0; i < 6; i++ {
		st = press(st, "tab")
		seen = append(seen, st.Tab)
	}
	if !reflect.DeepEqual(seen, []Tab{"warnings", "tax", "sources", "transactions", "chart", "holdings"}) {
		t.Errorf("tab cycle = %v", seen)
	}
	if InitialState().Tab != "chart" {
		t.Error("the portfolio chart is the landing tab")
	}
	if press(opened(nil), "4").Tab != "tax" {
		t.Error("number keys jump to a tab")
	}
	// A row-8 cursor carried into a 3-row list would sit off-screen.
	st = press(opened(func(st *State) { st.Cursor, st.Offset = 8, 4 }), "3")
	if st.Tab != "warnings" || st.Cursor != 0 || st.Offset != 0 {
		t.Errorf("tab change: %s cursor %d offset %d", st.Tab, st.Cursor, st.Offset)
	}
	if press(withEntities(3), "tab").SelectedEntity != nil {
		t.Error("tab keys do nothing on the picker")
	}
}

func TestQuittingAndGoingBack(t *testing.T) {
	// q goes back from an entity rather than exiting.
	next, action := Reduce(opened(nil), key("q"), 10)
	if next.SelectedEntity != nil || next.Quit || action != ActNone {
		t.Error("q on a tab should go back to the picker")
	}
	if next, action := Reduce(withEntities(3), key("q"), 10); !next.Quit || action != ActQuit {
		t.Error("q on the picker should quit")
	}
	// Stale data must never flash on the next entity.
	st := opened(func(st *State) { st.Data.Holdings = []api.Holding{{Symbol: "BTC"}} })
	if len(press(st, "q").Data.Holdings) != 0 {
		t.Error("going back should clear entity data")
	}
	if next, action := Reduce(opened(nil), Key{Name: "c", Ctrl: true}, 10); !next.Quit || action != ActQuit {
		t.Error("Ctrl-C always quits")
	}
}

func TestSync(t *testing.T) {
	next, action := Reduce(opened(nil), key("s"), 10)
	if action != ActSync || !next.Syncing {
		t.Error("s should request a sync and mark the state busy")
	}
	// The server's set-if-not-syncing claim makes a second press a no-op.
	next, action = Reduce(opened(func(st *State) { st.Syncing = true }), key("s"), 10)
	if action != ActNone || next.Message == nil || *next.Message != "Already syncing." {
		t.Error("s while syncing should refuse")
	}
	if _, action := Reduce(withEntities(3), key("s"), 10); action != ActNone {
		t.Error("s does nothing on the picker")
	}
}

func TestHelp(t *testing.T) {
	st := press(opened(nil), "?")
	if !st.ShowHelp || press(st, "x").ShowHelp {
		t.Error("? opens help and any key closes it")
	}
	// The dismissing key is swallowed.
	if next, action := Reduce(st, key("q"), 10); action != ActNone || next.SelectedEntity == nil {
		t.Error("the key that dismisses help must not also act")
	}
}

func TestRendering(t *testing.T) {
	for _, h := range []int{6, 24, 50} {
		if got := Render(withEntities(3), 80, h); len(got) != h {
			t.Errorf("height %d rendered %d lines", h, len(got))
		}
	}

	// A line wider than the terminal wraps and wrecks the layout.
	wide := opened(func(st *State) {
		st.Data.Holdings = []api.Holding{{Symbol: "AVERYLONGTOKENSYMBOL", Chain: s("an-extremely-long-chain-name"), Quantity: s("123456789.123456789"), Value: s("987654321.99"), HasMismatch: true}}
	})
	for _, line := range Render(wide, 40, 24) {
		if VisibleWidth(line) > 40 {
			t.Errorf("line wider than 40: %q", StripAnsi(line))
		}
	}

	if out := text(withEntities(2), 80, 24); !strings.Contains(out, "Entity 0") || !strings.Contains(out, "Entity 1") {
		t.Error("the picker should show entity names")
	}

	tax := func(applicable bool) State {
		return opened(func(st *State) {
			st.Tab = "tax"
			st.Data.Tax = []api.TaxYearSummary{{FinancialYear: "2025–26", TaxPayable: s("254142.028769325140330000"), Income: s("1071559.886401792102100000"), NetCapitalGainLoss: s("43459.49"), TaxableAmount: s("1016568.12"), Applicable: applicable}}
		})
	}
	if out := text(tax(true), 120, 24); !strings.Contains(out, "254,142.03") || strings.Contains(out, "254142.028769325140330000") {
		t.Error("money should be formatted, not raw precision")
	}
	// Pass-through: "n/a", never "0.00", which would read as nothing owed.
	if !strings.Contains(text(tax(false), 120, 24), "n/a") {
		t.Error("a pass-through entity should say n/a")
	}

	spam := opened(func(st *State) {
		st.Data.Holdings = []api.Holding{holding("REAL", nil, "1"), {Symbol: "SCAM", Value: s("1"), IsSpam: true}}
	})
	if out := text(spam, 80, 24); !strings.Contains(out, "REAL") || strings.Contains(out, "SCAM") || RowCount(spam) != 1 {
		t.Error("spam hidden, and not counted — or the cursor could sit on an invisible row")
	}

	count := 17000.0
	sources := opened(func(st *State) {
		st.Tab = "sources"
		st.Data.Sources = []api.Source{{ID: "s1", Label: "Kraken", AdapterKey: "kraken", TransactionCount: &count, LastSyncedAt: s("2026-08-01T00:00:00Z"), SyncStatus: "error", SyncEnabled: true}}
	})
	if out := text(sources, 120, 24); !strings.Contains(out, "Kraken") || !strings.Contains(out, "error") {
		t.Error("sources should show their sync status")
	}

	// Title, tabs and status bar, then the list card's column header,
	// border and shadow rows: seven.
	if ViewportRows(24) != 17 || ViewportRows(1) != 1 || ViewportRows(0) != 1 {
		t.Error("viewport leaves room for the chrome, and never drops below one row")
	}
}

func TestHeldInColumn(t *testing.T) {
	with := func(hs ...api.Holding) State { return opened(func(st *State) { st.Data.Holdings = hs }) }
	if out := text(with(holding("USDC", nil, "100", "hyperliquid")), 120, 24); !strings.Contains(out, "HELD IN") || !strings.Contains(out, "hyperliquid") {
		t.Error("a chainless holding shows its source")
	}
	if !strings.Contains(text(with(holding("USDC", s("solana"), "100", "sol-flex")), 120, 24), "solana") {
		t.Error("the chain wins when there is one")
	}
	// A long label mustn't shove QUANTITY right on its row.
	var rows []string
	for _, l := range strings.Split(text(with(holding("USDC", nil, "100", "Kraken-trading-account-a-very-long-label"), holding("USDC", s("solana"), "100", "x")), 120, 24), "\n") {
		if strings.Contains(l, "USDC") {
			rows = append(rows, l)
		}
	}
	if len(rows) != 2 || column(rows[0], "100") != column(rows[1], "100") {
		t.Errorf("columns misaligned: %q", rows)
	}
}

var brailleInk = regexp.MustCompile(`[\x{2801}-\x{28FF}]`)

func TestChartTab(t *testing.T) {
	Now = func() time.Time { return fixedNow }
	defer func() { Now = time.Now }()

	// Range "all": these are about what's drawn, and a default range would
	// filter these fixed dates out once they aged past the window.
	chart := func(mod func(*State)) State {
		return opened(func(st *State) {
			st.Tab, st.Range = "chart", "all"
			mod(st)
		})
	}

	st := chart(func(st *State) {
		st.Data.History = history([2]string{"2026-01-01", "1000"}, [2]string{"2026-06-01", "50000"}, [2]string{"2026-08-01", "42000"})
	})
	if out := text(st, 100, 20); !brailleInk.MatchString(out) || !strings.Contains(out, "2026-01-01") {
		t.Error("the chart should draw and date the series")
	}

	point := func(pl string) State {
		return chart(func(st *State) {
			st.Data.History = []api.PortfolioHistoryPoint{{Date: "2026-08-07", Value: s("721022.174"), CumulativeIncome: s("4321.5"), UnrealizedPL: s(pl)}}
		})
	}
	if out := text(point("12345.67"), 120, 20); !strings.Contains(out, "721,022.17   unrealised +12,345.67   income 4,321.50") {
		t.Error("the header should carry today's exact, signed figures")
	}
	if !strings.Contains(text(point("-987.65"), 120, 20), "unrealised -987.65") || !strings.Contains(text(point("0"), 120, 20), "unrealised 0.00") {
		t.Error("a loss keeps its sign; zero stays bare")
	}

	// Not a list: no reverse-video row across the plot (the tab bar and
	// range strip reverse legitimately, above it).
	st = chart(func(st *State) { st.Data.History = history([2]string{"2026-01-01", "1"}, [2]string{"2026-01-02", "2"}) })
	// The tab bar and the range control reverse legitimately; a line of the
	// plot never should.
	plotHighlighted := false
	for _, line := range Render(st, 100, 20) {
		if brailleInk.MatchString(line) && strings.Contains(line, reverse) {
			plotHighlighted = true
		}
	}
	if plotHighlighted || RowCount(st) != 0 {
		t.Error("the plot must never be highlighted as a row")
	}
	if !strings.Contains(text(st, 100, 20), "2 days") {
		t.Error("the status bar counts days on the chart")
	}

	// The web overview's second card: the activity breakdown, beside the
	// chart when there's room for both.
	breakdown := chart(func(st *State) {
		st.Data.History = history([2]string{"2026-01-01", "1000"}, [2]string{"2026-08-01", "5000"})
		var rows []api.BreakdownBucket
		for i, k := range []string{"trade", "staking_reward", "transfer", "send", "receive", "fee", "airdrop"} {
			rows = append(rows, api.BreakdownBucket{Day: "2026-03-01", Key: k, Value: float64(700 - i*100)})
		}
		// Years back, but the range is ALL, so it counts — and it's the
		// largest, so it leads.
		rows = append(rows, api.BreakdownBucket{Day: "2019-01-01", Key: "mining", Value: 99999})
		st.Data.Breakdown = &api.ActivityBreakdown{TotalEvents: 1234, Category: rows}
		st.Data.Coverage = &api.PriceCoverage{Total: 415, Priced: 412, Missing: 3}
		st.Data.Sources = []api.Source{{ID: "a", LastSyncedAt: s("2026-08-09T01:00:00.000Z")}, {ID: "b", LastSyncedAt: s("2026-08-01T00:00:00.000Z")}}
	})
	wide := text(breakdown, 140, 32)
	for _, want := range []string{"Portfolio value", "Activity breakdown", "Mining", "Trade", "Staking Reward", "Other (3)", "Connected sources", "Transactions", "1,234", "Last synced", "3 hours ago", "Price coverage", "412 / 415", "3 missing"} {
		if !strings.Contains(wide, want) {
			t.Errorf("the wide overview should show %q", want)
		}
	}
	// Largest first, with the web's category names rather than keys.
	if strings.Index(wide, "Mining") > strings.Index(wide, "Trade") || strings.Contains(wide, "staking_reward") {
		t.Error("the breakdown should be largest first, labelled as the web labels it")
	}
	// A narrower terminal gives the chart the width, and keeps the figures.
	if narrow := text(breakdown, 100, 32); strings.Contains(narrow, "Activity breakdown") || !strings.Contains(narrow, "Price coverage") {
		t.Error("below 110 columns the breakdown stands down, the stat cards stay")
	}
	// A short one keeps the chart, and the status bar.
	short := Render(breakdown, 140, 14)
	if strings.Contains(StripAnsi(strings.Join(short, "")), "Connected sources") || !strings.Contains(StripAnsi(short[13]), "r reload") {
		t.Error("a short terminal drops the stat cards first, and keeps the status bar")
	}
	// While the extras load, and if they fail, the cards say so.
	loading := breakdown
	loading.Data.Breakdown, loading.Data.Coverage = nil, nil
	if out := text(loading, 140, 32); !strings.Contains(out, "Loading…") || !strings.Contains(out, "…") {
		t.Error("the breakdown should say it's loading")
	}
	failed := loading
	failed.Data.BreakdownFailed, failed.Data.CoverageFailed = true, true
	if !strings.Contains(text(failed, 140, 32), "Couldn't load the breakdown.") {
		t.Error("a failed breakdown should say so")
	}

	empty := Render(chart(func(st *State) {}), 100, 20)
	if len(empty) != 20 || !strings.Contains(StripAnsi(strings.Join(empty, "\n")), "No portfolio history yet") || !strings.Contains(StripAnsi(empty[19]), "r reload") {
		t.Error("no history still fills the frame and keeps the status bar")
	}
}

func TestSpinner(t *testing.T) {
	glyph := regexp.MustCompile(`(\S) Loading`)
	frames := map[string]bool{}
	for tick := 0; tick < 4; tick++ {
		st := InitialState()
		st.Tick = tick
		if m := glyph.FindStringSubmatch(text(st, 80, 24)); m != nil {
			frames[m[1]] = true
		}
	}
	if len(frames) < 2 {
		t.Error("the spinner should turn as the tick advances")
	}
	if !strings.Contains(text(InitialState(), 80, 24), "Loading…") {
		t.Error("it should say what it's doing")
	}
	if !regexp.MustCompile(`\S Loading…`).MatchString(text(opened(func(st *State) { st.Loading, st.Tick = true, 2 }), 80, 24)) {
		t.Error("it spins on an entity's tabs too")
	}
}

func TestRange(t *testing.T) {
	Now = func() time.Time { return fixedNow }
	defer func() { Now = time.Now }()

	spanning := history([2]string{"2020-01-01", "1000"}, [2]string{"2026-08-01", "500000"})
	if InitialState().Range != "fy" {
		t.Error("the TUI opens on the financial year, as the web dashboard does")
	}
	out := text(opened(func(st *State) { st.Tab = "chart"; st.Data.History = spanning }), 100, 30)
	if !strings.Contains(out, "24H") || !strings.Contains(out, "ALL") || !strings.Contains(out, "[ ]") {
		t.Error("the range strip shows its options and its keys")
	}

	base := opened(func(st *State) { st.Tab = "chart" })
	step := func(from, k string) string {
		st := base
		st.Range = timerangeKey(from)
		return string(press(st, k).Range)
	}
	// Clamped at both ends, not wrapped.
	if step("fy", "]") != "all" || step("all", "]") != "all" || step("24h", "[") != "24h" || step("1w", "[") != "24h" {
		t.Error("[ and ] step the range, clamped")
	}
	if st := press(opened(func(st *State) { st.Range = "fy" }), "]"); st.Range != "fy" {
		t.Error("range keys are ignored away from the chart")
	}

	if !strings.Contains(text(opened(func(st *State) { st.Tab, st.Range = "chart", "all"; st.Data.History = spanning }), 100, 30), "2 days") {
		t.Error("the count covers only days inside the range")
	}
	if !strings.Contains(text(opened(func(st *State) { st.Tab, st.Range = "chart", "24h"; st.Data.History = spanning[:1] }), 100, 30), "No portfolio history yet") {
		t.Error("an empty range says so rather than drawing a flat chart")
	}
}

func timerangeKey(s string) timerange.Key { return timerange.Key(s) }

// column is the on-screen position of sub in line — JS indexOf, in UTF-16
// units, not the byte offset strings.Index gives.
func column(line, sub string) int {
	i := strings.Index(line, sub)
	if i < 0 {
		return -1
	}
	return len(utf16.Encode([]rune(line[:i])))
}
