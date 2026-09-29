package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/grubless/grubless-cli/internal/api"
	"github.com/grubless/grubless-cli/internal/chart"
	"github.com/grubless/grubless-cli/internal/clock"
	"github.com/grubless/grubless-cli/internal/jsstr"
	"github.com/grubless/grubless-cli/internal/output"
	"github.com/grubless/grubless-cli/internal/timerange"
	"github.com/grubless/grubless-cli/internal/txfmt"
)

// Rendering: pure functions from state to lines. Render always returns
// exactly `height` lines, which is what lets the painter diff frame by frame.

// Now is the clock the chart's ranges and "3 hours ago" are measured from.
// A variable so tests can pin it; GRUBLESS_NOW pins it for the scenario
// tests (see internal/clock).
var Now = clock.Now

// paletteOf is the escape codes for the state's theme.
func paletteOf(s State) palette { return paletteFor(s.Theme, s.TrueColour) }

// chromeRows is what a list screen spends outside its rows: the title, the
// tab bar and the status bar, then the list card's column header and its
// border and shadow rows.
const chromeRows = 3 + 1 + cardExtraRows

func Render(s State, width, height int) []string {
	p := paletteOf(s)
	switch {
	case s.ShowHelp:
		return frame(helpScreen(s, p, width, height), width, height)
	case s.SelectedEntity == nil:
		return frame(entityPickerLines(s, width, height), width, height)
	}
	head := []string{entityHeading(s, p), tabBar(p, s.Tab)}
	var content []string
	status := statusBar(s, countLabel(s), "↹ tab · r reload · s sync · ? help · q back")
	switch {
	case s.Filter != nil:
		content = filterCard(s, p, width, height-3)
		status = p.dim + "↑↓ field · ←→ change · type to edit · ^U clear field · ⏎ apply · Esc cancel" + p.reset
	case s.Detail && s.Cursor < len(s.Data.Transactions):
		content = detailCard(s, p, width, height-3)
		position := fmt.Sprintf("%d of %d", s.Cursor+1, len(s.Data.Transactions))
		if s.Data.TxNextCursor != nil {
			position += "+"
		}
		status = statusBar(s, position, "↑↓ previous / next · Esc back")
	case s.Tab == "chart":
		content = overview(s, p, width, height-3)
	default:
		content = listCard(s, p, width, height-3)
	}
	return frame(page(head, content, status, height), width, height)
}

