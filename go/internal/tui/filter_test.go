package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/grubless/grubless-cli/go/internal/api"
	"github.com/grubless/grubless-cli/go/internal/output"
	"github.com/grubless/grubless-cli/go/internal/txfmt"
)

// The Transactions filter form.

func filterable() State {
	st := onTransactions(3, true)
	st.Data.Sources = []api.Source{{ID: "src-k", Label: "Kraken"}, {ID: "src-s", Label: "sol-flex"}}
	st.Data.Holdings = []api.Holding{
		{AssetID: "usdc-eth", Symbol: "USDC", Chain: s("ethereum")},
		{AssetID: "usdc-sol", Symbol: "USDC", Chain: s("solana")},
		{AssetID: "scam", Symbol: "FREE", IsSpam: true},
	}
	st.Data.TxAssets = txfmt.Assets{"sol": {ID: "sol", Symbol: "SOL", Chain: s("solana")}, "usdc-sol": {ID: "usdc-sol", Symbol: "USDC", Chain: s("solana")}}
	return st
}

func typeText(st State, text string) State {
	for _, r := range text {
		st, _ = Reduce(st, key(string(r)), 10)
	}
	return st
}

func TestFilterOpens(t *testing.T) {
	for _, k := range []string{"f", "/"} {
		if press(filterable(), k).Filter == nil {
			t.Errorf("%s should open the filter on the transactions tab", k)
		}
	}
	if press(opened(nil), "f").Filter != nil {
		t.Error("f does nothing on other tabs")
	}
	loading := filterable()
	loading.Loading = true
	if press(loading, "f").Filter != nil {
		t.Error("f waits for the list to load")
	}
	// The form is modal: letters edit it rather than acting on the tab.
	st := press(filterable(), "f", "q", "r", "s")
	if st.Filter == nil || st.Filter.Draft.Category != "qrs" || st.Syncing || st.SelectedEntity == nil {
		t.Errorf("keys should go to the form: %+v", st.Filter)
	}
	if st := press(filterable(), "f", "escape"); st.Filter != nil || st.TxFilter != (TxFilter{}) {
		t.Error("Esc closes without applying")
	}
}

func TestCategoryField(t *testing.T) {
	st := typeText(press(filterable(), "f"), "tra")
	if got := Completion(st.Filter.Draft.Category); got != "nsfer" {
		t.Errorf("completion for tra = %q", got)
	}
	// Prefix matches first, then substring ones.
	matches := CategoryMatches("tra")
	if matches[0] != "transfer" || !contains(matches, "cross_chain_trade") || indexOf(matches, "cross_chain_trade") < indexOf(matches, "transfer") {
		t.Errorf("matches = %v", matches)
	}
	// → takes the suggestion and starts another.
	st = press(st, "right")
	if st.Filter.Draft.Category != "transfer," {
		t.Errorf("after → = %q", st.Filter.Draft.Category)
	}
	// Already-chosen categories aren't offered again.
	if contains(CategoryMatches("transfer,tra"), "transfer") {
		t.Error("a chosen category shouldn't be suggested twice")
	}
	st = press(typeText(st, "sen"), "right")
	if st.Filter.Draft.Category != "transfer,send," {
		t.Errorf("second completion = %q", st.Filter.Draft.Category)
	}
	if st = press(st, "backspace"); st.Filter.Draft.Category != "transfer,send" {
		t.Errorf("backspace = %q", st.Filter.Draft.Category)
	}
	if st, _ = Reduce(st, Key{Name: "u", Ctrl: true}, 10); st.Filter.Draft.Category != "" {
		t.Error("Ctrl-U clears the field")
	}
	if DecodeKeys("\x15")[0] != (Key{Name: "u", Ctrl: true}) {
		t.Error("Ctrl-U should decode")
	}
}

