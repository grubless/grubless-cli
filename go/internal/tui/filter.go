package tui

import (
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/grubless/grubless-cli/go/internal/jsstr"
)

// The Transactions tab's filter: what's applied, and the form that edits it.
// Every filter is one the API applies itself (GET /entities/:id/tx-events);
// the form only makes them easy to set without knowing a uuid or eighty
// category names by heart.

// TxFilter is the applied filter. Source and asset are kept as ids for the
// API and labels for the screen.
type TxFilter struct {
	Category    string `json:"category"`
	Direction   string `json:"direction"`
	From        string `json:"from"`
	To          string `json:"to"`
	Search      string `json:"search"`
	SourceID    string `json:"sourceId"`
	SourceLabel string `json:"sourceLabel"`
	AssetID     string `json:"assetId"`
	AssetLabel  string `json:"assetLabel"`
	Sort        string `json:"sort"`
}

// Active is how many filters are set. Sort isn't one: it reorders, it
// doesn't hide anything.
func (f TxFilter) Active() int {
	n := 0
	for _, v := range []string{f.Category, f.Direction, f.From, f.To, f.Search, f.SourceID, f.AssetID} {
		if v != "" {
			n++
		}
	}
	return n
}

// Query is the tx-events query string for one page.
func (f TxFilter) Query(limit int, cursor string) string {
	q := url.Values{}
	set := func(k, v string) {
		if v != "" {
			q.Set(k, v)
		}
	}
	set("category", f.Category)
	set("direction", f.Direction)
	set("from", f.From)
	set("to", f.To)
	set("q", f.Search)
	set("sourceId", f.SourceID)
	set("assetId", f.AssetID)
	set("sort", f.Sort)
	set("cursor", cursor)
	q.Set("limit", strconv.Itoa(limit))
	return q.Encode()
}

// Categories are the API's event types, as its validation error listed them
// on 2026-09-30. They drive completion and catch a typo before a request is
// made. If the server adds one, the form refuses it until this list catches
// up — `grubless transactions --category` passes anything through, so the
// command still reaches it.
var Categories = []string{
	"buy", "sell", "cross_chain_buy", "cross_chain_sell", "send", "receive", "transfer",
	"failed_in", "failed_out", "ignore_in", "ignore_out", "spam", "dust_in", "dust_out",
	"collateral_withdrawal", "remove_liquidity", "loan", "receive_receipt_token", "staking_reward",
	"voting_reward", "staking_withdrawal", "instant_unstake", "staking_deactivation", "staking_split",
	"staking_merge", "collateral_deposit", "add_liquidity", "loan_repayment", "loan_made",
	"send_receipt_token", "staking_deposit", "liquidation", "equity_vest", "equity_exercise",
	"fiat_deposit", "airdrop", "chain_split", "gift", "sales", "super_contribution_received", "income",
	"interest", "mining", "mint", "loan_repayment_received", "rebate", "royalties", "fiat_withdrawal",
	"burn", "lost", "outgoing_gift", "personal_use", "stolen", "card_purchase", "credit_purchase",
	"bill_payment", "bank_fee", "atm_withdrawal", "refund", "approval", "expense", "fee",
	"staking_activation", "wages", "super_contribution", "invoice_payment", "decrease_position",
	"receive_position_token", "realized_profit", "increase_position", "send_position_token",
	"realized_loss", "margin_fee", "incoming", "outgoing", "unknown", "trade", "wrapped_tokens",
	"reflection_tokens", "rebase_tokens", "cross_chain_trade",
}

// Form fields, in order. The last is the "Clear all filters" row.
const (
	fieldCategory = iota
	fieldDirection
	fieldFrom
	fieldTo
	fieldSource
	fieldAsset
	fieldSearch
	fieldSort
	fieldClear
	fieldCount
)

var fieldLabel = []string{"Category", "Direction", "From", "To", "Source", "Asset", "Search", "Sort", ""}

// option is one choice in a choice field.
type option struct{ id, label string }

// FilterForm is the open form: a draft of the filter, the focused field, and
// the choices its choice fields offer.
type FilterForm struct {
	Draft   TxFilter `json:"draft"`
	Field   int      `json:"field"`
	Error   string   `json:"error"`
	sources []option
	assets  []option
}

func isText(field int) bool {
	return field == fieldCategory || field == fieldFrom || field == fieldTo || field == fieldSearch
}

var directions = []option{{"", "Any"}, {"in", "In"}, {"out", "Out"}}
var sorts = []option{{"", "Newest first"}, {"asc", "Oldest first"}}

