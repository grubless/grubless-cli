package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/grubless/grubless-cli/go/internal/api"
	"github.com/grubless/grubless-cli/go/internal/txfmt"
)

// The Transactions tab: a list that pages itself in, and a detail view.

func txEvents(n int) []api.TxEvent {
	out := make([]api.TxEvent, n)
	for i := range out {
		out[i] = api.TxEvent{
			ID: fmt.Sprintf("%08d-0000-0000-0000-000000000000", i), EventType: "trade", Ts: "2026-09-29T01:30:57.000Z",
			Legs: []api.TxLeg{
				// Made-up figures. The fee leg's tiny gain is there so the
				// total (+37,575.01) differs from the primary leg's (+37,575.00).
				{AssetID: "sol", Direction: "out", Role: "primary", Amount: s("1250.5"), Value: s("187575.00"), Proceeds: s("187575.00"), CostBasis: s("150000.00"), GainLoss: s("37575.004")},
				{AssetID: "usdc", Direction: "in", Role: "primary", Amount: s("187000.5"), Value: s("187000.50")},
				{AssetID: "sol", Direction: "out", Role: "fee", Amount: s("0.000012345"), GainLoss: s("0.002")},
			},
		}
	}
	return out
}

func onTransactions(n int, more bool) State {
	return opened(func(st *State) {
		st.Tab = "transactions"
		st.Data.Transactions = txEvents(n)
		st.Data.TxAssets = txfmt.Assets{"sol": {ID: "sol", Symbol: "SOL"}, "usdc": {ID: "usdc", Symbol: "USDC"}}
		if more {
			st.Data.TxNextCursor = s("2026-09-01T00:00:00.000Z_abc")
		}
	})
}

func TestTransactionsTab(t *testing.T) {
	if press(opened(nil), "6").Tab != "transactions" {
		t.Error("6 jumps to the transactions tab")
	}
	st := onTransactions(3, false)
	if RowCount(st) != 3 {
		t.Errorf("RowCount = %d", RowCount(st))
	}
	out := text(st, 160, 24)
	for _, want := range []string{"6 Transactions", "DATE (UTC)", "2026-09-29 01:30", "1,250.5 SOL", "187,000.5 USDC", "+37,575.01", "3 rows"} {
		if !strings.Contains(out, want) {
			t.Errorf("tab should show %q", want)
		}
	}
	// With more on the server, the count says it's only what's loaded.
	if !strings.Contains(text(onTransactions(3, true), 160, 24), "3+ rows") {
		t.Error("a partial list should count as \"3+ rows\"")
	}
	for _, line := range Render(onTransactions(3, true), 60, 24) {
		if VisibleWidth(line) > 60 {
			t.Errorf("line wider than 60: %q", StripAnsi(line))
		}
	}
}

func TestTransactionDetail(t *testing.T) {
	st := onTransactions(3, false)
	st.Cursor = 1
	st = press(st, "return")
	if !st.Detail {
		t.Fatal("return on a transaction opens it")
	}

	out := text(st, 160, 30)
	// Exact figures per leg — what the list can only summarise — and fees
	// shown as their own leg.
	for _, want := range []string{"00000001-0000-0000-0000-000000000000", "187,575.00", "150,000.00", "+37,575.00", "fee", "0.00001234", "2 of 3"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail should show %q", want)
		}
	}
	// The total is the exact sum of both legs' gains, not either one.
	if !strings.Contains(out, "Gain/loss      +37,575.01") {
		t.Error("detail gain/loss should be the summed legs")
	}

	// Up and down move between transactions without closing.
	if next := press(st, "j"); !next.Detail || next.Cursor != 2 {
		t.Errorf("j in detail: open %v cursor %d", next.Detail, next.Cursor)
	}
	// Esc, q and return close — q must not also drop back to the picker.
	for _, k := range []string{"escape", "q", "return"} {
		next := press(st, k)
		if next.Detail || next.SelectedEntity == nil {
			t.Errorf("%s should close the detail and stay on the tab", k)
		}
	}
	// Other keys don't act on the tab hidden behind it.
	if next := press(st, "4"); next.Tab != "transactions" || !next.Detail {
		t.Error("tab keys are ignored while a transaction is open")
	}
	if next, action := Reduce(st, Key{Name: "c", Ctrl: true}, 10); !next.Quit || action != ActQuit {
		t.Error("Ctrl-C still quits")
	}
	// r too: a reload would replace the list the detail points into, so it
	// waits until the detail is closed.
	if next, action := Reduce(st, key("r"), 10); !next.Detail || action != ActNone {
		t.Error("r is ignored while a transaction is open")
	}
	if press(onTransactions(0, false), "return").Detail {
		t.Error("return on an empty list opens nothing")
	}
}

func TestTransactionsPageIn(t *testing.T) {
	// Far from the end: no request.
	st := onTransactions(100, true)
	if _, action := Reduce(st, key("down"), 10); action != ActNone {
		t.Error("no load while far from the end")
	}
	// Within a screenful of the end: one request, flagged in flight.
	st.Cursor = 89 // down lands on 90: within 10 rows of the end
	next, action := Reduce(st, key("down"), 10)
	if action != ActLoadMore || !next.Data.TxLoadingMore {
		t.Fatalf("near the end: action %s, loading %v", action, next.Data.TxLoadingMore)
	}
	// Never twice at once.
	if _, action := Reduce(next, key("down"), 10); action != ActNone {
		t.Error("a second load while one is in flight")
	}
	if !strings.Contains(text(next, 160, 40), "loading more…") {
		t.Error("an in-flight page should say so")
	}
	// Nothing more on the server: nothing to ask for.
	done := onTransactions(100, false)
	done.Cursor = 98
	if _, action := Reduce(done, key("down"), 10); action != ActNone {
		t.Error("no load when there's no next page")
	}
	// End and paging down count too, and so does moving within the detail.
	if _, action := Reduce(onTransactions(100, true), key("end"), 10); action != ActLoadMore {
		t.Error("End near the end should load more")
	}
	open := onTransactions(100, true)
	open.Cursor, open.Detail = 95, true
	if _, action := Reduce(open, key("j"), 10); action != ActLoadMore {
		t.Error("stepping through details should load more too")
	}
}
