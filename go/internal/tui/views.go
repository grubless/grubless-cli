package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/grubless/grubless-cli/go/internal/api"
	"github.com/grubless/grubless-cli/go/internal/chart"
	"github.com/grubless/grubless-cli/go/internal/jsstr"
	"github.com/grubless/grubless-cli/go/internal/output"
	"github.com/grubless/grubless-cli/go/internal/timerange"
	"github.com/grubless/grubless-cli/go/internal/txfmt"
)

// Rendering: pure functions from state to lines. Render always returns
// exactly `height` lines, which is what lets the painter diff frame by frame.
// See src/tui/views.ts.

// Now is the clock the chart's ranges are measured from. A variable so tests
// can pin it; the TS reads `new Date()` at the same points.
var Now = time.Now

// paletteOf is the escape codes for the state's theme.
func paletteOf(s State) palette { return paletteFor(s.Theme, s.TrueColour) }

const chromeRows = 4 // title + tabs + column header + status

func Render(s State, width, height int) []string {
	p := paletteOf(s)
	if s.ShowHelp {
		return frame(helpLines(p), width, height)
	}
	if s.Filter != nil && s.SelectedEntity != nil {
		return frame(filterLines(s, height), width, height)
	}
	if s.Detail && s.SelectedEntity != nil && s.Cursor < len(s.Data.Transactions) {
		return frame(detailLines(s, width, height), width, height)
	}
	var body []string
	if s.SelectedEntity != nil {
		body = entityLines(s, width, height)
	} else {
		body = entityPickerLines(s, height)
	}
	return frame(body, width, height)
}

// frame pads or truncates to exactly height × width.
func frame(lines []string, width, height int) []string {
	out := make([]string, 0, max(height, 0))
	for i, line := range lines {
		if i >= height {
			break
		}
		out = append(out, Truncate(line, width))
	}
	for len(out) < height {
		out = append(out, "")
	}
	return out
}

// ViewportRows is what the list body gets after the chrome.
func ViewportRows(height int) int { return max(1, height-chromeRows) }

// ---------- entity picker ----------

func entityPickerLines(s State, height int) []string {
	p := paletteOf(s)
	lines := []string{p.title() + "  " + p.dim + "select an entity" + p.reset, ""}

	if s.Loading && len(s.Entities) == 0 {
		return append(lines, loadingLine(s))
	}
	if len(s.Entities) == 0 {
		return append(lines, "No entities available to this account.")
	}

	rows := ViewportRows(height)
	nameWidth := 4
	for _, e := range s.Entities {
		nameWidth = max(nameWidth, jsstr.Len(e.Name))
	}
	for i := s.Offset; i < min(len(s.Entities), s.Offset+rows); i++ {
		e := s.Entities[i]
		line := "  " + Pad(e.Name, nameWidth) + "   " + p.dim + Pad(e.EntityType, 12) + e.Role + p.reset
		if i == s.EntityIndex {
			line = p.selected(StripAnsi(line))
		}
		lines = append(lines, line)
	}

	lines = append(lines, "")
	return append(lines, statusBar(s, plural(len(s.Entities), "entity", "entities"), "↑↓ move · ⏎ open · ? help · q quit"))
}

// spinner is a turning star: the only thing that says "fetching", not "hung".
var spinner = []string{"✶", "✸", "✹", "✺", "✹", "✷"}

func loadingLine(s State) string {
	p := paletteOf(s)
	return p.cyan + spinner[s.Tick%len(spinner)] + p.reset + " " + p.dim + "Loading…" + p.reset
}

func plural(count int, one, many string) string {
	if count == 1 {
		return strconv.Itoa(count) + " " + one
	}
	return strconv.Itoa(count) + " " + many
}

// ---------- entity detail ----------

