import type { PortfolioHistoryPoint } from "../api-types.js";
import { ansi } from "./terminal.js";

/**
 * A line chart drawn with Unicode braille.
 *
 * Braille patterns (U+2800–U+28FF) address 2×4 dots per character cell, so a
 * 100×16 region of terminal gives a 200×64 plotting grid — enough for an
 * actual line rather than the staircase you get from block characters. It is
 * what every terminal plotting library uses, for the same reason.
 *
 * Pure: points in, `string[]` out. No terminal, no state, so the geometry is
 * testable directly.
 *
 * Dot-to-bit layout is not sequential, which is the one genuinely fiddly part:
 *
 *     bit0  bit3        (row 0)
 *     bit1  bit4        (row 1)
 *     bit2  bit5        (row 2)
 *     bit6  bit7        (row 3)   <- appended later in Unicode history
 */

const BRAILLE_BASE = 0x2800;
const DOTS_X = 2;
const DOTS_Y = 4;

class BrailleCanvas {
  private readonly cells: Uint8Array;

  constructor(
    readonly cols: number,
    readonly rows: number,
  ) {
    this.cells = new Uint8Array(cols * rows);
  }

  /** `x` in [0, cols*2), `y` in [0, rows*4), y measured downward. */
  set(x: number, y: number): void {
    if (x < 0 || y < 0) return;
    const cx = Math.floor(x / DOTS_X);
    const cy = Math.floor(y / DOTS_Y);
    if (cx >= this.cols || cy >= this.rows) return;

    const dx = x % DOTS_X;
    const dy = y % DOTS_Y;
    const bit = dy === 3 ? (dx === 0 ? 6 : 7) : dx * 3 + dy;
    this.cells[cy * this.cols + cx] |= 1 << bit;
  }

  /** Straight line between two dots, so gaps between samples stay connected. */
  line(x0: number, y0: number, x1: number, y1: number): void {
    const dx = Math.abs(x1 - x0);
    const dy = Math.abs(y1 - y0);
    const sx = x0 < x1 ? 1 : -1;
    const sy = y0 < y1 ? 1 : -1;
    let err = dx - dy;
    let x = x0;
    let y = y0;

    // Bresenham. A plain "plot each sample" loop leaves a dotted line wherever
    // consecutive points differ by more than one dot vertically, which on a
    // volatile series is most of them.
    for (;;) {
      this.set(x, y);
      if (x === x1 && y === y1) break;
      const e2 = 2 * err;
      if (e2 > -dy) {
        err -= dy;
        x += sx;
      }
      if (e2 < dx) {
        err += dx;
        y += sy;
      }
    }
  }

  /**
   * The dot bitmap for one cell, 0 when empty.
   *
   * Read rather than rendered here because a cell's colour depends on which
   * SERIES owns it, which only the caller holding every layer knows. See
   * merge().
   */
  maskAt(col: number, row: number): number {
    return this.cells[row * this.cols + col];
  }
}

/**
 * One day of the portfolio, as numbers.
 *
 * Numbers, not the API's decimal strings, and only here: these drive plot
 * geometry, where a braille dot is one of a couple of hundred columns and
 * precision beyond a double is meaningless. Every figure a person reads off
 * the screen is formatted from the exact strings by the caller.
 */
export interface PortfolioPoint {
  date: string;
  value: number;
  /** Running total of income received to this date. */
  income: number;
  /** Market value of open lots minus their remaining cost basis. */
  pnl: number;
}

/**
 * The API's decimal strings → plot geometry.
 *
 * The single place a portfolio figure becomes a float, so the rule that they
 * never do anywhere else stays easy to check.
 */
export function toChartPoints(points: PortfolioHistoryPoint[]): PortfolioPoint[] {
  return points.map((p) => ({
    date: p.date,
    value: Number(p.value),
    income: Number(p.cumulativeIncome),
    pnl: Number(p.unrealizedPL),
  }));
}

export interface ChartOptions {
  width: number;
  /** Total rows including the x-axis label row. */
  height: number;
  /**
   * Escape codes in the output. Defaults on, for the TUI — which only ever
   * runs on a real terminal.
   *
   * `grubless portfolio` is the caller that turns it off: its stdout may be a
   * file or a pipe, and output.ts's second rule is that a redirect gets no
   * colour. Passing the flag beats stripping afterwards, which would also
   * strip an escape that legitimately appeared in the data.
   */
  colour?: boolean;
}