// page stacks a screen: its heading lines, content filling what's left, and
// the status bar on the last line.
func page(head, content []string, status string, height int) []string {
	lines := append(append([]string{}, head...), padLines(content, max(0, height-len(head)-1))...)
	return append(lines, status)
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

// ViewportRows is how many rows a list shows, after the chrome.
func ViewportRows(height int) int { return max(1, height-chromeRows) }

// ---------- entity picker ----------

func entityPickerLines(s State, width, height int) []string {
	p := paletteOf(s)
	head := []string{p.title() + "  " + p.dim + "select an entity" + p.reset, ""}
	status := statusBar(s, plural(len(s.Entities), "entity", "entities"), "↑↓ move · ⏎ open · t theme · ? help · q quit")

	var body []string
	switch {
	case s.Loading && len(s.Entities) == 0:
		body = []string{loadingLine(s)}
	case len(s.Entities) == 0:
		body = []string{"No entities available to this account."}
	default:
		rows := ViewportRows(height)
		nameWidth := 4
		for _, e := range s.Entities {
			nameWidth = max(nameWidth, jsstr.Len(e.Name))
		}
		inner := p.cardInner(width)
		body = []string{p.dim + Pad("NAME", nameWidth) + "   " + Pad("TYPE", 12) + "ROLE" + p.reset}
		for i := s.Offset; i < min(len(s.Entities), s.Offset+rows); i++ {
			e := s.Entities[i]
			line := Pad(e.Name, nameWidth) + "   " + p.dim + Pad(e.EntityType, 12) + e.Role + p.reset
			if i == s.EntityIndex {
				line = p.selected(Pad(StripAnsi(line), inner))
			}
			body = append(body, line)
		}
	}
	content := p.card("Entities", plural(len(s.Entities), "entity", "entities"), width, padLines(body, max(1, height-len(head)-1-cardExtraRows)))
	return page(head, content, status, height)
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

// ---------- entity screen ----------

func entityHeading(s State, p palette) string {
	e := s.SelectedEntity
	return p.bold + e.Name + p.reset + "  " + p.dim + e.EntityType + " · " + e.Role + p.reset
}

// listCard is a list tab: its rows in a card, under a column header, with the
// cursor's row highlighted across the card's width.
func listCard(s State, p palette, width, height int) []string {
	title, right := tabCardTitle(s)
	rows := max(1, height-cardExtraRows-1)
	inner := p.cardInner(width)
	header, body := tabContent(s, width)

	lines := []string{p.dim + header + p.reset}
	switch {
	case s.Loading:
		lines = append(lines, loadingLine(s))
	case len(body) == 0 && s.Tab == "transactions" && s.TxFilter.Active() > 0:
		// Not "Nothing here": the ledger isn't empty, the filter is.
		lines = append(lines, p.dim+"No transactions match this filter — f to change it."+p.reset)
	case len(body) == 0:
		lines = append(lines, p.dim+"Nothing here."+p.reset)
	default:
		for i := s.Offset; i < min(len(body), s.Offset+rows); i++ {
			if i == s.Cursor {
				lines = append(lines, p.selected(Pad(StripAnsi(body[i]), inner)))
			} else {
				lines = append(lines, body[i])
			}
		}
	}
	return p.card(title, right, width, padLines(lines, rows+1))
}

// tabCardTitle is each list's card title, as the web titles the same card,
// and the figure its border carries.
func tabCardTitle(s State) (string, string) {
	switch s.Tab {
	case "holdings":
		var values []*string
		for _, h := range visibleHoldings(s) {
			values = append(values, h.Value)
		}
		if total := output.SumDecimals(values...); total != nil {
			return "Current holdings", "total " + output.Money(total)
		}
		return "Current holdings", ""
	case "warnings":
		n := len(s.Data.ZeroCost) + len(s.Data.Uncategorized)
		if n == 0 {
			return "Blocking issues", "none"
		}
		return "Blocking issues", plural(n, "to review", "to review")
	case "tax":
		return "Tax summary by financial year", ""
	case "sources":
		return "Sources", plural(len(s.Data.Sources), "connected", "connected")
	case "transactions":
		if n := s.TxFilter.Active(); n > 0 {
			return "Transactions", plural(n, "filter", "filters")
		}
		return "Transactions", ""
	}
	return "", ""
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
	labels := make([]string, len(Tabs))
	current := 0
	for i, tab := range Tabs {
		labels[i] = strconv.Itoa(i+1) + " " + TabLabel[tab]
		if tab == active {
			current = i
		}
	}
	return p.tabs(labels, current)
}

func tabContent(s State, width int) (string, []string) {
	switch s.Tab {
	case "holdings":
		return holdingsTab(s)
	case "warnings":
		return warningsTab(s)
	case "tax":
		return taxTab(s)
	case "sources":
		return sourcesTab(s)
	case "transactions":
		return transactionsTab(s)
	}
	return "", nil
}

const (
	heldInWidth  = 22
	holdingsCols = 10
)

// ---------- overview ----------

// overview is the web dashboard's first tab, in its order: the portfolio
// chart with the activity breakdown beside it, then a row of headline
// figures. On a narrow terminal the breakdown stands down first, then the
// figures, so the chart keeps its room.
func overview(s State, p palette, width, height int) []string {
	statRows := 0
	switch {
	case height >= 16 && width >= 76:
		statRows = statCardRows
	case height >= 26 && width >= 40:
		statRows = 2 * statCardRows // two rows of two
	}
	chartHeight := height - statRows

	var top []string
	if width >= 110 {
		widths := split(width, 1, 2, 1)
		top = hjoin(1, widths, portfolioCard(s, p, widths[0], chartHeight), breakdownCard(s, p, widths[1], chartHeight))
	} else {
		top = portfolioCard(s, p, width, chartHeight)
	}
	if statRows == 0 {
		return top
	}
	return append(top, statCards(s, p, width, statRows)...)
}

func rangedHistory(s State) []api.PortfolioHistoryPoint {
	return timerange.Filter(s.Data.History, func(p api.PortfolioHistoryPoint) string { return p.Date }, s.Range, s.Data.Settings.FYStartMonth(), Now())
}

// portfolioCard: the headline figures, the range control, and the chart.
func portfolioCard(s State, p palette, width, height int) []string {
	points := rangedHistory(s)
	inner := p.cardInner(width)
	rows := max(1, height-cardExtraRows)

	headline := p.dim + "no history in this range" + p.reset
	if len(points) > 0 {
		last := points[len(points)-1]
		headline = p.bold + p.value + output.Money(last.Value) + p.reset +
			"   " + p.pnl + "unrealised " + output.Signed(last.UnrealizedPL) + p.reset +
			"   " + p.income + "income " + output.Money(last.CumulativeIncome) + p.reset
	}
	body := []string{headline, rangeStrip(s, p)}
	if s.Loading {
		body = append(body, loadingLine(s))
	} else if chartRows := rows - len(body); chartRows >= 3 {
		body = append(body, chart.Render(chart.FromHistory(points), chart.Options{Width: inner, Height: chartRows, Colour: true, Palette: &p.chart})...)
	}
	date := ""
	if len(points) > 0 {
		date = points[len(points)-1].Date
	}
	return p.card("Portfolio value", date, width, padLines(body, rows))
}

// rangeStrip is the web's range toggle: the same segmented control as the
// tab bar, with the keys that step it.
func rangeStrip(s State, p palette) string {
	labels := make([]string, len(timerange.Keys))
	current := 0
	for i, r := range timerange.Keys {
		labels[i] = timerange.Label[r]
		if r == s.Range {
			current = i
		}
	}
	return p.tabs(labels, current) + "  " + p.dim + "[ ]" + p.reset
}

// maxNamedSlices is the web breakdown's: five named, the rest as "Other".
const maxNamedSlices = 5

type slice struct {
	label string
	value float64
}

// breakdownSlices totals the breakdown's category buckets over the chart's
// range, largest first — computeSlices in the web's category-pie-chart.tsx.
func breakdownSlices(s State) []slice {
	b := s.Data.Breakdown
	if b == nil {
		return nil
	}
	cutoff := ""
	if start, ok := timerange.StartDate(s.Range, s.Data.Settings.FYStartMonth(), Now()); ok {
		cutoff = start.Format("2006-01-02")
	}
	totals := map[string]float64{}
	var order []string
	for _, row := range b.Category {
		if cutoff != "" && row.Day < cutoff {
			continue
		}
		if _, seen := totals[row.Key]; !seen {
			order = append(order, row.Key)
		}
		totals[row.Key] += row.Value
	}
	var out []slice
	for _, k := range order {
		if totals[k] != 0 {
			out = append(out, slice{txfmt.CategoryLabel(k), totals[k]})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].value > out[j].value })
	return out
}

// breakdownCard is the web's "Activity breakdown": what the entity's
// transactions were, by value, over the chart's range. A bar per category
// where the web draws a donut — a terminal draws bars better — in the same
// colours, the top five named and the rest as Other.
func breakdownCard(s State, p palette, width, height int) []string {
	rows := max(1, height-cardExtraRows)
	inner := p.cardInner(width)
	var body []string
	switch {
	case s.Data.BreakdownFailed:
		body = []string{p.dim + "Couldn't load the breakdown." + p.reset}
	case s.Data.Breakdown == nil:
		body = []string{p.dim + "Loading…" + p.reset}
	default:
		slices := breakdownSlices(s)
		if len(slices) == 0 {
			body = []string{p.dim + "No activity in this range." + p.reset}
			break
		}
		// Named rows, then Other for the rest — fewer named on a short card,
		// so nothing is dropped, only folded.
		named := min(maxNamedSlices, len(slices), max(1, rows-1))
		if len(slices) > named {
			named = min(named, max(1, rows-2))
		}
		shown := slices[:named]
		other := 0.0
		for _, sl := range slices[named:] {
			other += sl.value
		}
		top := slices[0].value
		if other > top {
			top = other
		}
		labelWidth := min(18, max(6, inner*2/5))
		valueWidth := 8
		barWidth := max(1, inner-2-labelWidth-1-valueWidth-1)
		row := func(colour, label string, v float64) string {
			return colour + "■" + p.reset + " " + Pad(output.Ellipsize(label, labelWidth), labelWidth) + " " +
				colour + bar(v/top, barWidth) + p.reset + " " + padLeft(chart.CompactMoney(v), valueWidth)
		}
		for i, sl := range shown {
			body = append(body, row(p.cat[i%len(p.cat)], sl.label, sl.value))
		}
		if len(slices) > named {
			body = append(body, row(p.dim, "Other ("+strconv.Itoa(len(slices)-named)+")", other))
		}
	}
	return p.card("Activity breakdown", timerange.Label[s.Range], width, padLines(body, rows))
}

// bar is a horizontal bar `fraction` of `width` long, in eighth-cell steps.
func bar(fraction float64, width int) string {
	if fraction < 0 {
		fraction = 0
	}
	eighths := int(jsstr.Round(fraction * float64(width*8)))
	if eighths == 0 && fraction > 0 {
		eighths = 1 // something, however small, is not nothing
	}
	partials := []string{"", "▏", "▎", "▍", "▌", "▋", "▊", "▉"}
	out := strings.Repeat("█", eighths/8) + partials[eighths%8]
	return Pad(out, width)
}

// statCards are the web's four headline figures, one card each.
func statCards(s State, p palette, width, rows int) []string {
	type stat struct{ label, value, note string }
	loading := func(failed bool) string {
		if failed {
			return "—"
		}
		return "…"
	}

	txns := loading(s.Data.BreakdownFailed)
	if b := s.Data.Breakdown; b != nil && !s.Data.BreakdownFailed {
		txns = output.Qty(output.Str(strconv.Itoa(b.TotalEvents)))
	}

	coverage, coverageNote := loading(s.Data.CoverageFailed), ""
	if c := s.Data.Coverage; c != nil && !s.Data.CoverageFailed {
		coverage = strconv.Itoa(c.Priced) + " / " + strconv.Itoa(c.Total)
		switch {
		case c.Total == 0:
			coverage, coverageNote = "—", p.dim+"nothing to price"+p.reset
		case c.Missing == 0:
			coverageNote = p.dim + "all priced" + p.reset
		default:
			coverageNote = p.yellow + strconv.Itoa(c.Missing) + " missing" + p.reset
		}
	}

	off := 0
	for _, src := range s.Data.Sources {
		if !src.SyncEnabled {
			off++
		}
	}
	sourcesNote := ""
	if off > 0 {
		sourcesNote = p.dim + strconv.Itoa(off) + " switched off" + p.reset
	}
	synced, syncedAt := lastSynced(s.Data.Sources)
	syncedNote := ""
	if !syncedAt.IsZero() {
		syncedNote = p.dim + jsstr.ISODay(syncedAt) + p.reset
	}

	stats := []stat{
		{"Connected sources", strconv.Itoa(len(s.Data.Sources)), sourcesNote},
		{"Transactions", txns, p.dim + "all time" + p.reset},
		{"Last synced", synced, syncedNote},
		{"Price coverage", coverage, coverageNote},
	}
	if s.Loading {
		for i := range stats {
			stats[i].value, stats[i].note = "…", ""
		}
	}

	// The figure, and a note beneath it — beneath rather than beside, as the
	// web's cards wrap theirs on a narrow screen: "412 / 415  3 missing"
	// doesn't fit a quarter of 100 columns.
	cardFor := func(st stat, w int) []string {
		return p.card(st.label, "", w, []string{p.bold + st.value + p.reset, st.note})
	}
	perRow := 4
	if rows > statCardRows {
		perRow = 2
	}
	var out []string
	for i := 0; i < len(stats); i += perRow {
		widths := split(width, 1, repeat(1, perRow)...)
		var blocks [][]string
		for j := 0; j < perRow; j++ {
			blocks = append(blocks, cardFor(stats[i+j], widths[j]))
		}
		out = append(out, hjoin(1, widths, blocks...)...)
	}
	return out
}

// statCardRows is a stat card's height: figure, note, and the card around them.
const statCardRows = 2 + cardExtraRows

func repeat(v, n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = v
	}
	return out
}