func entityLines(s State, width, height int) []string {
	p := paletteOf(s)
	e := s.SelectedEntity
	lines := []string{
		p.bold + e.Name + p.reset + "  " + p.dim + e.EntityType + " · " + e.Role + p.reset,
		tabBar(p, s.Tab),
	}

	rows := ViewportRows(height)
	header, body := tabContent(s, width, rows)

	// A list's header dims as a whole; the dashboard's carries its own colours.
	if IsList(s.Tab) {
		lines = append(lines, p.dim+header+p.reset)
	} else {
		lines = append(lines, header)
	}

	switch {
	case s.Loading:
		lines = append(lines, loadingLine(s))
	case !IsList(s.Tab):
		lines = append(lines, body...)
	case len(body) == 0 && s.Tab == "transactions" && s.TxFilter.Active() > 0:
		// Not "Nothing here": the ledger isn't empty, the filter is.
		lines = append(lines, p.dim+"No transactions match this filter — f to change it."+p.reset)
	case len(body) == 0:
		lines = append(lines, p.dim+"Nothing here."+p.reset)
	default:
		for i := s.Offset; i < min(len(body), s.Offset+rows); i++ {
			if i == s.Cursor {
				lines = append(lines, p.selected(StripAnsi(body[i])))
			} else {
				lines = append(lines, body[i])
			}
		}
	}

	for len(lines) < height-1 {
		lines = append(lines, "")
	}
	return append(lines, statusBar(s, countLabel(s), "↹ tab · r reload · s sync · ? help · q back"))
}

func rangedHistory(s State) []api.PortfolioHistoryPoint {
	return timerange.Filter(s.Data.History, func(p api.PortfolioHistoryPoint) string { return p.Date }, s.Range, s.Data.Settings.FYStartMonth(), Now())
}

// countLabel: the chart measures days, not rows.
func countLabel(s State) string {
	if s.Tab == "chart" {
		return plural(len(rangedHistory(s)), "day", "days")
	}
	if s.Tab == "transactions" {
		label := plural(RowCount(s), "row", "rows")
		if s.Data.TxNextCursor != nil {
			// "+": what's loaded so far, not the entity's total.
			label = strings.Replace(label, " ", "+ ", 1)
		}
		if n := s.TxFilter.Active(); n > 0 {
			label += " · " + plural(n, "filter", "filters") + " (f)"
		} else {
			label += " · f filter"
		}
		return label
	}
	return plural(RowCount(s), "row", "rows")
}

func tabBar(p palette, active Tab) string {
	var b strings.Builder
	for i, tab := range Tabs {
		label := " " + strconv.Itoa(i+1) + " " + TabLabel[tab] + " "
		if tab == active {
			b.WriteString(p.selected(label))
		} else {
			b.WriteString(p.dim + label + p.reset)
		}
	}
	return b.String()
}

func tabContent(s State, width, rows int) (string, []string) {
	switch s.Tab {
	case "chart":
		return dashboardTab(s, width, rows)
	case "holdings":
		return holdingsTab(s, width)
	case "warnings":
		return warningsTab(s, width)
	case "tax":
		return taxTab(s)
	case "sources":
		return sourcesTab(s, width)
	case "transactions":
		return transactionsTab(s, width)
	}
	return "", nil
}

const (
	// The chart's share of the tab; holdings take the rest.
	chartShare = 0.7
	// Header + one holding + "and N more" — anything less isn't a tile.
	minTileRows  = 3
	heldInWidth  = 22
	holdingsCols = 10
)

// dashboardTab is the portfolio over time with current holdings beneath —
// the web dashboard's two panels, in its order.
func dashboardTab(s State, width, rows int) (string, []string) {
	p := paletteOf(s)
	points := rangedHistory(s)

	header := "  " + p.dim + "no history" + p.reset
	if len(points) > 0 {
		last := points[len(points)-1]
		header = "  " + p.dim + last.Date + p.reset + "   " + p.value + "VALUE " + output.Money(last.Value) + p.reset +
			"   " + p.pnl + "UNREALISED " + output.Signed(last.UnrealizedPL) + p.reset +
			"   " + p.income + "INCOME " + output.Money(last.CumulativeIncome) + p.reset
	}

	// One row for the range strip, always: a chart whose window you can't see
	// is a chart you can misread.
	body := []string{rangeStrip(s)}
	remaining := rows - len(body)

	tileRows := remaining - int(jsstr.Round(float64(remaining)*chartShare))
	showTile := tileRows >= minTileRows && len(s.Data.Holdings) > 0
	chartRows := remaining
	if showTile {
		chartRows = remaining - tileRows
	}

	body = append(body, chart.Render(chart.FromHistory(points), chart.Options{Width: width, Height: chartRows, Colour: true, Palette: &p.chart})...)
	if showTile {
		body = append(body, holdingsTile(s, width, tileRows)...)
	}
	return Truncate(header, width), body
}

