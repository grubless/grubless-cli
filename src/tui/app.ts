import type {
  Entity,
  EntityActivity,
  EntityTaxSettings,
  Holding,
  PortfolioHistoryPoint,
  Source,
  TaxYearSummary,
  UncategorizedTransferWarning,
  ZeroCostWarning,
} from "@grubless/api-types";
import type { ApiClient } from "../client.js";
import { CliError, ExitCode } from "../output.js";
import { Terminal, type Key } from "./terminal.js";
import { initialState, reduce, type Action, type EntityData, type State } from "./state.js";
import { render, viewportRows } from "./views.js";

/**
 * The TUI event loop.
 *
 * Coexists with the scriptable commands rather than replacing them — a TUI
 * can't be piped, and the bulk report export and the CI warnings gate are
 * what justified the CLI in the first place. `grubless` with no arguments
 * opens this; `grubless report …` is unchanged.
 *
 * Everything decision-shaped lives in `state.ts` (pure reducer) and
 * `views.ts` (pure render). This file is the impure shell: fetch, paint,
 * dispatch keys.
 */

const SYNC_POLL_MS = 2000;
/** Fast enough to read as motion, slow enough not to strobe. */
const SPINNER_INTERVAL_MS = 120;

export async function runTui(client: ApiClient): Promise<number> {
  if (!process.stdin.isTTY || !process.stdout.isTTY) {
    // Refusing beats degrading: a TUI written into a pipe emits escape
    // sequences as data and silently corrupts whatever consumes it.
    throw new CliError(
      "The interactive interface needs a terminal.\nIn a script or CI, use the commands directly — run `grubless --help`.",
      ExitCode.UsageError,
    );
  }

  const term = new Terminal();
  let state = initialState();
  let syncTimer: NodeJS.Timeout | null = null;
  let spinTimer: NodeJS.Timeout | null = null;

  const paint = () => term.render(render(state, term.width, term.height));

  /**
   * Runs the spinner only while something is actually loading.
   *
   * A permanent repaint loop would be the simpler code and the wrong
   * behaviour: this is a long-lived terminal process, and waking every 120ms
   * to redraw an idle screen is a laptop battery cost for nothing. The
   * frame-diffing painter would swallow the redraw anyway, so it would burn
   * the wakeups without even showing.
   */
  const syncSpinner = () => {
    if (state.loading && spinTimer === null) {
      spinTimer = setInterval(() => {
        state = { ...state, tick: state.tick + 1 };
        paint();
      }, SPINNER_INTERVAL_MS);
      // Never hold the process open on its own account — the event loop
      // should end when the TUI does, not 120ms later.
      spinTimer.unref();
    } else if (!state.loading && spinTimer !== null) {
      clearInterval(spinTimer);
      spinTimer = null;
    }
  };

  const setState = (next: State) => {
    state = next;
    syncSpinner();
    paint();
  };

  const fail = (err: unknown) => {
    setState({
      ...state,
      loading: false,
      syncing: false,
      message: err instanceof Error ? err.message.split("\n")[0] : String(err),
      messageKind: "error",
    });
  };

  async function loadEntities(): Promise<void> {
    try {
      const entities = await client.get<Entity[]>("/entities");
      setState({ ...state, entities, loading: false });
    } catch (err) {
      fail(err);
    }
  }

  async function loadEntityData(entity: Entity): Promise<void> {
    try {
      // One round of parallel fetches rather than per-tab lazy loading. These
      // are all cheap reads, and an accountant switching between Holdings and
      // Warnings to cross-check a figure should not wait on a spinner each
      // time they press a number key.
      const [holdings, sources, tax, zeroCost, uncategorized, activity, history, settings] = await Promise.all([
        client.get<Holding[]>(`/entities/${entity.id}/holdings`),
        client.get<Source[]>(`/entities/${entity.id}/sources`),
        client.get<TaxYearSummary[]>(`/entities/${entity.id}/tax-summary`),
        client.get<ZeroCostWarning[]>(`/entities/${entity.id}/warnings/zero-cost`),
        client.get<UncategorizedTransferWarning[]>(`/entities/${entity.id}/warnings/uncategorized-transfers`),
        client.get<EntityActivity[]>(`/entities/${entity.id}/activity`),
        client.get<PortfolioHistoryPoint[]>(`/entities/${entity.id}/portfolio-history`),
        // The financial-year start month, for the "FY" range. 404s on an
        // entity with no settings row, which is not an error worth failing the
        // whole screen over — the chart falls back to the AU default and every
        // other range is unaffected.
        client.get<EntityTaxSettings>(`/entities/${entity.id}/tax-settings`).catch(() => null),
      ]);
      const data: EntityData = { holdings, sources, tax, zeroCost, uncategorized, activity, history, settings };
      setState({ ...state, data, loading: false });
    } catch (err) {
      fail(err);
    }
  }

  async function startSync(entity: Entity): Promise<void> {
    try {
      const sources = state.data.sources.filter((s) => s.syncEnabled);
      if (sources.length === 0) {
        setState({ ...state, syncing: false, message: "No syncable sources.", messageKind: "info" });
        return;
      }
      for (const source of sources) {
        // skipPriceBackfill across a multi-source run: each sync would
        // otherwise launch its own entity-wide price backfill against the
        // same rate-limited providers.
        await client.post(`/sources/${source.id}/sync`, { full: false, skipPriceBackfill: sources.length > 1 });
      }
      setState({ ...state, message: `Queued ${sources.length} source(s)…`, messageKind: "info" });
      watchSync(entity);
    } catch (err) {
      fail(err);
    }
  }

  /**
   * Polls activity while a sync runs, surfacing the worker's own progress
   * messages. This is the thing a TUI genuinely does better than the
   * scriptable path — a live redraw instead of a scrolling log.
   *
   * Never re-triggers the sync, only re-observes it: the server's
   * set-if-not-syncing claim makes a repeat POST a confusing no-op.
   */
  function watchSync(entity: Entity): void {
    if (syncTimer) clearTimeout(syncTimer);

    const tick = async () => {
      try {
        const activity = await client.get<EntityActivity[]>(`/entities/${entity.id}/activity`);
        const running = activity.filter((a) => a.status === "running");

        if (running.length > 0) {
          setState({
            ...state,
            data: { ...state.data, activity },
            message: running[0].message ?? "Syncing…",
            messageKind: "info",
          });
          syncTimer = setTimeout(tick, SYNC_POLL_MS);
          return;
        }

        const failed = activity.filter((a) => a.status === "error");
        setState({
          ...state,
          syncing: false,
          data: { ...state.data, activity },
          message: failed.length > 0 ? `${failed.length} source(s) failed` : "Sync finished",
          messageKind: failed.length > 0 ? "error" : "success",
        });
        // Figures will have moved — reload rather than leave stale numbers on
        // screen, which is the worst possible outcome for a tool people use
        // to check figures.
        await loadEntityData(entity);
      } catch (err) {
        fail(err);
      }
    };

    syncTimer = setTimeout(tick, SYNC_POLL_MS);
  }

  return new Promise<number>((resolve) => {
    const finish = (code: number) => {
      if (syncTimer) clearTimeout(syncTimer);
      if (spinTimer) clearInterval(spinTimer);
      term.close();
      resolve(code);
    };

    term.open();
    term.onResize(paint);

    term.onKey((key: Key) => {
      const { state: next, action } = reduce(state, key, viewportRows(term.height));
      setState(next);

      switch (action.type) {
        case "quit":
          finish(ExitCode.Ok);
          break;
        case "openEntity":
          if (next.selectedEntity) void loadEntityData(next.selectedEntity);
          break;
        case "reload":
          if (next.selectedEntity) void loadEntityData(next.selectedEntity);
          break;
        case "sync":
          if (next.selectedEntity) void startSync(next.selectedEntity);
          break;
        case "none":
          break;
      }
    });

    syncSpinner();
    paint();
    void loadEntities();
  });
}

export type { Action, State };