// openFilter builds the form from the applied filter and what's loaded: the
// entity's sources, and every asset it holds or that a loaded transaction
// touches. An asset in neither — sold long ago, and not yet scrolled to — isn't
// offered; the command's --asset takes its id.
func openFilter(s State) *FilterForm {
	f := &FilterForm{Draft: s.TxFilter}
	f.sources = []option{{"", "Any"}}
	for _, src := range s.Data.Sources {
		f.sources = append(f.sources, option{src.ID, src.Label})
	}

	seen := map[string]bool{}
	var assets []option
	add := func(id, symbol string, chain *string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		label := symbol
		if chain != nil && *chain != "" {
			label += " · " + *chain
		}
		assets = append(assets, option{id, label})
	}
	for _, h := range s.Data.Holdings {
		if !h.IsSpam {
			add(h.AssetID, h.Symbol, h.Chain)
		}
	}
	for _, a := range s.Data.TxAssets {
		add(a.ID, a.Symbol, a.Chain)
	}
	// The one applied now, even if neither list has it any more.
	if s.TxFilter.AssetID != "" {
		add(s.TxFilter.AssetID, s.TxFilter.AssetLabel, nil)
	}
	sort.SliceStable(assets, func(i, j int) bool { return strings.ToLower(assets[i].label) < strings.ToLower(assets[j].label) })
	f.assets = append([]option{{"", "Any"}}, assets...)
	return f
}

func (f *FilterForm) choices(field int) []option {
	switch field {
	case fieldDirection:
		return directions
	case fieldSource:
		return f.sources
	case fieldAsset:
		return f.assets
	case fieldSort:
		return sorts
	}
	return nil
}

// selected is the index of the draft's value among a choice field's options.
func (f *FilterForm) selected(field int) int {
	id := f.choiceID(field)
	for i, o := range f.choices(field) {
		if o.id == id {
			return i
		}
	}
	return 0
}

func (f *FilterForm) choiceID(field int) string {
	switch field {
	case fieldDirection:
		return f.Draft.Direction
	case fieldSource:
		return f.Draft.SourceID
	case fieldAsset:
		return f.Draft.AssetID
	case fieldSort:
		return f.Draft.Sort
	}
	return ""
}

func (f *FilterForm) choose(field, index int) {
	opts := f.choices(field)
	if len(opts) == 0 {
		return
	}
	o := opts[(index%len(opts)+len(opts))%len(opts)]
	label := o.label
	if o.id == "" {
		label = ""
	}
	switch field {
	case fieldDirection:
		f.Draft.Direction = o.id
	case fieldSource:
		f.Draft.SourceID, f.Draft.SourceLabel = o.id, label
	case fieldAsset:
		f.Draft.AssetID, f.Draft.AssetLabel = o.id, label
	case fieldSort:
		f.Draft.Sort = o.id
	}
}

func (f *FilterForm) text(field int) *string {
	switch field {
	case fieldCategory:
		return &f.Draft.Category
	case fieldFrom:
		return &f.Draft.From
	case fieldTo:
		return &f.Draft.To
	case fieldSearch:
		return &f.Draft.Search
	}
	return nil
}

// categoryToken is the category being typed: the text after the last comma.
func categoryToken(text string) string {
	if i := strings.LastIndex(text, ","); i >= 0 {
		return strings.TrimSpace(text[i+1:])
	}
	return strings.TrimSpace(text)
}

// CategoryMatches are the categories the token could be, in the API's order,
// leaving out any already chosen. Prefix matches come before substring ones,
// so "tra" offers "transfer" before "cross_chain_trade".
func CategoryMatches(text string) []string {
	token := strings.ToLower(categoryToken(text))
	chosen := map[string]bool{}
	for _, c := range strings.Split(text, ",") {
		chosen[strings.TrimSpace(strings.ToLower(c))] = true
	}
	var prefix, contains []string
	for _, c := range Categories {
		if chosen[c] {
			continue
		}
		switch {
		case token == "":
			// Nothing typed yet: no suggestions, rather than all eighty.
		case strings.HasPrefix(c, token):
			prefix = append(prefix, c)
		case strings.Contains(c, token):
			contains = append(contains, c)
		}
	}
	return append(prefix, contains...)
}

// Completion is what → would add to the category field: the rest of the
// first prefix match. Empty when there's nothing to complete.
func Completion(text string) string {
	token := strings.ToLower(categoryToken(text))
	if token == "" {
		return ""
	}
	for _, c := range CategoryMatches(text) {
		if strings.HasPrefix(c, token) && c != token {
			return c[len(token):]
		}
	}
	return ""
}