// rangeStrip shows the selected window, with its keys, as the web
// dashboard's segmented control.
func rangeStrip(s State) string {
	p := paletteOf(s)
	var b strings.Builder
	for _, r := range timerange.Keys {
		if r == s.Range {
			b.WriteString(p.selected(" " + timerange.Label[r] + " "))
		} else {
			b.WriteString(p.dim + " " + timerange.Label[r] + " " + p.reset)
		}
	}
	return "  " + b.String() + "  " + p.dim + "[ ]" + p.reset
}

func visibleHoldings(s State) []api.Holding {
	var out []api.Holding
	for _, h := range s.Data.Holdings {
		if !h.IsSpam {
			out = append(out, h)
		}
	}
	return out
}

// valueOf is Number(h.value), for ORDERING only — no figure that reaches
// the screen goes through it.
func valueOf(h api.Holding) float64 {
	if h.Value == nil {
		return 0
	}
	return jsstr.ParseNumber(*h.Value)
}

// holdingsTile is the largest holdings, counting what didn't fit rather than
// dropping it — four of thirty positions must not read as the portfolio.
func holdingsTile(s State, width, rows int) []string {
	p := paletteOf(s)
	sorted := visibleHoldings(s)
	// Stable, largest first; NaN compares as equal, as a JS comparator
	// returning NaN does.
	sort.SliceStable(sorted, func(i, j int) bool { return valueOf(sorted[j])-valueOf(sorted[i]) < 0 })

	lines := []string{"  " + p.dim + Pad("HOLDINGS", holdingsCols) + Pad("HELD IN", heldInWidth) + padLeft("QUANTITY", 18) + padLeft("VALUE", 16) + p.reset}

	// One row kept back for "and N more" whenever there is a remainder.
	slots := rows - 1
	if len(sorted) > rows-1 {
		slots = rows - 2
	}
	shown := max(0, slots)
	for _, h := range sorted[:min(shown, len(sorted))] {
		lines = append(lines, holdingLine(p, h))
	}
	if remaining := len(sorted) - shown; remaining > 0 {
		lines = append(lines, "  "+p.dim+"and "+strconv.Itoa(remaining)+" more — press 2"+p.reset)
	}

	if len(lines) > rows {
		lines = lines[:max(0, rows)]
	}
	for i := range lines {
		lines[i] = Truncate(lines[i], width)
	}
	return lines
}

func holdingLine(p palette, h api.Holding) string {
	flag := ""
	if h.HasMismatch {
		flag = "  " + p.yellow + "mismatch" + p.reset
	}
	// Truncated to the column, not just padded, so a long source label
	// can't shove later columns out of line.
	where := output.HeldIn(h.Chain, h.SourceLabels(), heldInWidth-1)
	return "  " + Pad(h.Symbol, holdingsCols) + Pad(where, heldInWidth) + padLeft(output.Qty(h.Quantity), 18) + padLeft(output.Money(h.Value), 16) + flag
}

func holdingsTab(s State, width int) (string, []string) {
	p := paletteOf(s)
	header := "  " + Pad("ASSET", holdingsCols) + Pad("HELD IN", heldInWidth) + padLeft("QUANTITY", 18) + padLeft("VALUE", 16)
	var body []string
	for _, h := range visibleHoldings(s) {
		body = append(body, holdingLine(p, h))
	}
	return Truncate(header, width), body
}

