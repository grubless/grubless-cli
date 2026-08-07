import { describe, it, expect } from "vitest";
import type { Entity, Holding, PortfolioHistoryPoint, Source, TaxYearSummary } from "@grubless/api-types";
import { decodeKeys, stripAnsi, truncate, visibleWidth } from "../tui/terminal.js";
import { EMPTY_DATA, initialState, reduce, rowCount, type State } from "../tui/state.js";
import { render, viewportRows } from "../tui/views.js";

/**
 * The TUI is split so that everything worth testing is pure: `state.ts` is a
 * reducer, `views.ts` is state → string[]. Only raw mode and painting need a
 * real terminal, and those are thin enough to check by eye.
 *
 * So these run with no TTY, no server and no timers.
 */

const KEY = (name: string, ctrl = false) => ({ name, ctrl });

function entity(id: string, name: string): Entity {
  return { id, name, entityType: "company", role: "owner", createdAt: "2026-01-01T00:00:00Z" };
}

function stateWithEntities(count: number): State {
  return {
    ...initialState(),
    loading: false,
    entities: Array.from({ length: count }, (_, i) => entity(`id-${i}`, `Entity ${i}`)),
  };
}

function openedState(overrides: Partial<State> = {}): State {
  const base = stateWithEntities(3);
  // Holdings, not the real landing tab (the chart) — most of what follows is
  // about list behaviour, and the chart is deliberately not a list. Chart
  // tests set `tab` explicitly.
  return { ...base, selectedEntity: base.entities[0], tab: "holdings", loading: false, ...overrides };
}

describe("key decoding", () => {
  it("decodes arrow keys from their escape sequences", () => {
    expect(decodeKeys("\u001b[A")).toEqual([KEY("up")]);
    expect(decodeKeys("\u001b[B")).toEqual([KEY("down")]);
    expect(decodeKeys("\u001b[C")).toEqual([KEY("right")]);
    expect(decodeKeys("\u001b[D")).toEqual([KEY("left")]);
  });

  it("decodes several keypresses arriving in one chunk", () => {
    // A fast typist or a paste delivers these together; handling only the
    // first would drop input.
    expect(decodeKeys("\u001b[Bj\u001b[B")).toEqual([KEY("down"), KEY("j"), KEY("down")]);
  });

  it("decodes control keys", () => {
    expect(decodeKeys("\u0003")).toEqual([KEY("c", true)]);
    expect(decodeKeys("\r")).toEqual([KEY("return")]);
    expect(decodeKeys("\t")).toEqual([KEY("tab")]);
  });

  it("consumes an unknown escape sequence rather than emitting its bytes", () => {
    // The failure this prevents: ESC[200~ (bracketed paste) being read as
    // "2", "0", "0", "~" and triggering four unrelated actions.
    expect(decodeKeys("\u001b[200~")).toEqual([]);
  });

  it("decodes page keys", () => {
    expect(decodeKeys("\u001b[5~")).toEqual([KEY("pageup")]);
    expect(decodeKeys("\u001b[6~")).toEqual([KEY("pagedown")]);
  });
});

describe("width handling", () => {
  it("measures visible width, ignoring escapes", () => {
    expect(visibleWidth("\u001b[1mabc\u001b[0m")).toBe(3);
  });

  it("truncates without cutting an escape sequence in half", () => {
    // A naive slice can leave a partial escape, which spills raw bytes onto
    // the screen and sticks the terminal's colour state.
    const out = truncate("\u001b[31mabcdefghij\u001b[0m", 5);
    expect(visibleWidth(out)).toBeLessThanOrEqual(5);
    expect(stripAnsi(out)).toBe("abcd…");
  });

  it("leaves short strings untouched", () => {
    expect(truncate("abc", 10)).toBe("abc");
  });

  it("returns nothing for a zero or negative width", () => {
    expect(truncate("abc", 0)).toBe("");
  });
});

