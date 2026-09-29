/**
 * Generates the golden files the Go tests check against, by running the
 * *actual* TS implementation over a large, seeded set of inputs.
 *
 *     pnpm exec tsx go/scripts/goldens.ts
 *
 * The Go port's contract is byte-identical output. Hand-written expectations
 * can only cover the cases someone thought of; these cover the cases a PRNG
 * thought of too — thousands of decimal strings, chart series, TUI screens
 * and key sequences — and every expected value comes from the TS itself, so
 * there is no second opinion to drift. Re-run after changing either build;
 * a Go test failing afterwards is a real behavioural difference.
 *
 * Inputs are seeded, so the files only change when behaviour does.
 */

import { mkdirSync, rmSync, writeFileSync } from "node:fs";
import { gzipSync } from "node:zlib";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

// Pin the clock before anything reads it. The views and range helpers call
// `new Date()`; the Go side pins `tui.Now` to the same instant.
const FIXED_NOW = Date.parse("2026-08-09T04:00:00.000Z");
const RealDate = Date;
class FixedDate extends RealDate {
  constructor(...args: unknown[]) {
    if (args.length === 0) super(FIXED_NOW);
    else super(...(args as [string]));
  }
  static now() {
    return FIXED_NOW;
  }
}
globalThis.Date = FixedDate as DateConstructor;

const { money, qty, signed, heldIn, ellipsize, shortDate } = await import("../../src/output.js");
const { parseCsv, csvToTable } = await import("../../src/csv.js");
const { rangeStartDate, pointsInRange, currentFinancialYearStart, RANGES } = await import("../../src/range.js");
const { niceCeil, compactMoney, resample, renderPortfolio, toChartPoints } = await import("../../src/tui/chart.js");
const { decodeKeys, truncate, visibleWidth } = await import("../../src/tui/terminal.js");
const { initialState, reduce, TABS } = await import("../../src/tui/state.js");
const { render } = await import("../../src/tui/views.js");

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
/** Gzipped: the files are large and compress ~15x, and Go reads gzip natively. */
function save(path: string, value: unknown): void {
  const file = join(root, path + ".gz");
  mkdirSync(dirname(file), { recursive: true });
  rmSync(join(root, path), { force: true });
  writeFileSync(file, gzipSync(JSON.stringify(value), { level: 9 }));
  console.log(`wrote ${path}.gz`);
}

// ---------- seeded randomness ----------

