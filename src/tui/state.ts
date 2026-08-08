import type {
  Entity,
  EntityActivity,
  EntityTaxSettings,
  Holding,
  PortfolioHistoryPoint,
  Source,
  TaxYearSummary,
  ZeroCostWarning,
  UncategorizedTransferWarning,
} from "@grubless/api-types";
import { RANGES, type RangeKey } from "../range.js";

/**
 * TUI state, and the pure reducers over it.
 *
 * Deliberately separated from both the terminal and the API client so the
 * whole interaction model is testable without a TTY and without a server:
 * feed a state and a keypress in, assert the state that comes out. Rendering
 * is likewise pure (`views.ts`) — a frame is just `string[]`.
 *
 * That split is what makes a hand-rolled TUI maintainable. The parts that
 * genuinely need a terminal (raw mode, escape sequences, repaint) stay in
 * terminal.ts and are thin enough to verify by eye.
 */

// Portfolio leads, as it does on the web dashboard: the first question is
// "how is this entity doing", and the answer is a shape, not a table. The
// tables are one keypress away either side.
export const TABS = ["chart", "holdings", "warnings", "tax", "sources"] as const;
export type Tab = (typeof TABS)[number];

export const TAB_LABEL: Record<Tab, string> = {
  chart: "Portfolio",
  holdings: "Holdings",
  warnings: "Warnings",
  tax: "Tax",
  sources: "Sources",
};

/**
 * Tabs whose body is a scrollable list of rows. The chart is not — it is one
 * picture sized to the window, so cursor movement and scrolling mean nothing
 * there and highlighting a "row" of it would just paint a bar across the
 * plot. See `rowCount` and `entityLines`.
 */
export function isList(tab: Tab): boolean {
  return tab !== "chart";
}

export interface EntityData {
  holdings: Holding[];
  sources: Source[];
  tax: TaxYearSummary[];
  zeroCost: ZeroCostWarning[];
  uncategorized: UncategorizedTransferWarning[];
  activity: EntityActivity[];
  history: PortfolioHistoryPoint[];
  /** Null until loaded, or when the entity has no settings row. */
  settings: EntityTaxSettings | null;
}

export const EMPTY_DATA: EntityData = {
  holdings: [],
  sources: [],
  tax: [],
  zeroCost: [],
  uncategorized: [],
  activity: [],
  history: [],
  settings: null,
};

export interface State {
  /** null until the entity list has loaded. */
  entities: Entity[];
  /** Index into `entities`; the entity picker is the first screen. */
  entityIndex: number;
  /** Null means "still on the picker". */
  selectedEntity: Entity | null;
  tab: Tab;
  /** Scroll offset within the active tab's list. */
  offset: number;
  /** Highlighted row within the active tab's list. */
  cursor: number;
  data: EntityData;
  loading: boolean;
  /** Transient message shown in the status bar (errors, confirmations). */
  message: string | null;
  messageKind: "info" | "error" | "success";
  /** True while a triggered sync is being watched. */
  syncing: boolean;
  showHelp: boolean;
  quit: boolean;
  /** Time range for the portfolio chart. See ../range.ts on the default. */
  range: RangeKey;
  /**
   * Spinner frame counter, advanced by the app's timer rather than by any
   * keypress — which is why the reducer neither reads nor writes it. A load
   * that takes twenty seconds has to look alive without input.
   */
  tick: number;
}

export function initialState(): State {
  return {
    entities: [],
    entityIndex: 0,
    selectedEntity: null,
    tab: "chart",
    offset: 0,
    cursor: 0,
    data: EMPTY_DATA,
    loading: true,
    message: null,
    messageKind: "info",
    syncing: false,
    showHelp: false,
    quit: false,
    // The web dashboard's default. Opening on all-time made the same entity
    // look different in the two surfaces — an early funding step reads as a
    // cliff that flattens everything after it.
    range: "fy",
    tick: 0,
  };
}

/** How many rows the active tab currently has — drives cursor clamping. */
export function rowCount(state: State): number {
  if (!state.selectedEntity) return state.entities.length;
  switch (state.tab) {
    // Not a list — nothing to scroll through, and a cursor over a plot is
    // meaningless. See isList().
    case "chart":
      return 0;
    case "holdings":
      return state.data.holdings.filter((h) => !h.isSpam).length;
    case "warnings":
      return state.data.zeroCost.length + state.data.uncategorized.length;
    case "tax":
      return state.data.tax.length;
    case "sources":
      return state.data.sources.length;
  }
}

/**
 * Actions the key handler can produce. Returned rather than performed, so the
 * reducer stays pure — the caller decides how to fulfil a `reload` or a
 * `sync`, and a test can just assert that one was requested.
 */
export type Action =
  | { type: "none" }
  | { type: "reload" }
  | { type: "sync" }
  | { type: "openEntity" }
  | { type: "quit" };

export interface KeyEvent {
  name: string;
  ctrl: boolean;
}

/**
 * The whole interaction model, as one pure function.
 *
 * `viewportRows` is passed in rather than read from the terminal so paging
 * behaviour is testable at any size — including the awkward ones (a two-row
 * window) that break naive scroll arithmetic.
 */