describe("navigation", () => {
  it("moves the cursor and stops at the ends", () => {
    let s = stateWithEntities(3);
    s = reduce(s, KEY("down"), 10).state;
    expect(s.entityIndex).toBe(1);
    s = reduce(s, KEY("down"), 10).state;
    s = reduce(s, KEY("down"), 10).state;
    // Clamped, not wrapped: wrapping past the end of a client list is
    // disorienting when you're checking them off one at a time.
    expect(s.entityIndex).toBe(2);
    s = reduce(s, KEY("up"), 10).state;
    expect(s.entityIndex).toBe(1);
  });

  it("accepts vim keys as well as arrows", () => {
    let s = stateWithEntities(3);
    s = reduce(s, KEY("j"), 10).state;
    expect(s.entityIndex).toBe(1);
    s = reduce(s, KEY("k"), 10).state;
    expect(s.entityIndex).toBe(0);
  });

  it("scrolls only once the cursor leaves the window", () => {
    let s = stateWithEntities(20);
    for (let i = 0; i < 4; i++) s = reduce(s, KEY("down"), 5).state;
    expect(s.entityIndex).toBe(4);
    expect(s.offset).toBe(0); // still visible

    s = reduce(s, KEY("down"), 5).state;
    expect(s.entityIndex).toBe(5);
    expect(s.offset).toBe(1); // scrolled by exactly one
  });

  it("handles a viewport of one row without looping or dividing by zero", () => {
    let s = stateWithEntities(5);
    s = reduce(s, KEY("down"), 1).state;
    expect(s.entityIndex).toBe(1);
    expect(s.offset).toBe(1);
  });

  it("jumps to the last row with End", () => {
    const s = reduce(stateWithEntities(50), KEY("end"), 10).state;
    expect(s.entityIndex).toBe(49);
    expect(s.offset).toBe(40);
  });
});

describe("opening an entity", () => {
  it("requests a load and shows the picker's selection", () => {
    const s = stateWithEntities(3);
    const { state, action } = reduce({ ...s, entityIndex: 1 }, KEY("return"), 10);
    expect(action).toEqual({ type: "openEntity" });
    expect(state.selectedEntity?.name).toBe("Entity 1");
    expect(state.loading).toBe(true);
  });

  it("does nothing on an empty entity list", () => {
    const { action } = reduce(initialState(), KEY("return"), 10);
    expect(action).toEqual({ type: "none" });
  });
});

describe("tabs", () => {
  it("cycles with tab and wraps around", () => {
    let s = openedState();
    expect(s.tab).toBe("holdings");
    s = reduce(s, KEY("tab"), 10).state;
    expect(s.tab).toBe("warnings");
    s = reduce(s, KEY("tab"), 10).state;
    s = reduce(s, KEY("tab"), 10).state;
    expect(s.tab).toBe("sources");
    s = reduce(s, KEY("tab"), 10).state;
    expect(s.tab).toBe("chart");
    s = reduce(s, KEY("tab"), 10).state;
    expect(s.tab).toBe("holdings");
  });

  it("opens on the portfolio chart", () => {
    // The landing tab, matching the web dashboard: the first question about
    // an entity is which way it's going, and that's a shape, not a table.
    expect(initialState().tab).toBe("chart");
  });

  it("jumps directly with number keys", () => {
    const s = reduce(openedState(), KEY("4"), 10).state;
    expect(s.tab).toBe("tax");
  });

  it("resets the cursor when the tab changes", () => {
    // Carrying a row-8 cursor from a 40-row list into a 3-row one would put
    // the highlight off-screen.
    const s = reduce({ ...openedState(), cursor: 8, offset: 4 }, KEY("3"), 10).state;
    expect(s.tab).toBe("warnings");
    expect(s.cursor).toBe(0);
    expect(s.offset).toBe(0);
  });

  it("ignores tab keys on the entity picker", () => {
    const s = reduce(stateWithEntities(3), KEY("tab"), 10).state;
    expect(s.selectedEntity).toBeNull();
  });
});

