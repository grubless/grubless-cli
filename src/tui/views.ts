import { heldIn, money, qty, signed } from "../output.js";
import { renderPortfolio, toChartPoints } from "./chart.js";
import { ansi, pad, truncate, visibleWidth } from "./terminal.js";
import { TABS, TAB_LABEL, isList, rowCount, type State, type Tab } from "./state.js";

/**
 * Rendering — pure functions from state to `string[]`.
 *
 * Nothing here touches the terminal, so every screen can be asserted in a
 * test without a TTY. `render()` always returns exactly `height` lines, which
 * is what lets the painter diff frames line by line.
 */

const CHROME_ROWS = 4; // title + tabs + column header + status

export function render(state: State, width: number, height: number): string[] {
  if (state.showHelp) return frame(helpLines(), width, height);

  const body = state.selectedEntity ? entityLines(state, width, height) : entityPickerLines(state, height);
  return frame(body, width, height);
}

/** Pads/truncates to an exact `height` × `width` frame. */
function frame(lines: string[], width: number, height: number): string[] {
  const out = lines.slice(0, height).map((line) => truncate(line, width));
  while (out.length < height) out.push("");
  return out;
}

/** Rows available to the list body, after title/tabs/header/status. */
export function viewportRows(height: number): number {
  return Math.max(1, height - CHROME_ROWS);
}

// ---------- entity picker ----------

function entityPickerLines(state: State, height: number): string[] {
  const lines: string[] = [];
  lines.push(`${ansi.bold}Grubless${ansi.reset}  ${ansi.dim}select an entity${ansi.reset}`);
  lines.push("");

  if (state.loading && state.entities.length === 0) {
    lines.push(loadingLine(state));
    return lines;
  }
  if (state.entities.length === 0) {
    lines.push("No entities available to this account.");
    return lines;
  }

  const rows = viewportRows(height);
  const visible = state.entities.slice(state.offset, state.offset + rows);
  const nameWidth = Math.max(...state.entities.map((e) => e.name.length), 4);

  for (const [i, entity] of visible.entries()) {
    const index = state.offset + i;
    const selected = index === state.entityIndex;
    const line = `  ${pad(entity.name, nameWidth)}   ${ansi.dim}${pad(entity.entityType, 12)}${entity.role}${ansi.reset}`;
    lines.push(selected ? `${ansi.reverse}${stripForHighlight(line)}${ansi.reset}` : line);
  }

  lines.push("");
  lines.push(statusBar(state, plural(state.entities.length, "entity", "entities"), "↑↓ move · ⏎ open · ? help · q quit"));
  return lines;
}

/**
 * A turning star. Cheap, but it is the only thing on screen that says the
 * difference between "fetching" and "hung" — an entity with years of history
 * takes a few seconds to load, and a static "Loading…" for that long reads as
 * a stall.
 */
const SPINNER = ["✶", "✸", "✹", "✺", "✹", "✷"];

function loadingLine(state: State): string {
  return `${ansi.cyan}${SPINNER[state.tick % SPINNER.length]}${ansi.reset} ${ansi.dim}Loading…${ansi.reset}`;
}

function plural(count: number, one: string, many: string): string {
  return `${count} ${count === 1 ? one : many}`;
}

/**
 * Reverse-video highlighting and embedded colour codes fight: a `dim` inside
 * a reversed run reads as unreadable grey-on-grey on most terminals. Strip
 * the inner styling and let the reverse do the work.
 */
