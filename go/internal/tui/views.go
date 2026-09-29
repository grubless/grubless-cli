package tui

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/grubless/grubless-cli/go/internal/api"
	"github.com/grubless/grubless-cli/go/internal/chart"
	"github.com/grubless/grubless-cli/go/internal/jsstr"
	"github.com/grubless/grubless-cli/go/internal/output"
	"github.com/grubless/grubless-cli/go/internal/timerange"
)

// Rendering: pure functions from state to lines. Render always returns
// exactly `height` lines, which is what lets the painter diff frame by frame.
// See src/tui/views.ts.

// Now is the clock the chart's ranges are measured from. A variable so tests
// can pin it; the TS reads `new Date()` at the same points.
var Now = time.Now

const chromeRows = 4 // title + tabs + column header + status

func Render(s State, width, height int) []string {
	if s.ShowHelp {
		return frame(helpLines(), width, height)
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
	lines := []string{bold + "Grubless" + reset + "  " + dim + "select an entity" + reset, ""}

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
		line := "  " + Pad(e.Name, nameWidth) + "   " + dim + Pad(e.EntityType, 12) + e.Role + reset
		if i == s.EntityIndex {
			line = reverse + StripAnsi(line) + reset
		}
		lines = append(lines, line)
	}

	lines = append(lines, "")
	return append(lines, statusBar(s, plural(len(s.Entities), "entity", "entities"), "↑↓ move · ⏎ open · ? help · q quit"))
}

// spinner is a turning star: the only thing that says "fetching", not "hung".
var spinner = []string{"✶", "✸", "✹", "✺", "✹", "✷"}

func loadingLine(s State) string {
	return cyan + spinner[s.Tick%len(spinner)] + reset + " " + dim + "Loading…" + reset
}

func plural(count int, one, many string) string {
	if count == 1 {
		return strconv.Itoa(count) + " " + one
	}
	return strconv.Itoa(count) + " " + many
}

// ---------- entity detail ----------