func TestChoiceFields(t *testing.T) {
	st := press(filterable(), "f", "down") // Direction
	st = press(st, "right")
	if st.Filter.Draft.Direction != "in" {
		t.Errorf("→ on direction = %q", st.Filter.Draft.Direction)
	}
	if st = press(st, "right", "right"); st.Filter.Draft.Direction != "" {
		t.Error("choices wrap back to Any")
	}
	if st = press(st, "left"); st.Filter.Draft.Direction != "out" {
		t.Error("← goes backwards, wrapping")
	}

	// Source: the entity's sources, by label.
	st = press(st, "down", "down", "down", "right")
	if st.Filter.Field != fieldSource || st.Filter.Draft.SourceID != "src-k" || st.Filter.Draft.SourceLabel != "Kraken" {
		t.Errorf("source = %+v", st.Filter.Draft)
	}

	// Asset: holdings and loaded transactions' assets, each once, spam left
	// out, labelled with the chain so the two USDCs are told apart.
	st = press(st, "down")
	var labels []string
	for _, o := range st.Filter.assets {
		labels = append(labels, o.label)
	}
	if strings.Join(labels, "|") != "Any|SOL · solana|USDC · ethereum|USDC · solana" {
		t.Errorf("asset options = %q", labels)
	}
	// Type-ahead: u, then u again, steps through the USDCs.
	st = press(st, "u")
	if st.Filter.Draft.AssetID != "usdc-eth" {
		t.Errorf("u = %q", st.Filter.Draft.AssetID)
	}
	if st = press(st, "u"); st.Filter.Draft.AssetID != "usdc-sol" {
		t.Errorf("u again = %q", st.Filter.Draft.AssetID)
	}
	if st, _ = Reduce(st, Key{Name: "u", Ctrl: true}, 10); st.Filter.Draft.AssetID != "" {
		t.Error("Ctrl-U resets a choice to Any")
	}
	// Fields wrap: down from the last row is the first.
	for st.Filter.Field != fieldClear {
		st = press(st, "down")
	}
	if st = press(st, "down"); st.Filter.Field != fieldCategory {
		t.Error("field focus should wrap")
	}
}

func TestApplyingAFilter(t *testing.T) {
	st := typeText(press(filterable(), "f"), " Transfer , send,")
	st = press(st, "down", "right", "right") // direction: out
	st = press(st, "down")
	st = typeText(st, "2025-07-01")
	next, action := Reduce(st, key("return"), 10)
	if action != ActApplyFilter || next.Filter != nil {
		t.Fatalf("apply: action %s, form open %v", action, next.Filter != nil)
	}
	// Tidied: case, spaces, the trailing comma.
	want := TxFilter{Category: "transfer,send", Direction: "out", From: "2025-07-01"}
	if next.TxFilter != want {
		t.Errorf("applied = %+v", next.TxFilter)
	}
	// Refetched from the top.
	if len(next.Data.Transactions) != 0 || next.Data.TxNextCursor != nil || next.Cursor != 0 || !next.Loading {
		t.Error("applying should clear the list and reload it")
	}
	if got := next.TxFilter.Query(100, ""); got != "category=transfer%2Csend&direction=out&from=2025-07-01&limit=100" {
		t.Errorf("query = %q", got)
	}
	if got := next.TxFilter.Query(100, "cur-1"); !strings.Contains(got, "cursor=cur-1") {
		t.Errorf("paging query = %q", got)
	}
	if next.TxFilter.Active() != 3 {
		t.Errorf("active = %d", next.TxFilter.Active())
	}

	// Reopening shows what's applied.
	next.Loading = false
	if reopened := press(next, "f"); reopened.Filter.Draft != want {
		t.Errorf("reopened draft = %+v", reopened.Filter.Draft)
	}

	// "Clear all filters", then Enter: everything off, and refetched.
	cleared := press(next, "f")
	for cleared.Filter.Field != fieldClear {
		cleared = press(cleared, "down")
	}
	cleared, action = Reduce(cleared, key("return"), 10)
	if action != ActApplyFilter || cleared.TxFilter != (TxFilter{}) {
		t.Errorf("clear all: %s %+v", action, cleared.TxFilter)
	}

	// A filter belongs to its entity.
	if back := press(next, "q"); back.TxFilter != (TxFilter{}) {
		t.Error("leaving the entity should drop its filter")
	}
}