/**
 * Rounds an axis bound up to a "clean" 1/2/2.5/5 × 10^n value, so labels read
 * as 0 / 500K / 1M rather than 721,022.17. Same steps as the web chart's
 * niceCeil, deliberately, so the two agree on where gridlines land.
 */
export function niceCeil(value: number): number {
  if (value <= 0) return 1;
  const exp = Math.floor(Math.log10(value));
  const base = Math.pow(10, exp);
  for (const step of [1, 2, 2.5, 5, 10]) {
    if (value <= step * base) return step * base;
  }
  return 10 * base;
}

/**
 * Short currency for an axis gutter: 1.2M, 721.0K, 45.
 *
 * Not `money()` — a y-axis with eight characters of "721,022.17" per label
 * eats the plot area it exists to describe.
 */
export function compactMoney(value: number): string {
  const abs = Math.abs(value);
  const sign = value < 0 ? "-" : "";
  // Bare "0", not "0.00" — it's the axis origin, and two decimal places there
  // claim a precision the label above it ("1.0M") doesn't have.
  if (abs === 0) return "0";
  if (abs >= 1_000_000_000) return `${sign}${(abs / 1_000_000_000).toFixed(1)}B`;
  if (abs >= 1_000_000) return `${sign}${(abs / 1_000_000).toFixed(1)}M`;
  if (abs >= 1_000) return `${sign}${(abs / 1_000).toFixed(1)}K`;
  if (abs >= 1) return `${sign}${abs.toFixed(0)}`;
  return `${sign}${abs.toFixed(2)}`;
}

/**
 * Picks `count` evenly-spaced samples across the series.
 *
 * A real entity carries years of daily points against a plot maybe 200 dots
 * wide, so something has to give. Nearest-sample beats averaging here: this
 * is a portfolio value, and an averaged peak would understate a real high the
 * user is looking for. The last point is always included, because "what is it
 * worth now" is the question the chart is usually being asked.
 */
export function resample(points: PortfolioPoint[], count: number): PortfolioPoint[] {
  if (points.length <= count || count <= 1) return points;
  const out: PortfolioPoint[] = [];
  for (let i = 0; i < count; i++) {
    const index = Math.round((i * (points.length - 1)) / (count - 1));
    out.push(points[index]);
  }
  return out;
}


const GUTTER = 9;
/** Rows the unrealised-P&L strip gets when it is drawn at all. */
const PNL_ROWS = 3;
/** Below this the strip would crowd out the main plot; it is dropped instead. */
const MIN_HEIGHT_FOR_PNL = 10;

/** One series' dots plus the colour they are drawn in. */
interface Layer {
  canvas: BrailleCanvas;
  colour: string;
}

/**
 * Renders the portfolio block: value and cumulative income on one axis, the
 * unrealised gain/loss in its own strip below, and a date axis.
 *
 * Returns exactly `height` lines so the caller can slot it into a fixed
 * viewport without re-measuring.
 *
 * Value and income share one y-axis because both are dollar amounts, and a
 * second y-scale on the same plot is the classic way to make two series look
 * related when they aren't. The gap between the two lines is then meaningful
 * on its own: it is the price-appreciation component — what the portfolio is
 * worth beyond the income it received. The web chart shades exactly that band
 * for the same reason.
 *
 * Unrealised P&L does NOT share that axis. It is a polarity measure — the
 * question is which side of zero, not how it compares to portfolio value —
 * and it crosses zero, which a zero-based magnitude axis cannot show. It gets
 * a small zero-anchored strip instead, again matching the web chart.
 */
