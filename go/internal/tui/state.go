package tui

import (
	"slices"
	"strconv"

	"github.com/grubless/grubless-cli/go/internal/api"
	"github.com/grubless/grubless-cli/go/internal/timerange"
	"github.com/grubless/grubless-cli/go/internal/txfmt"
)

// TUI state and the pure reducer over it — separated from the terminal and
// the client so the whole interaction model is testable with no TTY and no
// server. See src/tui/state.ts.

type Tab string

// Portfolio leads, as on the web dashboard. Transactions come last: the
// other tabs summarise, this one is the ledger they're summaries of.
var Tabs = []Tab{"chart", "holdings", "warnings", "tax", "sources", "transactions"}

// "Overview", as the web's first tab is called: it's the chart, the
// breakdown and the headline figures together.
var TabLabel = map[Tab]string{
	"chart": "Overview", "holdings": "Holdings", "warnings": "Warnings", "tax": "Tax", "sources": "Sources",
	"transactions": "Transactions",
}

// IsList: every tab but the chart is a scrollable list of rows.
func IsList(tab Tab) bool { return tab != "chart" }

// EntityData is everything one entity's tabs show. The json tags match the
// TS field names, so a state serialised by the TS decodes here — which is
// how the golden render tests feed both builds the same screen.
type EntityData struct {
	Holdings      []api.Holding                      `json:"holdings"`
	Sources       []api.Source                       `json:"sources"`
	Tax           []api.TaxYearSummary               `json:"tax"`
	ZeroCost      []api.ZeroCostWarning              `json:"zeroCost"`
	Uncategorized []api.UncategorizedTransferWarning `json:"uncategorized"`
	Activity      []api.EntityActivity               `json:"activity"`
	History       []api.PortfolioHistoryPoint        `json:"history"`
	// Nil until loaded, or when the entity has no settings row.
	Settings *api.EntityTaxSettings `json:"settings"`

	// Transactions, newest first, as far as they've been paged in. The tab
	// loads one page with the rest of the entity and fetches the next as
	// the cursor nears the end — an entity can have tens of thousands.
	Transactions []api.TxEvent `json:"transactions"`
	TxAssets     txfmt.Assets  `json:"-"`
	// TxNextCursor is where the next page starts; nil when there isn't one.
	TxNextCursor  *string `json:"txNextCursor"`
	TxLoadingMore bool    `json:"txLoadingMore"`

	// The overview's extras, fetched after everything above so a slow one
	// (the breakdown takes seconds on a large ledger) doesn't hold the
	// screen. Nil while loading; *Failed when it couldn't be had.
	Breakdown       *api.ActivityBreakdown `json:"breakdown"`
	BreakdownFailed bool                   `json:"breakdownFailed"`
	Coverage        *api.PriceCoverage     `json:"coverage"`
	CoverageFailed  bool                   `json:"coverageFailed"`
}

type State struct {
	Entities    []api.Entity `json:"entities"`
	EntityIndex int          `json:"entityIndex"`
	// Nil means "still on the picker".
	SelectedEntity *api.Entity `json:"selectedEntity"`
	Tab            Tab         `json:"tab"`
	Offset         int         `json:"offset"`
	Cursor         int         `json:"cursor"`
	Data           EntityData  `json:"data"`
	Loading        bool        `json:"loading"`
	// Message is the status bar's transient text; nil for none.
	Message     *string       `json:"message"`
	MessageKind string        `json:"messageKind"`
	Syncing     bool          `json:"syncing"`
	ShowHelp    bool          `json:"showHelp"`
	Quit        bool          `json:"quit"`
	Range       timerange.Key `json:"range"`
	// Tick drives the spinner, advanced by a timer rather than by any key —
	// a twenty-second load has to look alive without input.
	Tick int `json:"tick"`
	// Detail shows the transaction under the cursor, full screen.
	Detail bool `json:"detail"`
	// Theme is one of Themes. The app always sets it (to DefaultTheme when
	// nobody chose); "" renders in the terminal's own colours.
	Theme string `json:"theme"`
	// TrueColour is whether the terminal takes 24-bit colour, which a
	// theme with exact colours needs to show them exactly.
	TrueColour bool `json:"trueColour"`
	// TxFilter is the transactions filter in force for this entity.
	TxFilter TxFilter `json:"txFilter"`
	// Filter is the open filter form, or nil.
	Filter *FilterForm `json:"filter"`
}

func InitialState() State {
	return State{
		Tab:         "chart",
		Loading:     true,
		MessageKind: "info",
		// The web dashboard's default; all-time made the same entity look
		// different in the two places.
		Range: "fy",
	}
}

func msg(s string) *string { return &s }