func warningsTab(s State, width int) (string, []string) {
	p := paletteOf(s)
	header := "  " + Pad("KIND", 16) + Pad("DATE", 12) + Pad("ASSET", 10) + padLeft("AMOUNT", 18)
	var body []string
	// Blocking categories first, labelled: the ordering IS the message.
	for _, w := range s.Data.ZeroCost {
		body = append(body, "  "+p.yellow+Pad("no cost basis", 16)+p.reset+Pad(jsstr.Slice(w.Ts, 0, 10), 12)+Pad(w.AssetSymbol, 10)+padLeft(output.Qty(w.Quantity), 18)+"   "+p.dim+w.SourceLabel+p.reset)
	}
	for _, w := range s.Data.Uncategorized {
		body = append(body, "  "+p.yellow+Pad("uncategorised", 16)+p.reset+Pad(jsstr.Slice(w.Ts, 0, 10), 12)+Pad(w.AssetSymbol, 10)+padLeft(output.Qty(w.Amount), 18)+"   "+p.dim+w.Direction+p.reset)
	}
	return Truncate(header, width), body
}

func taxTab(s State) (string, []string) {
	header := "  " + Pad("FY", 10) + padLeft("INCOME", 16) + padLeft("NET CGT", 16) + padLeft("TAXABLE", 16) + padLeft("TAX", 14)
	var body []string
	for _, r := range s.Data.Tax {
		// A pass-through entity has no entity-level tax: "n/a", not "0.00".
		tax := "n/a"
		if r.Applicable {
			tax = output.Money(r.TaxPayable)
		}
		body = append(body, "  "+Pad(r.FinancialYear, 10)+padLeft(output.Money(r.Income), 16)+padLeft(output.Money(r.NetCapitalGainLoss), 16)+padLeft(output.Money(r.TaxableAmount), 16)+padLeft(tax, 14))
	}
	return header, body
}

func sourcesTab(s State, width int) (string, []string) {
	p := paletteOf(s)
	header := "  " + Pad("LABEL", 24) + Pad("ADAPTER", 18) + padLeft("TXNS", 8) + "   " + Pad("SYNCED", 12) + "STATUS"
	var body []string
	for _, src := range s.Data.Sources {
		status := src.SyncStatus
		switch {
		case !src.SyncEnabled:
			status = p.dim + "off" + p.reset
		case src.SyncStatus == "error":
			status = p.red + "error" + p.reset
		case src.SyncStatus == "syncing":
			status = p.cyan + "syncing" + p.reset
		}
		synced := "never"
		if src.LastSyncedAt != nil && *src.LastSyncedAt != "" {
			synced = jsstr.Slice(*src.LastSyncedAt, 0, 10)
		}
		count := "undefined"
		if src.TransactionCount != nil {
			count = jsstr.Number(*src.TransactionCount)
		}
		body = append(body, "  "+Pad(src.Label, 24)+Pad(src.AdapterKey, 18)+padLeft(count, 8)+"   "+Pad(synced, 12)+status)
	}
	return Truncate(header, width), body
}

// ---------- transactions ----------

// Sized so a 100-column terminal shows every figure; only the source label,
// last, gets cut.
const (
	txWhenWidth = 17
	txTypeWidth = 21
	txLegWidth  = 22
	txGainWidth = 13
)

func txAssets(s State) txfmt.Assets {
	if s.Data.TxAssets == nil {
		return txfmt.Assets{}
	}
	return s.Data.TxAssets
}