export function renderPortfolio(points: PortfolioPoint[], opts: ChartOptions): string[] {
  const { width, height } = opts;
  const paint = painter(opts.colour !== false);
  const plotCols = Math.max(1, width - GUTTER);

  if (points.length === 0) {
    return [paint(ansi.dim, "No portfolio history yet."), ...Array(Math.max(0, height - 1)).fill("")];
  }

  // Each series is drawn only if it says something. An entity with no income
  // would otherwise get a flat line pinned to zero, which reads as data.
  const hasIncome = points.some((p) => p.income !== 0);
  const hasPnl = points.some((p) => p.pnl !== 0);

  // The strip is dropped, not squeezed: three rows taken from a nine-row plot
  // costs more than the strip conveys.
  const pnlRows = hasPnl && height >= MIN_HEIGHT_FOR_PNL ? PNL_ROWS : 0;
  const plotRows = Math.max(1, height - 1 - pnlRows);

  const dotsWide = plotCols * DOTS_X;
  const sampled = resample(points, dotsWide);

  const lines = [
    ...mainPlot(sampled, { plotCols, plotRows, dotsWide, hasIncome, paint }),
    ...(pnlRows > 0 ? pnlStrip(sampled, { plotCols, plotRows: pnlRows, dotsWide, paint }) : []),
    dateAxis(points, plotCols, paint),
  ];
  return lines;
}

interface PlotGeometry {
  plotCols: number;
  plotRows: number;
  dotsWide: number;
  hasIncome: boolean;
  paint: (code: string, text: string) => string;
}

function mainPlot(points: PortfolioPoint[], geo: PlotGeometry): string[] {
  const { plotCols, plotRows, dotsWide, hasIncome, paint } = geo;
  const series = [points.map((p) => p.value), ...(hasIncome ? [points.map((p) => p.income)] : [])];
  const all = series.flat();

  // Zero-based unless something goes negative: a portfolio chart that starts
  // its axis at the running minimum turns ordinary noise into a cliff. The
  // web chart makes the same choice.
  const rawMin = Math.min(...all);
  const max = niceCeil(Math.max(...all, 1));
  const min = rawMin < 0 ? -niceCeil(-rawMin) : 0;
  const span = max - min || 1;

  const dotsHigh = plotRows * DOTS_Y;
  const toY = (value: number) => {
    const ratio = (value - min) / span;
    // Clamp, and invert: dot row 0 is the top of the plot.
    return Math.min(dotsHigh - 1, Math.max(0, Math.round((1 - ratio) * (dotsHigh - 1))));
  };

  // Value last so it wins any cell both series land in — a merged cell can
  // only carry one colour, and the headline series is the one to keep.
  const layers: Layer[] = [
    ...(hasIncome ? [{ canvas: draw(points.map((p) => p.income), toY, plotCols, plotRows, dotsWide), colour: ansi.green }] : []),
    { canvas: draw(points.map((p) => p.value), toY, plotCols, plotRows, dotsWide), colour: ansi.cyan },
  ].reverse();

  const plot = merge(layers, plotCols, plotRows, paint);

  // Y labels on the top, middle and bottom rows only. More would compete with
  // the line for attention in a space this small.
  return plot.map((row, i) => {
    let label = "";
    if (i === 0) label = compactMoney(max);
    else if (i === plotRows - 1) label = compactMoney(min);
    else if (i === Math.floor(plotRows / 2)) label = compactMoney(min + span / 2);
    return paint(ansi.dim, label.padStart(GUTTER - 1)) + " " + row;
  });
}

/**
 * The unrealised gain/loss, on a symmetric axis anchored at zero.
 *
 * Symmetric rather than fitted to the data's own range: on this strip the
 * distance from the baseline is the whole message, and an axis that rescales
 * to the visible minimum would draw a small loss exactly like a large one.
 *
 * The line is yellow and the baseline dim, rather than the green/red the web
 * chart fills with. Green is already the income series above, and a cell here
 * holds one colour for four dot rows — so a cell straddling zero would have
 * to claim a polarity it doesn't have. Position against a visible baseline
 * says it without guessing; the header states the signed figure outright.
 */