describe("quitting and going back", () => {
  it("q returns to the picker from an entity, rather than exiting", () => {
    const { state, action } = reduce(openedState(), KEY("q"), 10);
    expect(state.selectedEntity).toBeNull();
    expect(state.quit).toBe(false);
    expect(action).toEqual({ type: "none" });
  });

  it("q exits from the picker", () => {
    const { state, action } = reduce(stateWithEntities(3), KEY("q"), 10);
    expect(state.quit).toBe(true);
    expect(action).toEqual({ type: "quit" });
  });

  it("clears entity data on the way back, so a stale entity never flashes", () => {
    const s = openedState({ data: { ...EMPTY_DATA, holdings: [{ symbol: "BTC" } as Holding] } });
    expect(reduce(s, KEY("q"), 10).state.data.holdings).toHaveLength(0);
  });

  it("Ctrl-C always exits, even from inside an entity", () => {
    const { state, action } = reduce(openedState(), KEY("c", true), 10);
    expect(state.quit).toBe(true);
    expect(action).toEqual({ type: "quit" });
  });
});

describe("sync", () => {
  it("requests a sync and marks the state busy", () => {
    const { state, action } = reduce(openedState(), KEY("s"), 10);
    expect(action).toEqual({ type: "sync" });
    expect(state.syncing).toBe(true);
  });

  it("refuses to re-trigger while a sync is already running", () => {
    // POST /sources/:id/sync has a set-if-not-syncing claim server-side, so a
    // second press is a no-op that looks like it did something.
    const { state, action } = reduce(openedState({ syncing: true }), KEY("s"), 10);
    expect(action).toEqual({ type: "none" });
    expect(state.message).toBe("Already syncing.");
  });

  it("does nothing on the entity picker", () => {
    expect(reduce(stateWithEntities(3), KEY("s"), 10).action).toEqual({ type: "none" });
  });
});

describe("help", () => {
  it("opens with ? and closes on any key", () => {
    let s = reduce(openedState(), KEY("?"), 10).state;
    expect(s.showHelp).toBe(true);
    // Any key, not a specific one — a help overlay you have to guess your way
    // out of is worse than none.
    s = reduce(s, KEY("x"), 10).state;
    expect(s.showHelp).toBe(false);
  });

  it("swallows the dismissing key rather than acting on it", () => {
    const s = reduce(openedState(), KEY("?"), 10).state;
    const { state, action } = reduce(s, KEY("q"), 10);
    expect(action).toEqual({ type: "none" });
    expect(state.selectedEntity).not.toBeNull();
  });
});