func transactionsTab(s State, width int) (string, []string) {
	p := paletteOf(s)
	header := "  " + Pad("DATE (UTC)", txWhenWidth) + Pad("TYPE", txTypeWidth) + padLeft("OUT", txLegWidth) + padLeft("IN", txLegWidth) + padLeft("GAIN/LOSS", txGainWidth) + "  SOURCE"
	assets := txAssets(s)
	var body []string
	for _, e := range s.Data.Transactions {
		body = append(body, "  "+
			Pad(txfmt.When(e.Ts), txWhenWidth)+
			Pad(output.Ellipsize(txfmt.Type(e), txTypeWidth-1), txTypeWidth)+
			padLeft(output.Ellipsize(txfmt.Legs(e, "out", assets), txLegWidth-1), txLegWidth)+
			padLeft(output.Ellipsize(txfmt.Legs(e, "in", assets), txLegWidth-1), txLegWidth)+
			padLeft(output.Signed(txfmt.GainLoss(e)), txGainWidth)+
			"  "+p.dim+txfmt.Source(e)+p.reset)
	}
	// A trailing line, not a row: the cursor never lands on it.
	if s.Data.TxLoadingMore && len(body) > 0 {
		body = append(body, "  "+p.dim+"loading more…"+p.reset)
	}
	return Truncate(header, width), body
}

// detailLines is one transaction in full: every leg with its exact figures,
// which the list can only summarise.
func detailLines(s State, width, height int) []string {
	p := paletteOf(s)
	e := s.Data.Transactions[s.Cursor]
	assets := txAssets(s)
	opt := func(v *string) string {
		if v == nil || *v == "" {
			return p.dim + "—" + p.reset
		}
		return *v
	}
	field := func(label, value string) string { return "  " + p.dim + Pad(label, 15) + p.reset + value }

	lines := []string{
		p.bold + txfmt.Type(e) + p.reset + "  " + p.dim + txfmt.When(e.Ts) + " UTC" + p.reset,
		"",
		field("ID", e.ID),
		field("Source", txfmt.Source(e)),
		field("Tax treatment", opt(e.TaxTreatment)),
		field("Protocol", opt(e.DetectedProtocol)),
		field("Tags", txfmt.Tags(e)),
		field("Notes", opt(e.Notes)),
		field("Description", opt(e.Description)),
		field("Gain/loss", output.Signed(txfmt.GainLoss(e))),
	}
	if e.IsManuallyCategorized {
		lines = append(lines, field("", p.dim+"categorised by hand"+p.reset))
	}

	// Sized to fit 100 columns with every figure whole: 97 wide.
	lines = append(lines, "", "  "+p.dim+
		Pad("DIR", 5)+Pad("ROLE", 9)+padLeft("AMOUNT", 18)+"  "+Pad("ASSET", 9)+
		padLeft("VALUE", 13)+padLeft("PROCEEDS", 13)+padLeft("COST BASIS", 13)+padLeft("GAIN/LOSS", 13)+p.reset)
	for _, leg := range e.Legs {
		lines = append(lines, "  "+
			Pad(leg.Direction, 5)+Pad(output.Ellipsize(leg.Role, 8), 9)+padLeft(output.Qty(leg.Amount), 18)+"  "+
			Pad(output.Ellipsize(assets.Symbol(leg.AssetID), 8), 9)+
			padLeft(output.Money(leg.Value), 13)+padLeft(output.Money(leg.Proceeds), 13)+
			padLeft(output.Money(leg.CostBasis), 13)+padLeft(output.Signed(leg.GainLoss), 13))
	}

	for len(lines) < height-1 {
		lines = append(lines, "")
	}
	position := fmt.Sprintf("%d of %d", s.Cursor+1, len(s.Data.Transactions))
	if s.Data.TxNextCursor != nil {
		position += "+"
	}
	return append(lines, statusBar(s, position, "↑↓ previous / next · Esc back"))
}

// ---------- filter form ----------

var fieldHint = map[int]string{
	fieldCategory: "any — type to search, e.g. transfer,send",
	fieldFrom:     "any — YYYY-MM-DD",
	fieldTo:       "any — YYYY-MM-DD",
	fieldSearch:   "any — free text",
}