// lastSynced is the most recent sync across the sources, in the web's words
// (describeStaleness in the main repo's client-core): "3 hours ago", "just
// now", and "Never" when nothing has synced. The instant comes back too, for
// the card's note.
func lastSynced(sources []api.Source) (string, time.Time) {
	var latest time.Time
	for _, src := range sources {
		if src.LastSyncedAt == nil {
			continue
		}
		if t, ok := jsstr.ParseDate(*src.LastSyncedAt); ok && t.After(latest) {
			latest = t
		}
	}
	if latest.IsZero() {
		return "Never", latest
	}
	minutes := int(Now().Sub(latest).Minutes())
	unit := func(n int, word string) string {
		if n == 1 {
			return "1 " + word + " ago"
		}
		return strconv.Itoa(n) + " " + word + "s ago"
	}
	switch {
	case minutes < 1:
		return "just now", latest
	case minutes < 60:
		return unit(minutes, "minute"), latest
	case minutes < 60*24:
		return unit(minutes/60, "hour"), latest
	}
	return unit(minutes/60/24, "day"), latest
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

func holdingLine(p palette, h api.Holding) string {
	flag := ""
	if h.HasMismatch {
		flag = "  " + p.yellow + "mismatch" + p.reset
	}
	// Truncated to the column, not just padded, so a long source label
	// can't shove later columns out of line.
	where := output.HeldIn(h.Chain, h.SourceLabels(), heldInWidth-1)
	// The symbol is cut to its column too: an unresolved token's is its
	// 42-character contract address, which ran into HELD IN when the TUI
	// still matched the Node build's screens — that TUI never cut it.
	return Pad(output.Ellipsize(h.Symbol, holdingsCols-1), holdingsCols) + Pad(where, heldInWidth) + padLeft(output.Qty(h.Quantity), 18) + padLeft(output.Money(h.Value), 16) + flag
}

func holdingsTab(s State) (string, []string) {
	p := paletteOf(s)
	header := Pad("ASSET", holdingsCols) + Pad("HELD IN", heldInWidth) + padLeft("QUANTITY", 18) + padLeft("VALUE", 16)
	var body []string
	for _, h := range visibleHoldings(s) {
		body = append(body, holdingLine(p, h))
	}
	return header, body
}

func warningsTab(s State) (string, []string) {
	p := paletteOf(s)
	header := Pad("KIND", 16) + Pad("DATE", 12) + Pad("ASSET", 10) + padLeft("AMOUNT", 18)
	var body []string
	// Blocking categories first, labelled: the ordering IS the message.
	for _, w := range s.Data.ZeroCost {
		body = append(body, p.yellow+Pad("no cost basis", 16)+p.reset+Pad(jsstr.Slice(w.Ts, 0, 10), 12)+Pad(w.AssetSymbol, 10)+padLeft(output.Qty(w.Quantity), 18)+"   "+p.dim+w.SourceLabel+p.reset)
	}
	for _, w := range s.Data.Uncategorized {
		body = append(body, p.yellow+Pad("uncategorised", 16)+p.reset+Pad(jsstr.Slice(w.Ts, 0, 10), 12)+Pad(w.AssetSymbol, 10)+padLeft(output.Qty(w.Amount), 18)+"   "+p.dim+w.Direction+p.reset)
	}
	return header, body
}

func taxTab(s State) (string, []string) {
	header := Pad("FY", 10) + padLeft("INCOME", 16) + padLeft("NET CGT", 16) + padLeft("TAXABLE", 16) + padLeft("TAX", 14)
	var body []string
	for _, r := range s.Data.Tax {
		// A pass-through entity has no entity-level tax: "n/a", not "0.00".
		tax := "n/a"
		if r.Applicable {
			tax = output.Money(r.TaxPayable)
		}
		body = append(body, Pad(r.FinancialYear, 10)+padLeft(output.Money(r.Income), 16)+padLeft(output.Money(r.NetCapitalGainLoss), 16)+padLeft(output.Money(r.TaxableAmount), 16)+padLeft(tax, 14))
	}
	return header, body
}

func sourcesTab(s State) (string, []string) {
	p := paletteOf(s)
	header := Pad("LABEL", 24) + Pad("ADAPTER", 18) + padLeft("TXNS", 8) + "   " + Pad("SYNCED", 12) + "STATUS"
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
		body = append(body, Pad(src.Label, 24)+Pad(src.AdapterKey, 18)+padLeft(count, 8)+"   "+Pad(synced, 12)+status)
	}
	return header, body
}

