import { heldIn, money, qty, signed } from "../output.js";
import { renderChart } from "./chart.js";
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
    lines.push(`${ansi.dim}Loading…${ansi.reset}`);
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

  lines.push(`${ansi.dim}${header}${ansi.reset}`);

  if (state.loading) {
    lines.push(`${ansi.dim}Loading…${ansi.reset}`);
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
      return chartTab(state, width, rows);
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

/**
 * Portfolio value over time, plus where it stands today.
 *
 * The header carries the three figures the plot cannot: today's value, the
 * paper gain/loss on what's still held, and income received to date. The
 * line answers "which way, and how fast"; the header answers "how much".
 *
 * No colour in the header — `entityLines` wraps it in `dim`, and an embedded
 * `reset` would end the dim run for everything after it on that row.
 */
function chartTab(state: State, width: number, rows: number): { header: string; body: string[] } {
  const points = state.data.history;
  const last = points[points.length - 1];

  const header = last
    ? `  ${last.date}   VALUE ${money(last.value)}   UNREALISED ${signed(last.unrealizedPL)}   INCOME ${money(last.cumulativeIncome)}`
    : "  no history";

  const body = renderChart(
    // The float conversion is confined to plot geometry — a braille dot is
    // one of ~200 columns, so precision beyond a double is meaningless here.
    // Every figure a person reads off this screen comes from the header
    // above, which formats the server's exact decimal strings.
    points.map((p) => ({ date: p.date, value: Number(p.value) })),
    { width, height: rows },
  );

  return { header: truncate(header, width), body };
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