// normaliseCategories tidies the typed list — spaces, case, empty entries —
// and names the first entry that isn't a category.
func normaliseCategories(text string) (string, string) {
	var out []string
	known := map[string]bool{}
	for _, c := range Categories {
		known[c] = true
	}
	for _, c := range strings.Split(text, ",") {
		c = strings.ToLower(strings.TrimSpace(c))
		if c == "" {
			continue
		}
		if !known[c] {
			return "", c
		}
		out = append(out, c)
	}
	return strings.Join(out, ","), ""
}

// validate readies the draft to apply, or says what's wrong with it.
func (f *FilterForm) validate() (TxFilter, string) {
	d := f.Draft
	categories, unknown := normaliseCategories(d.Category)
	if unknown != "" {
		// The suggestion is drawn in grey after what's typed, which reads as
		// if it were already there. So a refusal points at it, rather than
		// completing it silently: this filters what someone is checking a
		// tax return against.
		if matches := CategoryMatches(unknown); len(matches) > 0 {
			return d, "Unknown category \"" + unknown + "\" — did you mean " + matches[0] + "? → completes it."
		}
		return d, "Unknown category \"" + unknown + "\" — type to see the ones that exist."
	}
	d.Category = categories
	d.From, d.To, d.Search = strings.TrimSpace(d.From), strings.TrimSpace(d.To), strings.TrimSpace(d.Search)
	from, fromOK := jsstr.ParseDate(d.From)
	to, toOK := jsstr.ParseDate(d.To)
	switch {
	case d.From != "" && !fromOK:
		return d, "From isn't a date. Use YYYY-MM-DD, or a full ISO timestamp."
	case d.To != "" && !toOK:
		return d, "To isn't a date. Use YYYY-MM-DD, or a full ISO timestamp."
	case fromOK && toOK && to.Before(from):
		return d, "To is before From."
	}
	return d, ""
}

// reduceFilter is every key while the form is open. The form is modal: keys
// edit it, and nothing reaches the tab behind it.
func reduceFilter(state State, key Key) (State, Action) {
	next := state
	f := *state.Filter
	f.Error = ""
	next.Filter = &f

	move := func(delta int) { f.Field = ((f.Field+delta)%fieldCount + fieldCount) % fieldCount }

	switch {
	case key.Name == "escape":
		next.Filter = nil
		return next, ActNone

	case key.Name == "return":
		if f.Field == fieldClear {
			f.Draft = TxFilter{}
		}
		applied, problem := f.validate()
		if problem != "" {
			f.Error = problem
			return next, ActNone
		}
		next.Filter = nil
		next.TxFilter = applied
		// The list is refetched from its first page under the new filter.
		next.Data.Transactions, next.Data.TxNextCursor, next.Data.TxLoadingMore = nil, nil, false
		next.Cursor, next.Offset, next.Loading = 0, 0, true
		return next, ActApplyFilter

	case key.Name == "down" || key.Name == "tab":
		move(1)
	case key.Name == "up":
		move(-1)

	case key.Ctrl && key.Name == "u":
		if t := f.text(f.Field); t != nil {
			*t = ""
		} else if f.choices(f.Field) != nil {
			f.choose(f.Field, 0)
		}

	case key.Name == "left" || key.Name == "right":
		switch {
		case f.choices(f.Field) != nil:
			delta := 1
			if key.Name == "left" {
				delta = -1
			}
			f.choose(f.Field, f.selected(f.Field)+delta)
		case f.Field == fieldCategory && key.Name == "right":
			// Like a shell's autosuggestion: → takes it, and a comma after
			// it starts the next.
			if rest := Completion(f.Draft.Category); rest != "" {
				f.Draft.Category += rest + ","
			}
		}

	case key.Name == "backspace":
		if t := f.text(f.Field); t != nil && *t != "" {
			r := []rune(*t)
			*t = string(r[:len(r)-1])
		}

	case len([]rune(key.Name)) == 1 && !key.Ctrl:
		if t := f.text(f.Field); t != nil {
			*t += key.Name
		} else if opts := f.choices(f.Field); opts != nil {
			// Type-ahead: the next option starting with this letter.
			letter := strings.ToLower(key.Name)
			start := f.selected(f.Field)
			for i := 1; i <= len(opts); i++ {
				o := opts[(start+i)%len(opts)]
				if strings.HasPrefix(strings.ToLower(o.label), letter) {
					f.choose(f.Field, (start+i)%len(opts))
					break
				}
			}
		}
	}
	return next, ActNone
}