// ---------- transactions ----------

// Sized so a 100-column terminal shows every figure inside the card (95
// columns of it); only the source label, last, gets cut.
const (
	txWhenWidth = 17
	txTypeWidth = 21
	txLegWidth  = 20
	txGainWidth = 13
)

func txAssets(s State) txfmt.Assets {
	if s.Data.TxAssets == nil {
		return txfmt.Assets{}
	}
	return s.Data.TxAssets
}

func transactionsTab(s State) (string, []string) {
	p := paletteOf(s)
	header := Pad("DATE (UTC)", txWhenWidth) + Pad("TYPE", txTypeWidth) + padLeft("OUT", txLegWidth) + padLeft("IN", txLegWidth) + padLeft("GAIN/LOSS", txGainWidth) + "  SOURCE"
	assets := txAssets(s)
	var body []string
	for _, e := range s.Data.Transactions {
		body = append(body, Pad(txfmt.When(e.Ts), txWhenWidth)+
			Pad(output.Ellipsize(txfmt.Type(e), txTypeWidth-1), txTypeWidth)+
			padLeft(output.Ellipsize(txfmt.Legs(e, "out", assets), txLegWidth-1), txLegWidth)+
			padLeft(output.Ellipsize(txfmt.Legs(e, "in", assets), txLegWidth-1), txLegWidth)+
			padLeft(output.Signed(txfmt.GainLoss(e)), txGainWidth)+
			"  "+p.dim+txfmt.Source(e)+p.reset)
	}
	// A trailing line, not a row: the cursor never lands on it.
	if s.Data.TxLoadingMore && len(body) > 0 {
		body = append(body, p.dim+"loading more…"+p.reset)
	}
	return header, body
}