// RowCount is how many rows the active tab has — it drives cursor clamping.
func RowCount(s State) int {
	if s.SelectedEntity == nil {
		return len(s.Entities)
	}
	switch s.Tab {
	case "holdings":
		n := 0
		for _, h := range s.Data.Holdings {
			if !h.IsSpam {
				n++
			}
		}
		return n
	case "warnings":
		return len(s.Data.ZeroCost) + len(s.Data.Uncategorized)
	case "tax":
		return len(s.Data.Tax)
	case "sources":
		return len(s.Data.Sources)
	case "transactions":
		return len(s.Data.Transactions)
	}
	// The chart isn't a list.
	return 0
}

// Action is what a keypress asks the app to do. Returned, not performed, so
// the reducer stays pure.
type Action string

const (
	ActNone       Action = "none"
	ActReload     Action = "reload"
	ActSync       Action = "sync"
	ActOpenEntity Action = "openEntity"
	ActQuit       Action = "quit"
	// ActLoadMore asks for the next page of transactions.
	ActLoadMore Action = "loadMore"
	// ActTheme asks for the new theme to be applied and saved.
	ActTheme Action = "theme"
	// ActApplyFilter asks for the transactions to be refetched under the
	// state's TxFilter.
	ActApplyFilter Action = "applyFilter"
)

// backToPicker is where q and Esc go from inside an entity — clearing data,
// so a stale entity never flashes on the next one.
func backToPicker(s State) State {
	s.SelectedEntity = nil
	s.Cursor, s.Offset = 0, 0
	s.Data = EntityData{}
	s.Detail = false
	// A filter belongs to the entity it was set on.
	s.TxFilter, s.Filter = TxFilter{}, nil
	return s
}

// withMore requests the next page of transactions once the cursor is within
// a screenful of the end, so scrolling rarely has to wait on it.
func withMore(s State, viewportRows int) (State, Action) {
	d := s.Data
	if s.SelectedEntity == nil || s.Tab != "transactions" || d.TxNextCursor == nil || d.TxLoadingMore || s.Loading {
		return s, ActNone
	}
	if s.Cursor < len(d.Transactions)-max(1, viewportRows) {
		return s, ActNone
	}
	s.Data.TxLoadingMore = true
	return s, ActLoadMore
}

// reduceDetail handles keys while a transaction is open: move to the
// previous or next one, or close. Anything else is ignored rather than
// acting on the tab hidden behind it.
func reduceDetail(state State, key Key, viewportRows int) (State, Action) {
	next := state
	switch key.Name {
	case "escape", "q", "return":
		next.Detail = false
		return next, ActNone
	case "up", "k":
		return withMore(moveCursor(next, -1, viewportRows), viewportRows)
	case "down", "j":
		return withMore(moveCursor(next, 1, viewportRows), viewportRows)
	}
	return next, ActNone
}

