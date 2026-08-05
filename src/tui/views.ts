import { money, qty } from "../output.js";
import { ansi, pad, truncate, visibleWidth } from "./terminal.js";
import { TABS, TAB_LABEL, rowCount, type State, type Tab } from "./state.js";

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
  const { header, body } = tabContent(state, width);

  lines.push(`${ansi.dim}${header}${ansi.reset}`);

  if (state.loading) {
    lines.push(`${ansi.dim}Loading…${ansi.reset}`);
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
  lines.push(statusBar(state, plural(rowCount(state), "row", "rows"), "↹ tab · r reload · s sync · ? help · q back"));
  return lines;
}

function tabBar(active: Tab): string {
  return TABS.map((tab, i) => {
    const label = ` ${i + 1} ${TAB_LABEL[tab]} `;
    return tab === active ? `${ansi.reverse}${label}${ansi.reset}` : `${ansi.dim}${label}${ansi.reset}`;
  }).join("");
}

function tabContent(state: State, width: number): { header: string; body: string[] } {
  switch (state.tab) {
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

function holdingsTab(state: State, width: number): { header: string; body: string[] } {
  const rows = state.data.holdings.filter((h) => !h.isSpam);
  const header = `  ${pad("ASSET", 10)}${pad("CHAIN", 10)}${padLeft("QUANTITY", 18)}${padLeft("VALUE", 16)}`;
  const body = rows.map((h) => {
    const flag = h.hasMismatch ? `  ${ansi.yellow}mismatch${ansi.reset}` : "";
    return `  ${pad(h.symbol, 10)}${pad(h.chain ?? "—", 10)}${padLeft(qty(h.quantity), 18)}${padLeft(money(h.value), 16)}${flag}`;
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
    "  1..4           jump to a tab",
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