describe("rendering", () => {
  it("always fills exactly the terminal height", () => {
    for (const height of [6, 24, 50]) {
      expect(render(stateWithEntities(3), 80, height)).toHaveLength(height);
    }
  });

  it("never emits a line wider than the terminal", () => {
    const wide = openedState({
      data: {
        ...EMPTY_DATA,
        holdings: [
          {
            assetId: "a",
            symbol: "AVERYLONGTOKENSYMBOL",
            chain: "an-extremely-long-chain-name",
            imageUrl: null,
            quantity: "123456789.123456789",
            value: "987654321.99",
            hasMismatch: true,
            isSpam: false,
            sources: [],
          },
        ],
      },
    });
    // A line wider than the terminal wraps and destroys the layout, which is
    // the most common way a hand-rolled TUI looks broken.
    for (const line of render(wide, 40, 24)) {
      expect(visibleWidth(line)).toBeLessThanOrEqual(40);
    }
  });

  it("shows entity names on the picker", () => {
    const text = stripAnsi(render(stateWithEntities(2), 80, 24).join("\n"));
    expect(text).toContain("Entity 0");
    expect(text).toContain("Entity 1");
  });

  it("formats money rather than dumping raw numeric precision", () => {
    const s = openedState({
      tab: "tax",
      data: {
        ...EMPTY_DATA,
        tax: [
          {
            financialYear: "2025–26",
            startYear: 2025,
            taxPayable: "254142.028769325140330000",
            income: "1071559.886401792102100000",
            netCapitalGainLoss: "43459.49",
            taxableAmount: "1016568.12",
            applicable: true,
          } as TaxYearSummary,
        ],
      },
    });
    const text = stripAnsi(render(s, 120, 24).join("\n"));
    expect(text).toContain("254,142.03");
    expect(text).not.toContain("254142.028769325140330000");
  });

  it("says n/a for a pass-through entity instead of implying nothing is owed", () => {
    const s = openedState({
      tab: "tax",
      data: {
        ...EMPTY_DATA,
        tax: [
          {
            financialYear: "2025–26",
            income: "100",
            netCapitalGainLoss: "0",
            taxableAmount: "0",
            taxPayable: "0",
            applicable: false,
          } as TaxYearSummary,
        ],
      },
    });
    const text = stripAnsi(render(s, 120, 24).join("\n"));
    expect(text).toContain("n/a");
  });

  it("hides spam holdings but keeps the row count honest", () => {
    const s = openedState({
      data: {
        ...EMPTY_DATA,
        holdings: [
          { symbol: "REAL", chain: null, quantity: "1", value: "1", isSpam: false, hasMismatch: false } as Holding,
          { symbol: "SCAM", chain: null, quantity: "1", value: "1", isSpam: true, hasMismatch: false } as Holding,
        ],
      },
    });
    const text = stripAnsi(render(s, 80, 24).join("\n"));
    expect(text).toContain("REAL");
    expect(text).not.toContain("SCAM");
    // rowCount drives cursor clamping — if it counted the hidden row the
    // cursor could sit on something invisible.
    expect(rowCount(s)).toBe(1);
  });

  it("renders a source list with its sync status", () => {
    const s = openedState({
      tab: "sources",
      data: {
        ...EMPTY_DATA,
        sources: [
          {
            id: "s1",
            label: "Kraken",
            adapterKey: "kraken",
            transactionCount: 17000,
            lastSyncedAt: "2026-08-01T00:00:00Z",
            syncStatus: "error",
            syncEnabled: true,
          } as Source,
        ],
      },
    });
    const text = stripAnsi(render(s, 120, 24).join("\n"));
    expect(text).toContain("Kraken");
    expect(text).toContain("error");
  });
});

describe("viewportRows", () => {
  it("leaves room for the chrome", () => {
    expect(viewportRows(24)).toBe(20);
  });

  it("never returns less than one row, however small the terminal", () => {
    expect(viewportRows(1)).toBe(1);
    expect(viewportRows(0)).toBe(1);
  });
});

describe("holdings \"held in\" column", () => {
  const holding = (chain: string | null, sources: Array<{ label: string }>): Holding =>
    ({
      assetId: "a",
      symbol: "USDC",
      chain,
      imageUrl: null,
      quantity: "100",
      value: "100",
      hasMismatch: false,
      isSpam: false,
      sources,
    }) as Holding;

  it("shows the source for a chainless holding instead of a dash", () => {
    const s = openedState({
      data: { ...EMPTY_DATA, holdings: [holding(null, [{ label: "hyperliquid" }])] },
    });
    const text = stripAnsi(render(s, 120, 24).join("\n"));
    expect(text).toContain("HELD IN");
    expect(text).toContain("hyperliquid");
  });

  it("still shows the chain when there is one", () => {
    const s = openedState({
      data: { ...EMPTY_DATA, holdings: [holding("solana", [{ label: "sol-flex" }])] },
    });
    expect(stripAnsi(render(s, 120, 24).join("\n"))).toContain("solana");
  });

  it("keeps columns aligned when a source label is long", () => {
    const s = openedState({
      data: {
        ...EMPTY_DATA,
        holdings: [
          holding(null, [{ label: "Kraken-nodeintegration-a-very-long-label" }]),
          holding("solana", [{ label: "x" }]),
        ],
      },
    });
    const lines = render(s, 120, 24).map(stripAnsi);
    const rows = lines.filter((l) => l.includes("USDC"));
    expect(rows).toHaveLength(2);
    // The QUANTITY column must start at the same offset on both rows — a
    // long label shoving it right is exactly what the truncation prevents.
    expect(rows[0].indexOf("100")).toBe(rows[1].indexOf("100"));
  });
});