function pnlStrip(
  points: PortfolioPoint[],
  geo: { plotCols: number; plotRows: number; dotsWide: number; paint: (code: string, text: string) => string },
): string[] {
  const { plotCols, plotRows, dotsWide, paint } = geo;
  const values = points.map((p) => p.pnl);
  const bound = niceCeil(Math.max(...values.map(Math.abs), 1));

  const dotsHigh = plotRows * DOTS_Y;
  const zeroDot = Math.floor((dotsHigh - 1) / 2);
  const toY = (value: number) => {
    const offset = (value / bound) * zeroDot;
    return Math.min(dotsHigh - 1, Math.max(0, Math.round(zeroDot - offset)));
  };

  const baseline = new BrailleCanvas(plotCols, plotRows);
  for (let x = 0; x < dotsWide; x++) baseline.set(x, zeroDot);

  const layers: Layer[] = [
    { canvas: draw(values, toY, plotCols, plotRows, dotsWide), colour: ansi.yellow },
    { canvas: baseline, colour: ansi.dim },
  ];

  const plot = merge(layers, plotCols, plotRows, paint);
  return plot.map((row, i) => {
    // Only the extremes are labelled: the strip is three rows, and a label on
    // every one would outweigh the line.
    const label = i === 0 ? compactMoney(bound) : i === plotRows - 1 ? compactMoney(-bound) : "P/L";
    return paint(ansi.dim, label.padStart(GUTTER - 1)) + " " + row;
  });
}

/** One series onto its own canvas, joined with straight segments. */
function draw(
  values: number[],
  toY: (value: number) => number,
  plotCols: number,
  plotRows: number,
  dotsWide: number,
): BrailleCanvas {
  const canvas = new BrailleCanvas(plotCols, plotRows);
  let prevX = 0;
  let prevY = toY(values[0]);
  canvas.set(prevX, prevY);

  for (let i = 1; i < values.length; i++) {
    const x = Math.round((i * (dotsWide - 1)) / (values.length - 1));
    const y = toY(values[i]);
    canvas.line(prevX, prevY, x, y);
    prevX = x;
    prevY = y;
  }
  return canvas;
}

/**
 * Flattens the layers into coloured rows.
 *
 * A braille cell carries one colour for all eight of its dots, so where two
 * series occupy the same cell only the first layer's dots are drawn — ORing
 * the masks together would paint one series' dots in another's colour, which
 * states something false. At two dots per column the collisions are rare and
 * confined to actual crossings.
 *
 * Runs of the same colour share one escape pair; a per-cell pair would make
 * a 100-column line several kilobytes of mostly escapes.
 */
function merge(layers: Layer[], cols: number, rows: number, paint: (code: string, text: string) => string): string[] {
  const out: string[] = [];

  for (let row = 0; row < rows; row++) {
    let line = "";
    let run = "";
    let runColour: string | null = null;

    const flush = () => {
      if (run !== "") line += runColour === null ? run : paint(runColour, run);
      run = "";
    };

    for (let col = 0; col < cols; col++) {
      const owner = layers.find((layer) => layer.canvas.maskAt(col, row) !== 0);
      const colour = owner?.colour ?? null;
      const char = owner ? String.fromCharCode(BRAILLE_BASE + owner.canvas.maskAt(col, row)) : " ";
      if (colour !== runColour) {
        flush();
        runColour = colour;
      }
      run += char;
    }
    flush();
    out.push(line.trimEnd());
  }
  return out;
}

/**
 * Wraps text in an escape pair, or leaves it alone. One place to make that
 * decision, so a `colour: false` caller cannot be defeated by a code path
 * that concatenates its own escapes.
 */
function painter(colour: boolean): (code: string, text: string) => string {
  return (code, text) => (colour ? `${code}${text}${ansi.reset}` : text);
}

/** First / middle / last dates, positioned under the points they describe. */
function dateAxis(points: PortfolioPoint[], plotCols: number, paint: (code: string, text: string) => string): string {
  const first = points[0].date;
  const last = points[points.length - 1].date;
  const mid = points[Math.floor(points.length / 2)].date;

  if (plotCols < first.length * 3 + 4) {
    // Not enough room for three without them colliding — show the span only.
    const text = `${first} → ${last}`;
    return " ".repeat(GUTTER) + paint(ansi.dim, text.slice(0, plotCols));
  }

  const line = Array(plotCols).fill(" ");
  const place = (text: string, start: number) => {
    for (let i = 0; i < text.length && start + i < plotCols; i++) line[start + i] = text[i];
  };
  place(first, 0);
  // On a series of two or three days the middle sample IS the first or last
  // one, and printing it again reads as three separate dates that happen to
  // repeat.
  if (mid !== first && mid !== last) place(mid, Math.floor((plotCols - mid.length) / 2));
  place(last, plotCols - last.length);

  return " ".repeat(GUTTER) + paint(ansi.dim, line.join(""));
}