func TestFilterValidation(t *testing.T) {
	cases := map[string]func(State) State{
		"Unknown category \"transfr\"":          func(st State) State { return typeText(st, "transfr") },
		"did you mean transfer? → completes it": func(st State) State { return typeText(st, "tran") },
		"From isn't a date":                     func(st State) State { return typeText(press(st, "down", "down"), "last week") },
		"To is before From": func(st State) State {
			return typeText(press(typeText(press(st, "down", "down"), "2026-01-01"), "down"), "2025-01-01")
		},
	}
	for want, edit := range cases {
		st := edit(press(filterable(), "f"))
		next, action := Reduce(st, key("return"), 10)
		if action != ActNone || next.Filter == nil || !strings.Contains(next.Filter.Error, want) {
			t.Errorf("%s: action %s, error %q", want, action, next.Filter.Error)
			continue
		}
		// Shown, and gone on the next key.
		if !strings.Contains(text(next, 120, 30), want) {
			t.Errorf("%s: error not on screen", want)
		}
		if press(next, "down").Filter.Error != "" {
			t.Errorf("%s: error should clear on the next key", want)
		}
	}
}

func TestFilterScreens(t *testing.T) {
	form := press(filterable(), "f")
	out := text(form, 120, 30)
	for _, want := range []string{"Filter transactions", "Category", "Direction", "Source", "Asset", "Search", "Sort", "Newest first", "Clear all filters", "type to search", "⏎ apply"} {
		if !strings.Contains(out, want) {
			t.Errorf("form should show %q", want)
		}
	}
	// Suggestions, and the ghost of the completion after the typed text.
	out = text(typeText(form, "stak"), 120, 30)
	// "stak" + the ghost "ing_reward" + the cursor.
	if !strings.Contains(out, "staking_reward, staking_withdrawal") || !strings.Contains(out, "staking_reward█") {
		t.Errorf("category suggestions missing:\n%s", out)
	}
	if !strings.Contains(text(typeText(form, "zzz"), 120, 30), "no category matches “zzz”") {
		t.Error("a token matching nothing should say so")
	}
	for _, line := range Render(typeText(form, "a"), 60, 24) {
		if VisibleWidth(line) > 60 {
			t.Errorf("form line wider than 60: %q", StripAnsi(line))
		}
	}

	// On the tab: how many filters, and an empty result that says why.
	filtered := filterable()
	filtered.TxFilter = TxFilter{Category: "transfer", Direction: "out"}
	if !strings.Contains(text(filtered, 160, 24), "2 filters (f)") {
		t.Error("the status bar should count active filters")
	}
	if !strings.Contains(text(filterable(), 160, 24), "f filter") {
		t.Error("with none active, the status bar says how to filter")
	}
	filtered.Data.Transactions, filtered.Data.TxNextCursor = nil, nil
	if !strings.Contains(text(filtered, 160, 24), "No transactions match this filter") {
		t.Error("an empty filtered list should blame the filter")
	}
}

func TestFilterError(t *testing.T) {
	err := &output.CliError{Message: "GET /entities/x/tx-events?category=bogus&limit=100 rejected:\n{\n  \"formErrors\": [],\n  \"fieldErrors\": {\n    \"category\": [\"Invalid enum value\"],\n    \"from\": [\"must be an ISO date or timestamp\"]\n  }\n}", ExitCode: output.UsageError}
	got := filterError(err).Error()
	if got != "The API rejected the filter — category: Invalid enum value · from: must be an ISO date or timestamp" {
		t.Errorf("filterError = %q", got)
	}
	if other := errors.New("Could not reach x"); filterError(other) != other {
		t.Error("other errors pass through")
	}
}

func contains(xs []string, x string) bool { return indexOf(xs, x) >= 0 }

func indexOf(xs []string, x string) int {
	for i, v := range xs {
		if v == x {
			return i
		}
	}
	return -1
}