describe("portfolio chart tab", () => {
  const history = (points: Array<[string, string]>): PortfolioHistoryPoint[] =>
    points.map(([date, value]) => ({ date, value, cumulativeIncome: "0", unrealizedPL: "0" }));

  const chartState = (data: PortfolioHistoryPoint[]): State =>
    openedState({ tab: "chart", data: { ...EMPTY_DATA, history: data } });

  it("draws the series and dates it", () => {
    const s = chartState(history([["2026-01-01", "1000"], ["2026-06-01", "50000"], ["2026-08-01", "42000"]]));
    const text = stripAnsi(render(s, 100, 20).join("\n"));
    expect(text).toMatch(/[⠁-⣿]/);
    expect(text).toContain("2026-01-01");
  });

  it("puts today's exact figures in the header, formatted from the decimal strings", () => {
    const s = chartState([
      { date: "2026-08-07", value: "721022.174", cumulativeIncome: "4321.5", unrealizedPL: "12345.67" },
    ]);
    const text = stripAnsi(render(s, 120, 20).join("\n"));
    expect(text).toContain("VALUE 721,022.17");
    expect(text).toContain("INCOME 4,321.50");
  });

  it("signs an unrealised gain, so the figure reads as a direction", () => {
    const gain = chartState([{ date: "2026-08-07", value: "10", cumulativeIncome: "0", unrealizedPL: "12345.67" }]);
    expect(stripAnsi(render(gain, 120, 20).join("\n"))).toContain("UNREALISED +12,345.67");

    const loss = chartState([{ date: "2026-08-07", value: "10", cumulativeIncome: "0", unrealizedPL: "-987.65" }]);
    expect(stripAnsi(render(loss, 120, 20).join("\n"))).toContain("UNREALISED -987.65");

    // Zero stays bare: "+0.00" claims a gain that isn't there.
    const flat = chartState([{ date: "2026-08-07", value: "10", cumulativeIncome: "0", unrealizedPL: "0" }]);
    expect(stripAnsi(render(flat, 120, 20).join("\n"))).toContain("UNREALISED 0.00");
  });

  it("never highlights a row of the plot", () => {
    // The chart isn't a list. With rowCount 0 the cursor sits at 0, and the
    // list path would paint a reverse-video bar across the top of the plot.
    const s = chartState(history([["2026-01-01", "1"], ["2026-01-02", "2"]]));
    // Body only — the tab bar reverses the active tab, legitimately.
    const body = render(s, 100, 20).slice(3, -1);
    expect(body.join("\n")).not.toContain("[7m");
    expect(rowCount(s)).toBe(0);
  });

  it("counts days rather than rows in the status bar", () => {
    const s = chartState(history([["2026-01-01", "1"], ["2026-01-02", "2"]]));
    expect(stripAnsi(render(s, 100, 20).join("\n"))).toContain("2 days");
  });

  it("puts a holdings tile under the chart, taking about 30% of the tab", () => {
    // The web dashboard's two panels, in the same order: which way is this
    // going, then what is it made of.
    const s = openedState({
      tab: "chart",
      data: {
        ...EMPTY_DATA,
        history: history([["2026-01-01", "1000"], ["2026-08-01", "5000"]]),
        holdings: [
          { assetId: "1", symbol: "SOL", chain: "solana", quantity: "10", value: "900", isSpam: false, hasMismatch: false, sources: [] } as unknown as Holding,
          { assetId: "2", symbol: "BTC", chain: "bitcoin", quantity: "1", value: "100", isSpam: false, hasMismatch: false, sources: [] } as unknown as Holding,
        ],
      },
    });
    const lines = render(s, 100, 30).map(stripAnsi);
    const text = lines.join("\n");
    expect(text).toContain("HOLDINGS");
    expect(text).toContain("SOL");

    // The tile sits BELOW the plot, and the plot keeps the larger share.
    const tileAt = lines.findIndex((l) => l.includes("HOLDINGS"));
    const plotAt = lines.findIndex((l) => /[⠁-⣿]/.test(l));
    expect(plotAt).toBeGreaterThan(-1);
    expect(tileAt).toBeGreaterThan(plotAt);
    const body = 30 - 4;
    expect(tileAt - plotAt).toBeGreaterThan(body / 2);
  });

  it("orders the tile by value, largest first", () => {
    const s = openedState({
      tab: "chart",
      data: {
        ...EMPTY_DATA,
        history: history([["2026-01-01", "1000"], ["2026-08-01", "5000"]]),
        holdings: [
          { assetId: "1", symbol: "SMALL", chain: "solana", quantity: "1", value: "5", isSpam: false, hasMismatch: false, sources: [] } as unknown as Holding,
          { assetId: "2", symbol: "BIG", chain: "solana", quantity: "1", value: "5000", isSpam: false, hasMismatch: false, sources: [] } as unknown as Holding,
        ],
      },
    });
    const lines = render(s, 100, 30).map(stripAnsi);
    expect(lines.findIndex((l) => l.includes("BIG"))).toBeLessThan(lines.findIndex((l) => l.includes("SMALL")));
  });

  it("counts the holdings it could not fit rather than dropping them", () => {
    // A tile showing four of thirty positions reads as the whole portfolio
    // unless it says otherwise.
    const many = Array.from({ length: 30 }, (_, i) => ({
      assetId: String(i),
      symbol: `TOK${i}`,
      chain: "solana",
      quantity: "1",
      value: String(1000 - i),
      isSpam: false,
      hasMismatch: false,
      sources: [],
    })) as unknown as Holding[];
    const s = openedState({
      tab: "chart",
      data: { ...EMPTY_DATA, history: history([["2026-01-01", "1"], ["2026-08-01", "2"]]), holdings: many },
    });
    expect(stripAnsi(render(s, 100, 30).join("\n"))).toMatch(/and \d+ more/);
  });

  it("drops the tile on a short terminal rather than showing a header with nothing under it", () => {
    const s = openedState({
      tab: "chart",
      data: {
        ...EMPTY_DATA,
        history: history([["2026-01-01", "1000"], ["2026-08-01", "5000"]]),
        holdings: [
          { assetId: "1", symbol: "SOL", chain: "solana", quantity: "10", value: "900", isSpam: false, hasMismatch: false, sources: [] } as unknown as Holding,
        ],
      },
    });
    const lines = render(s, 100, 10);
    expect(lines).toHaveLength(10);
    expect(stripAnsi(lines.join("\n"))).not.toContain("HOLDINGS");
    // The status bar must survive whatever the split does.
    expect(stripAnsi(lines[9])).toContain("r reload");
  });

  it("still fills the frame when the entity has no history at all", () => {
    const s = chartState([]);
    const lines = render(s, 100, 20);
    expect(lines).toHaveLength(20);
    const text = stripAnsi(lines.join("\n"));
    expect(text).toContain("No portfolio history yet");
    // The status bar is the last line and must survive — the chart sizing
    // itself wrong is exactly how it would get pushed off-screen.
    expect(stripAnsi(lines[19])).toContain("r reload");
  });
});