func entityLines(s State, width, height int) []string {
	e := s.SelectedEntity
	lines := []string{
		bold + e.Name + reset + "  " + dim + e.EntityType + " · " + e.Role + reset,
		tabBar(s.Tab),
	}

	rows := ViewportRows(height)
	header, body := tabContent(s, width, rows)

	// A list's header dims as a whole; the dashboard's carries its own colours.
	if IsList(s.Tab) {
		lines = append(lines, dim+header+reset)
	} else {
		lines = append(lines, header)
	}

	switch {
	case s.Loading:
		lines = append(lines, loadingLine(s))
	case !IsList(s.Tab):
		lines = append(lines, body...)
	case len(body) == 0:
		lines = append(lines, dim+"Nothing here."+reset)
	default:
		for i := s.Offset; i < min(len(body), s.Offset+rows); i++ {
			if i == s.Cursor {
				lines = append(lines, reverse+StripAnsi(body[i])+reset)
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
	return plural(RowCount(s), "row", "rows")
}

func tabBar(active Tab) string {
	var b strings.Builder
	for i, tab := range Tabs {
		label := " " + strconv.Itoa(i+1) + " " + TabLabel[tab] + " "
		if tab == active {
			b.WriteString(reverse + label + reset)
		} else {
			b.WriteString(dim + label + reset)
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
	points := rangedHistory(s)

	header := "  " + dim + "no history" + reset
	if len(points) > 0 {
		last := points[len(points)-1]
		header = "  " + dim + last.Date + reset + "   " + cyan + "VALUE " + output.Money(last.Value) + reset +
			"   " + yellow + "UNREALISED " + output.Signed(last.UnrealizedPL) + reset +
			"   " + green + "INCOME " + output.Money(last.CumulativeIncome) + reset
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

	body = append(body, chart.Render(chart.FromHistory(points), chart.Options{Width: width, Height: chartRows, Colour: true})...)
	if showTile {
		body = append(body, holdingsTile(s, width, tileRows)...)
	}
	return Truncate(header, width), body
}

// rangeStrip shows the selected window, with its keys, as the web
// dashboard's segmented control.
func rangeStrip(s State) string {
	var b strings.Builder
	for _, r := range timerange.Keys {
		if r == s.Range {
			b.WriteString(reverse + " " + timerange.Label[r] + " " + reset)
		} else {
			b.WriteString(dim + " " + timerange.Label[r] + " " + reset)
		}
	}
	return "  " + b.String() + "  " + dim + "[ ]" + reset
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
	sorted := visibleHoldings(s)
	// Stable, largest first; NaN compares as equal, as a JS comparator
	// returning NaN does.
	sort.SliceStable(sorted, func(i, j int) bool { return valueOf(sorted[j])-valueOf(sorted[i]) < 0 })

	lines := []string{"  " + dim + Pad("HOLDINGS", holdingsCols) + Pad("HELD IN", heldInWidth) + padLeft("QUANTITY", 18) + padLeft("VALUE", 16) + reset}

	// One row kept back for "and N more" whenever there is a remainder.
	slots := rows - 1
	if len(sorted) > rows-1 {
		slots = rows - 2
	}
	shown := max(0, slots)
	for _, h := range sorted[:min(shown, len(sorted))] {
		lines = append(lines, holdingLine(h))
	}
	if remaining := len(sorted) - shown; remaining > 0 {
		lines = append(lines, "  "+dim+"and "+strconv.Itoa(remaining)+" more — press 2"+reset)
	}

	if len(lines) > rows {
		lines = lines[:max(0, rows)]
	}
	for i := range lines {
		lines[i] = Truncate(lines[i], width)
	}
	return lines
}

func holdingLine(h api.Holding) string {
	flag := ""
	if h.HasMismatch {
		flag = "  " + yellow + "mismatch" + reset
	}
	// Truncated to the column, not just padded, so a long source label
	// can't shove later columns out of line.
	where := output.HeldIn(h.Chain, h.SourceLabels(), heldInWidth-1)
	return "  " + Pad(h.Symbol, holdingsCols) + Pad(where, heldInWidth) + padLeft(output.Qty(h.Quantity), 18) + padLeft(output.Money(h.Value), 16) + flag
}

func holdingsTab(s State, width int) (string, []string) {
	header := "  " + Pad("ASSET", holdingsCols) + Pad("HELD IN", heldInWidth) + padLeft("QUANTITY", 18) + padLeft("VALUE", 16)
	var body []string
	for _, h := range visibleHoldings(s) {
		body = append(body, holdingLine(h))
	}
	return Truncate(header, width), body
}

func warningsTab(s State, width int) (string, []string) {
	header := "  " + Pad("KIND", 16) + Pad("DATE", 12) + Pad("ASSET", 10) + padLeft("AMOUNT", 18)
	var body []string
	// Blocking categories first, labelled: the ordering IS the message.
	for _, w := range s.Data.ZeroCost {
		body = append(body, "  "+yellow+Pad("no cost basis", 16)+reset+Pad(jsstr.Slice(w.Ts, 0, 10), 12)+Pad(w.AssetSymbol, 10)+padLeft(output.Qty(w.Quantity), 18)+"   "+dim+w.SourceLabel+reset)
	}
	for _, w := range s.Data.Uncategorized {
		body = append(body, "  "+yellow+Pad("uncategorised", 16)+reset+Pad(jsstr.Slice(w.Ts, 0, 10), 12)+Pad(w.AssetSymbol, 10)+padLeft(output.Qty(w.Amount), 18)+"   "+dim+w.Direction+reset)
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
	header := "  " + Pad("LABEL", 24) + Pad("ADAPTER", 18) + padLeft("TXNS", 8) + "   " + Pad("SYNCED", 12) + "STATUS"
	var body []string
	for _, src := range s.Data.Sources {
		status := src.SyncStatus
		switch {
		case !src.SyncEnabled:
			status = dim + "off" + reset
		case src.SyncStatus == "error":
			status = red + "error" + reset
		case src.SyncStatus == "syncing":
			status = cyan + "syncing" + reset
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

// ---------- chrome ----------

func statusBar(s State, left, hints string) string {
	if s.Message != nil && *s.Message != "" {
		colour := cyan
		switch s.MessageKind {
		case "error":
			colour = red
		case "success":
			colour = green
		}
		return colour + *s.Message + reset
	}
	if s.Syncing {
		return cyan + "syncing…" + reset + "  " + dim + hints + reset
	}
	return dim + left + "  ·  " + hints + reset
}

func helpLines() []string {
	return []string{
		bold + "Grubless — keys" + reset,
		"",
		"  ↑ ↓ / k j      move",
		"  PgUp PgDn      page",
		"  Home End       jump to first / last",
		"  ⏎              open the selected entity",
		"  ↹ / ← → / h l  switch tab",
		"  1..5           jump to a tab",
		"  [ ]            narrow / widen the chart's time range",
		"  r              reload this entity",
		"  s              sync every enabled source, and watch it",
		"  q / Esc        back to entities, or quit from there",
		"  Ctrl-C         quit immediately",
		"",
		"  " + dim + "Scriptable commands still exist: grubless --help" + reset,
		"",
		dim + "press any key" + reset,
	}
}

func padLeft(text string, width int) string {
	if gap := width - VisibleWidth(text); gap > 0 {
		return strings.Repeat(" ", gap) + text
	}
	return text
}