function stripForHighlight(line: string): string {
  return line.replace(/\u001b\[[0-9;?]*[a-zA-Z]/g, "");
}

// ---------- entity detail ----------

function entityLines(state: State, width: number, height: number): string[] {
  const entity = state.selectedEntity!;
  const lines: string[] = [];

  lines.push(
    `${ansi.bold}${entity.name}${ansi.reset}  ${ansi.dim}${entity.entityType} · ${entity.role}${ansi.reset}`,
  );
  lines.push(tabBar(state.tab));

  const rows = viewportRows(height);
  const { header, body } = tabContent(state, width, rows);

  // A list's header is a column rule and dims as a whole. The dashboard's
  // carries its own colour — each figure tinted to match its line on the
  // chart, which is what saves a row that would otherwise go to a legend — so
  // wrapping it here would end the dim run at its first embedded reset.
  lines.push(isList(state.tab) ? `${ansi.dim}${header}${ansi.reset}` : header);

  if (state.loading) {
    lines.push(loadingLine(state));
  } else if (!isList(state.tab)) {
    // Already sized to the viewport, and not a list: no scroll offset to
    // apply and no row to highlight. See isList().
    lines.push(...body);
  } else if (body.length === 0) {
    lines.push(`${ansi.dim}Nothing here.${ansi.reset}`);
  } else {
    const visible = body.slice(state.offset, state.offset + rows);
    for (const [i, line] of visible.entries()) {
      const index = state.offset + i;
      lines.push(index === state.cursor ? `${ansi.reverse}${stripForHighlight(line)}${ansi.reset}` : line);
    }
  }

  while (lines.length < height - 1) lines.push("");
  lines.push(statusBar(state, countLabel(state), "↹ tab · r reload · s sync · ? help · q back"));
  return lines;
}

/** What the status bar counts differs by tab — the chart measures days, not rows. */
function countLabel(state: State): string {
  if (state.tab === "chart") return plural(state.data.history.length, "day", "days");
  return plural(rowCount(state), "row", "rows");
}

function tabBar(active: Tab): string {
  return TABS.map((tab, i) => {
    const label = ` ${i + 1} ${TAB_LABEL[tab]} `;
    return tab === active ? `${ansi.reverse}${label}${ansi.reset}` : `${ansi.dim}${label}${ansi.reset}`;
  }).join("");
}

function tabContent(state: State, width: number, rows: number): { header: string; body: string[] } {
  switch (state.tab) {
    case "chart":
      return dashboardTab(state, width, rows);
    case "holdings":
      return holdingsTab(state, width);
    case "warnings":
      return warningsTab(state, width);
    case "tax":
      return taxTab(state);
    case "sources":
      return sourcesTab(state, width);
  }
}

/** The chart's share of the tab; holdings take what's left. */
const CHART_SHARE = 0.7;
/** Header + one holding + "and N more" — anything less isn't a tile. */
const MIN_TILE_ROWS = 3;

/**
 * The dashboard: portfolio over time, with current holdings beneath it.
 *
 * Same two panels as the web dashboard, in the same order, because they
 * answer consecutive questions — "which way is this going" and then "what is
 * it actually made of". Neither is much use without the other, and a terminal
 * has room for both.
 *
 * The chart takes 70% of the tab and the tile the rest. The split is on the
 * viewport rather than fixed rows so both panels grow with the window; on a
 * short one the tile is dropped entirely rather than shown as a header with
 * nothing under it.
 *
 * The header carries the three figures the plot cannot state exactly, each
 * tinted to match its own line below — that is the legend, folded into a row
 * that had to exist anyway. UNREALISED takes its strip's yellow rather than
 * the app's green/red gain-loss convention: with three lines on screen, a
 * colour that tracks polarity instead of series would collide with the green
 * income line at exactly the moment the gain turns positive. The sign is
 * explicit in the figure itself.
 */
function dashboardTab(state: State, width: number, rows: number): { header: string; body: string[] } {
  const points = state.data.history;
  const last = points[points.length - 1];

  const header = last
    ? `  ${ansi.dim}${last.date}${ansi.reset}   ${ansi.cyan}VALUE ${money(last.value)}${ansi.reset}` +
      `   ${ansi.yellow}UNREALISED ${signed(last.unrealizedPL)}${ansi.reset}` +
      `   ${ansi.green}INCOME ${money(last.cumulativeIncome)}${ansi.reset}`
    : `  ${ansi.dim}no history${ansi.reset}`;

  const tileRows = rows - Math.round(rows * CHART_SHARE);
  const showTile = tileRows >= MIN_TILE_ROWS && state.data.holdings.length > 0;
  const chartRows = showTile ? rows - tileRows : rows;

  const body = renderPortfolio(toChartPoints(points), { width, height: chartRows });
  if (showTile) body.push(...holdingsTile(state, width, tileRows));

  return { header: truncate(header, width), body };
}

/**
 * The largest holdings, as a compact tile under the chart.
 *
 * Sorted by value and truncated to the space, with the remainder counted
 * rather than dropped silently — a tile that shows four of an entity's
 * thirty positions must say so, or it reads as the whole portfolio. The
 * Holdings tab has the full list.
 */
function holdingsTile(state: State, width: number, rows: number): string[] {
  const visible = state.data.holdings.filter((h) => !h.isSpam);
  // Number() for ORDERING only — never for a figure that reaches the screen,
  // which stays formatted from the exact string by money()/qty().
  const sorted = [...visible].sort((a, b) => Number(b.value) - Number(a.value));

  const lines = [`  ${ansi.dim}${pad("HOLDINGS", 10)}${pad("HELD IN", HELD_IN_WIDTH)}${padLeft("QUANTITY", 18)}${padLeft("VALUE", 16)}${ansi.reset}`];

  // One row is kept back for the "and N more" line whenever there IS a
  // remainder; without that the last holding shown would silently be the last
  // one there is.
  const slots = sorted.length > rows - 1 ? rows - 2 : rows - 1;
  for (const h of sorted.slice(0, Math.max(0, slots))) {
    const flag = h.hasMismatch ? `  ${ansi.yellow}mismatch${ansi.reset}` : "";
    lines.push(
      `  ${pad(h.symbol, 10)}${pad(heldIn(h, HELD_IN_WIDTH - 1), HELD_IN_WIDTH)}${padLeft(qty(h.quantity), 18)}${padLeft(money(h.value), 16)}${flag}`,
    );
  }

  const remaining = sorted.length - Math.max(0, slots);
  if (remaining > 0) lines.push(`  ${ansi.dim}and ${remaining} more — press 2${ansi.reset}`);

  return lines.slice(0, rows).map((line) => truncate(line, width));
}

const HELD_IN_WIDTH = 22;

function holdingsTab(state: State, width: number): { header: string; body: string[] } {
  const rows = state.data.holdings.filter((h) => !h.isSpam);
  // "Held in", not "Chain": a Hyperliquid or Kraken balance is exchange-native
  // and has no chain at all, so "—" stated nothing where the useful fact —
  // which source holds it — was already in the payload. Matches the web app's
  // holdings card. See heldIn().
  const header = `  ${pad("ASSET", 10)}${pad("HELD IN", HELD_IN_WIDTH)}${padLeft("QUANTITY", 18)}${padLeft("VALUE", 16)}`;
  const body = rows.map((h) => {
    const flag = h.hasMismatch ? `  ${ansi.yellow}mismatch${ansi.reset}` : "";
    // Truncated to the column, not just padded — a source label like
    // "Kraken-nodeintegration" would otherwise shove every later column out
    // of alignment on that one row.
    const where = heldIn(h, HELD_IN_WIDTH - 1);
    return `  ${pad(h.symbol, 10)}${pad(where, HELD_IN_WIDTH)}${padLeft(qty(h.quantity), 18)}${padLeft(money(h.value), 16)}${flag}`;
  });
  return { header: truncate(header, width), body };
}

function warningsTab(state: State, width: number): { header: string; body: string[] } {
  const header = `  ${pad("KIND", 16)}${pad("DATE", 12)}${pad("ASSET", 10)}${padLeft("AMOUNT", 18)}`;
  const body: string[] = [];

  // Blocking categories first, and labelled as such — the ordering IS the
  // message. These are the ones that make the filed figures wrong.
  for (const w of state.data.zeroCost) {
    body.push(
      `  ${ansi.yellow}${pad("no cost basis", 16)}${ansi.reset}${pad(w.ts.slice(0, 10), 12)}${pad(w.assetSymbol, 10)}${padLeft(qty(w.quantity), 18)}   ${ansi.dim}${w.sourceLabel}${ansi.reset}`,
    );
  }
  for (const w of state.data.uncategorized) {
    body.push(
      `  ${ansi.yellow}${pad("uncategorised", 16)}${ansi.reset}${pad(w.ts.slice(0, 10), 12)}${pad(w.assetSymbol, 10)}${padLeft(qty(w.amount), 18)}   ${ansi.dim}${w.direction}${ansi.reset}`,
    );
  }
  return { header: truncate(header, width), body };
}

function taxTab(state: State): { header: string; body: string[] } {
  const header = `  ${pad("FY", 10)}${padLeft("INCOME", 16)}${padLeft("NET CGT", 16)}${padLeft("TAXABLE", 16)}${padLeft("TAX", 14)}`;
  const body = state.data.tax.map((s) => {
    // A pass-through entity has no entity-level tax. "0.00" would read as
    // "nothing owed" rather than "not computed here" — a materially different
    // statement in front of someone preparing a return.
    const tax = s.applicable ? money(s.taxPayable) : "n/a";
    return `  ${pad(s.financialYear, 10)}${padLeft(money(s.income), 16)}${padLeft(money(s.netCapitalGainLoss), 16)}${padLeft(money(s.taxableAmount), 16)}${padLeft(tax, 14)}`;
  });
  return { header, body };
}

function sourcesTab(state: State, width: number): { header: string; body: string[] } {
  const header = `  ${pad("LABEL", 24)}${pad("ADAPTER", 18)}${padLeft("TXNS", 8)}   ${pad("SYNCED", 12)}STATUS`;
  const body = state.data.sources.map((s) => {
    const status = !s.syncEnabled
      ? `${ansi.dim}off${ansi.reset}`
      : s.syncStatus === "error"
        ? `${ansi.red}error${ansi.reset}`
        : s.syncStatus === "syncing"
          ? `${ansi.cyan}syncing${ansi.reset}`
          : s.syncStatus;
    const synced = s.lastSyncedAt ? s.lastSyncedAt.slice(0, 10) : "never";
    return `  ${pad(s.label, 24)}${pad(s.adapterKey, 18)}${padLeft(String(s.transactionCount), 8)}   ${pad(synced, 12)}${status}`;
  });
  return { header: truncate(header, width), body };
}

// ---------- chrome ----------

function statusBar(state: State, left: string, hints: string): string {
  if (state.message) {
    const colour =
      state.messageKind === "error" ? ansi.red : state.messageKind === "success" ? ansi.green : ansi.cyan;
    return `${colour}${state.message}${ansi.reset}`;
  }
  if (state.syncing) return `${ansi.cyan}syncing…${ansi.reset}  ${ansi.dim}${hints}${ansi.reset}`;
  return `${ansi.dim}${left}  ·  ${hints}${ansi.reset}`;
}

function helpLines(): string[] {
  return [
    `${ansi.bold}Grubless — keys${ansi.reset}`,
    "",
    "  ↑ ↓ / k j      move",
    "  PgUp PgDn      page",
    "  Home End       jump to first / last",
    "  ⏎              open the selected entity",
    "  ↹ / ← → / h l  switch tab",
    "  1..5           jump to a tab",
    "  r              reload this entity",
    "  s              sync every enabled source, and watch it",
    "  q / Esc        back to entities, or quit from there",
    "  Ctrl-C         quit immediately",
    "",
    `  ${ansi.dim}Scriptable commands still exist: grubless --help${ansi.reset}`,
    "",
    `${ansi.dim}press any key${ansi.reset}`,
  ];
}

function padLeft(text: string, width: number): string {
  const gap = width - visibleWidth(text);
  return gap > 0 ? " ".repeat(gap) + text : text;
}