function mulberry32(seed: number) {
  return () => {
    seed |= 0;
    seed = (seed + 0x6d2b79f5) | 0;
    let t = Math.imul(seed ^ (seed >>> 15), 1 | seed);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}
const rand = mulberry32(20260929);
const int = (lo: number, hi: number) => lo + Math.floor(rand() * (hi - lo + 1));
const pick = <T>(xs: readonly T[]): T => xs[int(0, xs.length - 1)];
const digits = (n: number) => Array.from({ length: n }, () => String(int(0, 9))).join("");

function randomDecimal(): string {
  const shape = int(0, 9);
  const sign = rand() < 0.25 ? "-" : "";
  if (shape === 0) return `${sign}${int(1, 9)}${rand() < 0.5 ? "." + digits(int(1, 4)) : ""}e${pick(["", "+", "-"])}${int(0, 12)}`;
  if (shape === 1) return `${sign}0.${"0".repeat(int(0, 12))}${digits(int(1, 6))}`;
  if (shape === 2) return `${sign}${"9".repeat(int(1, 8))}.${"9".repeat(int(1, 5))}${digits(int(0, 3))}`;
  if (shape === 3) return `${sign}${digits(int(1, 3))}.${pick(["005", "004", "995", "994", "5", "45", "0"])}`;
  const whole = rand() < 0.2 ? "0" : String(int(1, 9)) + digits(int(0, 11));
  const frac = rand() < 0.3 ? "" : "." + digits(int(0, 24));
  return sign + whole + frac;
}

const LABELS = [
  "Kraken", "hyperliquid", "sol-flex", "evm-wallet-a (Ethereum)", "Kraken-trading-account-a-very-long-label",
  "Société Générale", "東京ウォレット", "wallet 😀 emoji", "x", "",
];

// ---------- output formatting ----------

const formatInputs = [
  "254142.028769325140330000", "1071559.886401792102100000", "0.000000000000000000", "1.005", "1.004", "0.999",
  "9.999", "999.999", "1999999.995", "8.454236583100000300", "98636.422322129450373000",
  "-41434.915835700685932000", "-9.999", "42", "985.967271550000000000", "311.108976023000000000",
  "0.318136000000000000", "0.313763290115432553", "300.000000000000000000", "56282.264678000000000000",
  "-2.209850000000000000", "2e-9", "1.5e-3", "1.2345e2", "5e3", "0.000000002", "-0.000000002", "0.00000001", "0",
  "1.5e3", "-2.5e-1", "", "-0", ".5", "5.", "-0.005", "0.995", "12345678901234567890.125",
  ...Array.from({ length: 1500 }, randomDecimal),
];
save("internal/output/testdata/format.json", {
  money: formatInputs.map((v) => [v, money(v)]),
  qty: formatInputs.map((v) => [v, qty(v)]),
  signed: formatInputs.map((v) => [v, signed(v)]),
  ellipsize: [...LABELS, "0xccef6bdd7534f750eb1f494367493b5fd65c905d", "😀😀😀😀"].flatMap((text) =>
    [0, 1, 2, 3, 5, 12, 20, 40].map((w) => [text, w, ellipsize(text, w)]),
  ),
  heldIn: Array.from({ length: 300 }, () => {
    const chain = rand() < 0.3 ? pick(["solana", "ethereum", "an-extremely-long-chain-name-indeed"]) : null;
    const sources = Array.from({ length: int(0, 5) }, () => ({ label: pick(LABELS) }));
    const width = pick([5, 12, 21, 28]);
    return [chain, sources.map((s) => s.label), width, heldIn({ chain, sources }, width)];
  }),
  shortDate: [
    "2025-07-01T00:00:00.000Z", "2025-02-30", "2025-02-29T00:00:00Z", "2025-13-01", "2025-07-01", "2025-07", "2025",
    "2025-07-01T10:00:00.123456Z", "2025-07-01T23:30:00+10:00", "2025-07-01 10:00:00Z", "2025-07-01T24:00:00Z",
    "2025-07-01T10:00:60Z", "2025-07-01T10:00:00.5Z", "2025-07-01T10:00Z", "2025-07-01t10:00:00z",
    "2025-06-30T20:00:00-05:00", "garbage", "",
  ].map((v) => [v, shortDate(v)]),
});

// ---------- JS number semantics ----------

const numbers = [
  0, -0, 1, -1, 0.5, -0.5, 1.5, 2.5, -2.5, 0.125, 0.375, 1.005, 1e21, 1e-7, 1e-6, 123456789012345680000, 0.1, 0.3,
  721022.17, 1234567, 2500000, 0.25, 45, 999.95, 999.949, 1e300, 5e-324, 1.7976931348623157e308,
  ...Array.from({ length: 400 }, () => (rand() - 0.5) * 10 ** int(-8, 12)),
  ...Array.from({ length: 100 }, () => int(-2000, 2000) / 8),
];
save("internal/jsstr/testdata/numbers.json", {
  string: numbers.map((n) => [n, String(n)]),
  toFixed: numbers.filter((n) => Math.abs(n) < 1e21).flatMap((n) => [0, 1, 2].map((d) => [n, d, n.toFixed(d)])),
  round: numbers.map((n) => [n, Math.round(n)]),
  parse: [
    "", " ", "0", "-0", "1.5", " 42 ", "0x10", "0b101", "0o17", "1e3", "1E-3", ".5", "5.", "+7", "Infinity",
    "-Infinity", "abc", "1,000", "1e", "NaN", "0.1", "1e400", "\t12\n", "1_000",
  // Non-finite results as strings: JSON.stringify would write all three as null.
  ].map((s) => [s, Number.isFinite(Number(s)) ? Number(s) : String(Number(s))]),
});

// ---------- CSV ----------

const csvInputs = [
  "a,b\r\n1,2\r\n", "a,b\n1,2\n", 'name,amount\r\n"Smith, Robert",1000\r\n', 'note\r\n"he said ""no"""\r\n',
  'note,amount\r\n"line one\nline two",5\r\n', "a,b,c\r\n1,,\r\n", "a\r\n1\r\n", "",
  "Asset,Proceeds\r\nBTC,1234.56\r\nETH,7.89\r\n", "Field,Value\r\nNo cached tax summary found for this financial year.\r\n",
  "a,b\r\n1,2,3\r\n", "a,b,c\r\n1,2\r\n", "Asset,Proceeds,Cost Basis\r\n", "Year,2025,Name\r\nx,y,z\r\n",
  'a,b\r\nx"y,"q"r\r\n', "a,a\r\n1,2\r\n", "a,b\r\n   \r\n1,2\r\n", "a,b\r\n\r\n1,2", 'a,b\r\n"unterminated,1\r\n',
  ...Array.from({ length: 200 }, () =>
    Array.from({ length: int(1, 6) }, () =>
      Array.from({ length: int(1, 4) }, () => pick(["x", "", '"q,uoted"', '"a""b"', "1.5", '"multi\nline"', "Société", " sp "])).join(","),
    ).join(pick(["\r\n", "\n"])) + pick(["", "\r\n", "\n"]),
  ),
];
save("internal/csv/testdata/csv.json", csvInputs.map((text) => [text, parseCsv(text), JSON.stringify(csvToTable(text), null, 2)]));

// ---------- ranges ----------

const nows = ["2026-08-09T04:00:00.000Z", "2026-03-15T00:00:00.000Z", "2026-01-01T00:00:00.000Z", "2025-12-31T23:59:59.999Z", "2024-02-29T12:00:00.000Z"];
const series = Array.from({ length: 60 }, (_, i) => ({ date: new RealDate(RealDate.UTC(2024, 0, 1) + i * 17 * 86400000).toISOString().slice(0, 10), value: "1" }));
save("internal/timerange/testdata/range.json", nows.flatMap((now) =>
  [1, 7, 10, 12].flatMap((month) =>
    RANGES.map((range) => {
      const at = new RealDate(now);
      const start = rangeStartDate(range, month, at);
      return {
        now, month, range,
        start: start ? start.toISOString() : null,
        fyStart: currentFinancialYearStart(month, at).toISOString(),
        points: pointsInRange(series, range, month, at).map((p) => p.date),
      };
    }),
  ),
));

// ---------- chart ----------

function randomSeries(n: number, opts: { income: boolean; pnl: boolean; negative: boolean }) {
  let value = rand() * 10 ** int(0, 7);
  let income = 0;
  let pnl = 0;
  return Array.from({ length: n }, (_, i) => {
    value = Math.max(opts.negative ? -Infinity : 0, value * (1 + (rand() - 0.48) * 0.2) + (opts.negative ? (rand() - 0.6) * 1000 : 0));
    if (opts.income && rand() < 0.2) income += rand() * 500;
    if (opts.pnl) pnl += (rand() - 0.5) * value * 0.05;
    return {
      date: new RealDate(RealDate.UTC(2023, 0, 1) + i * 86400000).toISOString().slice(0, 10),
      value: String(value),
      cumulativeIncome: String(income),
      unrealizedPL: String(pnl),
    };
  });
}

const chartCases = [
  ...[[1, 5, 3, 9], [0, 1_000_000, 250_000, 900_000], [1, 2, 3, 4, 5], [0, 100], [900_000, 950_000, 1_000_000], [-500, 200], [100, 100, 100], [0, 0], [42], []].map(
    (vals) => vals.map((v, i) => ({ date: `2026-01-${String(i + 1).padStart(2, "0")}`, value: String(v), cumulativeIncome: "0", unrealizedPL: "0" })),
  ),
  ...Array.from({ length: 150 }, () =>
    randomSeries(pick([0, 1, 2, 3, 7, 40, 200, 900]), { income: rand() < 0.6, pnl: rand() < 0.6, negative: rand() < 0.15 }),
  ),
];
save("internal/chart/testdata/chart.json", {
  niceCeil: [721022.17, 180000, 21, 7, 500, 1000, 0, -5, 1, 1e-3, 999.9999, 2.5e6, ...Array.from({ length: 200 }, () => rand() * 10 ** int(0, 10))].map((v) => [v, niceCeil(v)]),
  compactMoney: [1234567, 721022.17, 45, 0.25, -2500000, 0, 2.5, 3.5, 999.95, 1e9, 1.25e9, 0.005, ...Array.from({ length: 300 }, () => (rand() - 0.3) * 10 ** int(-3, 11))].map((v) => [v, compactMoney(v)]),
  resample: [[900, 50], [3, 10], [101, 101], [10, 3], [1000, 199], [7, 2]].map(([n, count]) => {
    const points = Array.from({ length: n }, (_, i) => ({ date: String(i), value: i, income: 0, pnl: 0 }));
    return [n, count, resample(points, count).map((p) => p.value)];
  }),
  render: chartCases.flatMap((history) => {
    const width = pick([20, 34, 40, 60, 80, 100, 141, 200]);
    const height = pick([1, 3, 6, 8, 9, 10, 16, 30]);
    const colour = rand() < 0.5;
    return [{ history, width, height, colour, lines: renderPortfolio(toChartPoints(history), { width, height, colour }) }];
  }),
});

// ---------- TUI ----------

const keyChunks = [
  "\u001b[A", "\u001b[B", "\u001b[C", "\u001b[D", "\u001b[H", "\u001b[F", "\u001bOA", "\u001bOx", "\u001b[Bj\u001b[B",
  "\u0003", "\u0004", "\r", "\n", "\t", "\u007f", "\b", "\u001b[200~", "\u001b[5~", "\u001b[6~", "\u001b[3~",
  "\u001b[1;5C", "\u001b", "\u001b[", "abc", "é", "[]", "q?1", "\u0001\u0002", "\u001b\u001b[A",
];
const truncateInputs = [
  "\u001b[31mabcdefghij\u001b[0m", "abc", "", "Société Générale Holdings", "東京ウォレット東京ウォレット", "a😀b😀c😀d",
  "\u001b[1mAcme\u001b[0m \u001b[2mcompany\u001b[0m", "plain text that is long enough to cut",
];
save("internal/tui/testdata/terminal.json", {
  decodeKeys: keyChunks.map((c) => [c, decodeKeys(c)]),
  truncate: truncateInputs.flatMap((t) => [0, 1, 2, 3, 4, 5, 8, 13, 40].map((w) => [t, w, truncate(t, w), visibleWidth(t)])),
});

const ENTITY_NAMES = ["Acme Trading Pty Ltd", "Smith Family Trust", "Société Générale", "東京 Holdings", "X", "A very long entity name that will not fit in a narrow window"];
function randomEntity(i: number) {
  return { id: `id-${i}`, name: pick(ENTITY_NAMES) + (i > 5 ? ` ${i}` : ""), entityType: pick(["company", "trust", "smsf", "individual"]), role: pick(["owner", "preparer", "viewer"]), createdAt: "2026-01-01T00:00:00Z" };
}
function randomHolding(i: number) {
  return {
    assetId: String(i), symbol: pick(["BTC", "ETH", "USDC", "SOL", "0xccef6bdd7534f750eb1f494367493b5fd65c905d", "😀MEME"]),
    chain: rand() < 0.5 ? pick(["solana", "ethereum", "an-extremely-long-chain-name"]) : null, imageUrl: null,
    quantity: randomDecimal(), value: rand() < 0.15 ? null : randomDecimal(), hasMismatch: rand() < 0.2, isSpam: rand() < 0.1,
    sources: Array.from({ length: int(0, 4) }, () => ({ sourceId: "s", label: pick(LABELS), calculatedQuantity: "0", reportedQuantity: null, mismatch: false })),
  };
}
function randomData() {
  return {
    holdings: Array.from({ length: pick([0, 1, 2, 5, 30]) }, (_, i) => randomHolding(i)),
    sources: Array.from({ length: int(0, 6) }, (_, i) => ({
      id: `s${i}`, entityId: "e", sourceType: "exchange_api", adapterKey: pick(["kraken", "evm", "csv", "hyperliquid-perps-adapter"]), label: pick(LABELS),
      config: {}, lastSyncedAt: rand() < 0.3 ? null : "2026-08-01T00:00:00Z", syncStatus: pick(["idle", "syncing", "error"]), syncError: null,
      syncEnabled: rand() < 0.8, createdAt: "2026-01-01T00:00:00Z", transactionCount: int(0, 20000), lastTransactionAt: null,
    })),
    tax: Array.from({ length: int(0, 4) }, (_, i) => ({
      financialYear: `${2022 + i}–${23 + i}`, startYear: 2022 + i, income: randomDecimal(), netCapitalGainLoss: randomDecimal(),
      taxableAmount: randomDecimal(), taxPayable: randomDecimal(), applicable: rand() < 0.7, currency: "aud", incomeLabel: "Income",
    })),
    zeroCost: Array.from({ length: int(0, 3) }, () => ({ ts: "2026-02-03T04:05:06Z", assetSymbol: pick(["BTC", "ETH"]), sourceLabel: pick(LABELS), quantity: randomDecimal(), proceedsAmount: randomDecimal() })),
    uncategorized: Array.from({ length: int(0, 3) }, () => ({ ts: "2026-02-03T04:05:06Z", assetSymbol: "USDC", direction: pick(["in", "out"]), amount: randomDecimal() })),
    activity: [],
    history: randomSeries(pick([0, 1, 2, 30, 90, 400]), { income: rand() < 0.5, pnl: rand() < 0.5, negative: false }),
    settings: rand() < 0.3 ? null : { entityId: "e", baseCurrency: "aud", financialYearStartMonth: pick([1, 7, 10]) },
  };
}
function randomState() {
  const entities = Array.from({ length: pick([0, 1, 3, 12, 60]) }, (_, i) => randomEntity(i));
  const selected = entities.length > 0 && rand() < 0.75 ? pick(entities) : null;
  return {
    ...initialState(),
    entities,
    entityIndex: entities.length ? int(0, entities.length - 1) : 0,
    selectedEntity: selected,
    tab: pick(TABS),
    offset: int(0, 3),
    cursor: int(0, 6),
    data: randomData(),
    loading: rand() < 0.15,
    message: rand() < 0.2 ? pick(["Sync finished", "Already syncing.", "Could not reach https://api.grubless.io: fetch failed", "Fetching page 12…"]) : null,
    messageKind: pick(["info", "error", "success"]),
    syncing: rand() < 0.15,
    showHelp: rand() < 0.05,
    range: pick(RANGES),
    tick: int(0, 20),
  };
}

const screens = Array.from({ length: 300 }, () => {
  const state = randomState();
  const width = pick([20, 40, 80, 100, 120, 200]);
  const height = pick([1, 6, 10, 20, 24, 30, 50]);
  return { state, width, height, lines: render(state as never, width, height) };
});
save("internal/tui/testdata/render.json", screens);

// Key sequences through the reducer: every intermediate state's navigation
// fields, and every action requested.
const KEYS = ["up", "down", "k", "j", "pageup", "pagedown", "home", "end", "return", "tab", "right", "left", "l", "h", "q", "escape", "?", "r", "s", "[", "]", "1", "2", "3", "4", "5", "6", "0", "x", "backspace"];
const project = (s: ReturnType<typeof initialState>) => ({
  entityIndex: s.entityIndex, selected: s.selectedEntity?.id ?? null, tab: s.tab, offset: s.offset, cursor: s.cursor,
  loading: s.loading, message: s.message, messageKind: s.messageKind, syncing: s.syncing, showHelp: s.showHelp,
  quit: s.quit, range: s.range, holdings: s.data.holdings.length,
});
save("internal/tui/testdata/reduce.json", Array.from({ length: 300 }, () => {
  // History and holdings values don't affect navigation; dropped to keep the
  // file small. Holdings stay (their count and spam flags drive rowCount).
  const random = randomState();
  const start = { ...random, showHelp: false, loading: false, data: { ...random.data, history: [] } };
  const viewport = pick([1, 2, 5, 10, 20]);
  const keys = Array.from({ length: int(1, 25) }, () => (rand() < 0.05 ? { name: pick(["c", "d"]), ctrl: true } : { name: pick(KEYS), ctrl: false }));
  let state = start as never;
  const steps = keys.map((key) => {
    const { state: next, action } = reduce(state, key, viewport);
    state = next;
    return { key, action: action.type, state: project(next) };
  });
  return { start, viewport, steps };
}));