func filterLines(s State, height int) []string {
	p := paletteOf(s)
	f := s.Filter
	lines := []string{
		p.bold + "Filter transactions" + p.reset + "  " + p.dim + s.SelectedEntity.Name + p.reset,
		"",
	}
	marker := func(field int) string {
		if f.Field == field {
			return p.cyan + "▸ " + p.reset
		}
		return "  "
	}

	for field := 0; field < fieldClear; field++ {
		focused := f.Field == field
		label := Pad(fieldLabel[field], 11)
		if focused {
			label = p.bold + label + p.reset
		}

		var value string
		if t := f.text(field); t != nil {
			switch {
			case *t == "" && !focused:
				value = p.dim + fieldHint[field] + p.reset
			case focused:
				value = *t + p.cyan + "█" + p.reset
				if field == fieldCategory {
					value = *t + p.dim + Completion(*t) + p.reset + p.cyan + "█" + p.reset
				}
			default:
				value = *t
			}
		} else {
			o := f.choices(field)[f.selected(field)]
			switch {
			case focused:
				value = p.cyan + "‹ " + p.reset + o.label + p.cyan + " ›" + p.reset
			case o.id == "":
				value = p.dim + o.label + p.reset
			default:
				value = o.label
			}
		}
		lines = append(lines, "  "+marker(field)+label+" "+value)

		// Under the category field, what the typed text could be.
		if field == fieldCategory && focused {
			token := categoryToken(f.Draft.Category)
			matches := CategoryMatches(f.Draft.Category)
			indent := strings.Repeat(" ", 16)
			switch {
			case token == "":
				lines = append(lines, indent+p.dim+"type to search "+strconv.Itoa(len(Categories))+" categories · → completes · , for another"+p.reset)
			case len(matches) == 0:
				lines = append(lines, indent+p.red+"no category matches “"+token+"”"+p.reset)
			default:
				shown := matches[:min(8, len(matches))]
				more := ""
				if len(matches) > len(shown) {
					more = " +" + strconv.Itoa(len(matches)-len(shown)) + " more"
				}
				lines = append(lines, indent+p.dim+strings.Join(shown, ", ")+more+p.reset)
			}
		}
	}

	lines = append(lines, "", "  "+marker(fieldClear)+"Clear all filters")
	if f.Error != "" {
		lines = append(lines, "", "  "+p.red+f.Error+p.reset)
	}
	for len(lines) < height-1 {
		lines = append(lines, "")
	}
	return append(lines, p.dim+"↑↓ field · ←→ change · type to edit · ^U clear field · ⏎ apply · Esc cancel"+p.reset)
}

// ---------- chrome ----------

func statusBar(s State, left, hints string) string {
	p := paletteOf(s)
	if s.Message != nil && *s.Message != "" {
		colour := p.cyan
		switch s.MessageKind {
		case "error":
			colour = p.red
		case "success":
			colour = p.green
		}
		return colour + *s.Message + p.reset
	}
	if s.Syncing {
		return p.cyan + "syncing…" + p.reset + "  " + p.dim + hints + p.reset
	}
	return p.dim + left + "  ·  " + hints + p.reset
}

func helpLines(p palette) []string {
	return []string{
		p.bold + "Grubless — keys" + p.reset,
		"",
		"  ↑ ↓ / k j      move",
		"  PgUp PgDn      page",
		"  Home End       jump to first / last",
		"  ⏎              open the selected entity",
		"  ↹ / ← → / h l  switch tab",
		"  1..6           jump to a tab",
		"  ⏎              open the selected transaction (Transactions tab)",
		"  [ ]            narrow / widen the chart's time range",
		"  r              reload this entity",
		"  t              switch theme (Terminal / Cypherpunk)",
		"  f or /         filter transactions (Transactions tab)",
		"  s              sync every enabled source, and watch it",
		"  q / Esc        back to entities, or quit from there",
		"  Ctrl-C         quit immediately",
		"",
		"  " + p.dim + "Scriptable commands still exist: grubless --help" + p.reset,
		"",
		p.dim + "press any key" + p.reset,
	}
}

func padLeft(text string, width int) string {
	if gap := width - VisibleWidth(text); gap > 0 {
		return strings.Repeat(" ", gap) + text
	}
	return text
}