export function reduce(state: State, key: KeyEvent, viewportRows: number): { state: State; action: Action } {
  const next = { ...state, message: state.message };

  // Help is modal: any key dismisses it. A help overlay you have to guess
  // your way out of is worse than no help at all.
  if (state.showHelp) {
    return { state: { ...next, showHelp: false }, action: { type: "none" } };
  }

  if ((key.name === "c" && key.ctrl) || (key.name === "d" && key.ctrl)) {
    return { state: { ...next, quit: true }, action: { type: "quit" } };
  }

  switch (key.name) {
    case "q":
      // On a tab, `q` goes back to the entity picker rather than exiting
      // outright — an accountant moving between clients does that far more
      // often than they quit, and an app that drops to the shell on the most
      // reflexive key is tiring to use.
      if (state.selectedEntity) {
        return {
          state: { ...next, selectedEntity: null, cursor: 0, offset: 0, data: EMPTY_DATA },
          action: { type: "none" },
        };
      }
      return { state: { ...next, quit: true }, action: { type: "quit" } };

    case "escape":
      if (state.selectedEntity) {
        return {
          state: { ...next, selectedEntity: null, cursor: 0, offset: 0, data: EMPTY_DATA },
          action: { type: "none" },
        };
      }
      return { state: next, action: { type: "none" } };

    case "?":
      return { state: { ...next, showHelp: true }, action: { type: "none" } };

    case "up":
    case "k":
      return { state: moveCursor(next, -1, viewportRows), action: { type: "none" } };

    case "down":
    case "j":
      return { state: moveCursor(next, 1, viewportRows), action: { type: "none" } };

    case "pageup":
      return { state: moveCursor(next, -viewportRows, viewportRows), action: { type: "none" } };

    case "pagedown":
      return { state: moveCursor(next, viewportRows, viewportRows), action: { type: "none" } };

    // Home/End must move `entityIndex` as well as `cursor`. The picker reads
    // entityIndex and the tabs read cursor; updating only one made both keys
    // silently do nothing on whichever screen used the other.
    case "home":
      return { state: { ...next, cursor: 0, entityIndex: 0, offset: 0 }, action: { type: "none" } };

    case "end": {
      const last = Math.max(0, rowCount(state) - 1);
      return {
        state: clampScroll({ ...next, cursor: last, entityIndex: last }, viewportRows),
        action: { type: "none" },
      };
    }

    case "return":
      if (!state.selectedEntity && state.entities.length > 0) {
        return {
          state: { ...next, selectedEntity: state.entities[state.entityIndex], cursor: 0, offset: 0, loading: true },
          action: { type: "openEntity" },
        };
      }
      return { state: next, action: { type: "none" } };

    case "tab":
    case "right":
    case "l":
      if (!state.selectedEntity) return { state: next, action: { type: "none" } };
      return { state: switchTab(next, 1), action: { type: "none" } };

    case "left":
    case "h":
      if (!state.selectedEntity) return { state: next, action: { type: "none" } };
      return { state: switchTab(next, -1), action: { type: "none" } };

    // Range stepping, on the chart tab only. `[`/`]` rather than ←/→, which
    // already switch tabs, and rather than the number keys, which jump to one.
    // The strip above the plot shows where you are, so this needs no mode.
    case "[":
    case "]":
      if (!state.selectedEntity || state.tab !== "chart") return { state: next, action: { type: "none" } };
      return { state: { ...next, range: stepRange(state.range, key.name === "]" ? 1 : -1) }, action: { type: "none" } };

    case "r":
      if (!state.selectedEntity) return { state: next, action: { type: "none" } };
      return { state: { ...next, loading: true, message: null }, action: { type: "reload" } };

    case "s":
      if (!state.selectedEntity) return { state: next, action: { type: "none" } };
      if (state.syncing) {
        // Re-triggering is not just wasteful: POST /sources/:id/sync has a
        // set-if-not-syncing claim server-side, so a second press does
        // nothing while looking like it did something.
        return {
          state: { ...next, message: "Already syncing.", messageKind: "info" },
          action: { type: "none" },
        };
      }
      return { state: { ...next, syncing: true, message: null }, action: { type: "sync" } };

    default: {
      // Number keys jump straight to a tab.
      const index = Number.parseInt(key.name, 10);
      if (state.selectedEntity && index >= 1 && index <= TABS.length) {
        return { state: { ...next, tab: TABS[index - 1], cursor: 0, offset: 0 }, action: { type: "none" } };
      }
      return { state: next, action: { type: "none" } };
    }
  }
}

/** Clamped, not wrapped: stepping off "ALL" onto "24H" is disorienting. */
function stepRange(current: RangeKey, delta: number): RangeKey {
  const index = RANGES.indexOf(current);
  return RANGES[Math.min(RANGES.length - 1, Math.max(0, index + delta))];
}

function switchTab(state: State, delta: number): State {
  const current = TABS.indexOf(state.tab);
  const tab = TABS[(current + delta + TABS.length) % TABS.length];
  return { ...state, tab, cursor: 0, offset: 0 };
}

function moveCursor(state: State, delta: number, viewportRows: number): State {
  const total = state.selectedEntity ? rowCount(state) : state.entities.length;
  if (total === 0) return state;

  const clamped = Math.min(Math.max(0, (state.selectedEntity ? state.cursor : state.entityIndex) + delta), total - 1);

  if (!state.selectedEntity) {
    return clampScroll({ ...state, entityIndex: clamped, cursor: clamped }, viewportRows);
  }
  return clampScroll({ ...state, cursor: clamped }, viewportRows);
}

/** Keeps the cursor inside the visible window, scrolling only when it leaves. */
function clampScroll(state: State, viewportRows: number): State {
  const rows = Math.max(1, viewportRows);
  let offset = state.offset;
  if (state.cursor < offset) offset = state.cursor;
  if (state.cursor >= offset + rows) offset = state.cursor - rows + 1;
  return { ...state, offset: Math.max(0, offset) };
}