// Reduce is the whole interaction model, as one pure function.
// viewportRows is passed in so paging is testable at any size.
func Reduce(state State, key Key, viewportRows int) (State, Action) {
	next := state

	// Help is modal: any key dismisses it, and is swallowed.
	if state.ShowHelp {
		next.ShowHelp = false
		return next, ActNone
	}
	if key.Ctrl && (key.Name == "c" || key.Name == "d") {
		next.Quit = true
		return next, ActQuit
	}
	if state.Filter != nil {
		return reduceFilter(state, key)
	}
	if state.Detail {
		return reduceDetail(state, key, viewportRows)
	}

	switch key.Name {
	case "q":
		// On a tab, q goes back rather than exiting: moving between clients
		// is far more common than quitting.
		if state.SelectedEntity != nil {
			return backToPicker(next), ActNone
		}
		next.Quit = true
		return next, ActQuit

	case "escape":
		if state.SelectedEntity != nil {
			return backToPicker(next), ActNone
		}
		return next, ActNone

	case "?":
		next.ShowHelp = true
		return next, ActNone

	case "f", "/":
		if state.SelectedEntity == nil || state.Tab != "transactions" || state.Loading {
			return next, ActNone
		}
		next.Filter = openFilter(state)
		return next, ActNone

	// Anywhere, like the web app's toggle — the theme is a preference about
	// the whole interface, not about one screen of it.
	case "t":
		next.Theme = nextTheme(themeOrDefault(state.Theme))
		next.Message = msg("Theme: " + ThemeLabel[next.Theme])
		next.MessageKind = "info"
		return next, ActTheme

	case "up", "k":
		return moveCursor(next, -1, viewportRows), ActNone
	case "down", "j":
		return withMore(moveCursor(next, 1, viewportRows), viewportRows)
	case "pageup":
		return moveCursor(next, -viewportRows, viewportRows), ActNone
	case "pagedown":
		return withMore(moveCursor(next, viewportRows, viewportRows), viewportRows)

	// Home/End move entityIndex AND cursor: the picker reads one, the tabs
	// the other.
	case "home":
		next.Cursor, next.EntityIndex, next.Offset = 0, 0, 0
		return next, ActNone
	case "end":
		last := max(0, RowCount(state)-1)
		next.Cursor, next.EntityIndex = last, last
		return withMore(clampScroll(next, viewportRows), viewportRows)

	case "return":
		if state.SelectedEntity != nil && state.Tab == "transactions" && !state.Loading && state.Cursor < len(state.Data.Transactions) {
			next.Detail = true
			return next, ActNone
		}
		if state.SelectedEntity == nil && len(state.Entities) > 0 {
			// Out of range is reachable, and not only in theory: End on a tab
			// sets entityIndex to that tab's last row, which can be past the
			// end of the entity list. The TS then reads `entities[i]` as
			// undefined and silently opens nothing; that's kept here (rather
			// than panicking) until it's fixed in both builds.
			if state.EntityIndex >= 0 && state.EntityIndex < len(state.Entities) {
				e := state.Entities[state.EntityIndex]
				next.SelectedEntity = &e
			}
			next.Cursor, next.Offset = 0, 0
			next.Loading = true
			return next, ActOpenEntity
		}
		return next, ActNone

	case "tab", "right", "l":
		if state.SelectedEntity == nil {
			return next, ActNone
		}
		return switchTab(next, 1), ActNone

	case "left", "h":
		if state.SelectedEntity == nil {
			return next, ActNone
		}
		return switchTab(next, -1), ActNone

	// Range stepping, chart tab only: [ and ], since ←/→ switch tabs.
	case "[", "]":
		if state.SelectedEntity == nil || state.Tab != "chart" {
			return next, ActNone
		}
		delta := -1
		if key.Name == "]" {
			delta = 1
		}
		next.Range = stepRange(state.Range, delta)
		return next, ActNone

	case "r":
		if state.SelectedEntity == nil {
			return next, ActNone
		}
		next.Loading = true
		next.Message = nil
		next.Detail = false
		return next, ActReload

	case "s":
		if state.SelectedEntity == nil {
			return next, ActNone
		}
		if state.Syncing {
			// The server's set-if-not-syncing claim makes a second press a
			// no-op that would look like it did something.
			next.Message = msg("Already syncing.")
			next.MessageKind = "info"
			return next, ActNone
		}
		next.Syncing = true
		next.Message = nil
		return next, ActSync
	}

	// Number keys jump straight to a tab. parseInt semantics: leading digits.
	if index, ok := leadingInt(key.Name); ok && state.SelectedEntity != nil && index >= 1 && index <= len(Tabs) {
		next.Tab = Tabs[index-1]
		next.Cursor, next.Offset = 0, 0
	}
	return next, ActNone
}

// leadingInt is Number.parseInt(s, 10) for the non-negative case: the
// leading digits, or false for none.
func leadingInt(s string) (int, bool) {
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, false
	}
	n, err := strconv.Atoi(s[:end])
	return n, err == nil
}

// stepRange is clamped, not wrapped: stepping off ALL onto 24H disorients.
func stepRange(current timerange.Key, delta int) timerange.Key {
	i := slices.Index(timerange.Keys, current)
	return timerange.Keys[min(len(timerange.Keys)-1, max(0, i+delta))]
}

func switchTab(s State, delta int) State {
	current := slices.Index(Tabs, s.Tab)
	s.Tab = Tabs[((current+delta)%len(Tabs)+len(Tabs))%len(Tabs)]
	s.Cursor, s.Offset = 0, 0
	return s
}

func moveCursor(s State, delta, viewportRows int) State {
	total := len(s.Entities)
	position := s.EntityIndex
	if s.SelectedEntity != nil {
		total = RowCount(s)
		position = s.Cursor
	}
	if total == 0 {
		return s
	}
	clamped := min(max(0, position+delta), total-1)
	if s.SelectedEntity == nil {
		s.EntityIndex, s.Cursor = clamped, clamped
	} else {
		s.Cursor = clamped
	}
	return clampScroll(s, viewportRows)
}

// clampScroll keeps the cursor visible, scrolling only when it leaves.
func clampScroll(s State, viewportRows int) State {
	rows := max(1, viewportRows)
	offset := s.Offset
	if s.Cursor < offset {
		offset = s.Cursor
	}
	if s.Cursor >= offset+rows {
		offset = s.Cursor - rows + 1
	}
	s.Offset = max(0, offset)
	return s
}

// themeOrDefault is the theme a state renders in: "" (as in a bare test
// state) renders with the terminal's own colours. The app always sets one.
func themeOrDefault(theme string) string {
	if t, _ := NormaliseTheme(theme); t != "" {
		return t
	}
	return ThemeTerminal
}