// detailCard is one transaction in full: every leg with its exact figures,
// which the list can only summarise.
func detailCard(s State, p palette, width, height int) []string {
	e := s.Data.Transactions[s.Cursor]
	assets := txAssets(s)
	opt := func(v *string) string {
		if v == nil || *v == "" {
			return p.dim + "—" + p.reset
		}
		return *v
	}
	field := func(label, value string) string { return p.dim + Pad(label, 15) + p.reset + value }

	lines := []string{
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

	// Sized to fit 100 columns, card and all, with every figure whole.
	lines = append(lines, "", p.dim+
		Pad("DIR", 5)+Pad("ROLE", 9)+padLeft("AMOUNT", 18)+"  "+Pad("ASSET", 9)+
		padLeft("VALUE", 13)+padLeft("PROCEEDS", 13)+padLeft("COST BASIS", 13)+padLeft("GAIN/LOSS", 13)+p.reset)
	for _, leg := range e.Legs {
		lines = append(lines,
			Pad(leg.Direction, 5)+Pad(output.Ellipsize(leg.Role, 8), 9)+padLeft(output.Qty(leg.Amount), 18)+"  "+
				Pad(output.Ellipsize(assets.Symbol(leg.AssetID), 8), 9)+
				padLeft(output.Money(leg.Value), 13)+padLeft(output.Money(leg.Proceeds), 13)+
				padLeft(output.Money(leg.CostBasis), 13)+padLeft(output.Signed(leg.GainLoss), 13))
	}
	return p.card(txfmt.Type(e), txfmt.When(e.Ts)+" UTC", width, padLines(lines, max(1, height-cardExtraRows)))
}

// ---------- filter form ----------

var fieldHint = map[int]string{
	fieldCategory: "any — type to search, e.g. transfer,send",
	fieldFrom:     "any — YYYY-MM-DD",
	fieldTo:       "any — YYYY-MM-DD",
	fieldSearch:   "any — free text",
}

func filterCard(s State, p palette, width, height int) []string {
	f := s.Filter
	var lines []string
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
		lines = append(lines, marker(field)+label+" "+value)

		// Under the category field, what the typed text could be.
		if field == fieldCategory && focused {
			token := categoryToken(f.Draft.Category)
			matches := CategoryMatches(f.Draft.Category)
			indent := strings.Repeat(" ", 14)
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

	lines = append(lines, "", marker(fieldClear)+"Clear all filters")
	if f.Error != "" {
		lines = append(lines, "", p.red+f.Error+p.reset)
	}
	return p.card("Filter transactions", "Esc cancel", width, padLines(lines, max(1, height-cardExtraRows)))
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

func helpScreen(s State, p palette, width, height int) []string {
	keys := []string{
		"↑ ↓ / k j      move",
		"PgUp PgDn      page",
		"Home End       jump to first / last",
		"⏎              open the selected entity",
		"↹ / ← → / h l  switch tab",
		"1..6           jump to a tab",
		"⏎              open the selected transaction (Transactions tab)",
		"[ ]            narrow / widen the chart's time range",
		"r              reload this entity",
		"t              switch theme (Terminal / Cypherpunk)",
		"f or /         filter transactions (Transactions tab)",
		"s              sync every enabled source, and watch it",
		"q / Esc        back to entities, or quit from there",
		"Ctrl-C         quit immediately",
		"",
		p.dim + "Scriptable commands still exist: grubless --help" + p.reset,
	}
	head := []string{p.title() + "  " + p.dim + "keys" + p.reset, ""}
	content := p.card("Keys", "", width, padLines(keys, max(1, min(len(keys), height-len(head)-1-cardExtraRows))))
	return page(head, content, p.dim+"press any key"+p.reset, height)
}

func padLeft(text string, width int) string {
	if gap := width - VisibleWidth(text); gap > 0 {
		return strings.Repeat(" ", gap) + text
	}
	return text
}
