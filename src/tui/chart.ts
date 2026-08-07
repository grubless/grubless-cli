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

  render(): string[] {
    const lines: string[] = [];
    for (let row = 0; row < this.rows; row++) {
      let line = "";
      for (let col = 0; col < this.cols; col++) {
        const mask = this.cells[row * this.cols + col];
        // U+2800 itself is a blank braille cell, but it is not a space — some
        // terminals render it at a different width. Emit a real space when a
        // cell is empty.
        line += mask === 0 ? " " : String.fromCharCode(BRAILLE_BASE + mask);
      }
      lines.push(line.trimEnd());
    }
    return lines;
  }
}

export interface ChartPoint {
  date: string;
  value: number;
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
export function resample(points: ChartPoint[], count: number): ChartPoint[] {
  if (points.length <= count || count <= 1) return points;
  const out: ChartPoint[] = [];
  for (let i = 0; i < count; i++) {
    const index = Math.round((i * (points.length - 1)) / (count - 1));
    out.push(points[index]);
  }
  return out;
}

const GUTTER = 9;

/**
 * Renders the chart, including its y-axis gutter and x-axis date labels.
 *
 * Returns exactly `height` lines so the caller can slot it into a fixed
 * viewport without re-measuring.
 */
export function renderChart(points: ChartPoint[], opts: ChartOptions): string[] {
  const { width, height } = opts;
  const paint = painter(opts.colour !== false);
  const plotCols = Math.max(1, width - GUTTER);
  const plotRows = Math.max(1, height - 1); // last row is the date axis

  if (points.length === 0) {
    return [paint(ansi.dim, "No portfolio history yet."), ...Array(Math.max(0, height - 1)).fill("")];
  }

  const values = points.map((p) => p.value);
  const rawMax = Math.max(...values);
  const rawMin = Math.min(...values);

  // Zero-based unless the series goes negative: a portfolio chart that starts
  // its axis at the running minimum turns ordinary noise into a cliff. The
  // web chart makes the same choice.
  const max = niceCeil(rawMax > 0 ? rawMax : 1);
  const min = rawMin < 0 ? -niceCeil(-rawMin) : 0;
  const span = max - min || 1;

  const canvas = new BrailleCanvas(plotCols, plotRows);
  const dotsWide = plotCols * DOTS_X;
  const dotsHigh = plotRows * DOTS_Y;

  const sampled = resample(points, dotsWide);
  const toY = (value: number) => {
    const ratio = (value - min) / span;
    // Clamp, and invert: dot row 0 is the top of the plot.
    return Math.min(dotsHigh - 1, Math.max(0, Math.round((1 - ratio) * (dotsHigh - 1))));
  };

  let prevX = 0;
  let prevY = toY(sampled[0].value);
  canvas.set(prevX, prevY);

  for (let i = 1; i < sampled.length; i++) {
    const x = sampled.length === 1 ? 0 : Math.round((i * (dotsWide - 1)) / (sampled.length - 1));
    const y = toY(sampled[i].value);
    canvas.line(prevX, prevY, x, y);
    prevX = x;
    prevY = y;
  }

  const plot = canvas.render();

  // Y labels on the top, middle and bottom rows only. More would compete with
  // the line for attention in a space this small.
  const lines: string[] = [];
  for (let row = 0; row < plotRows; row++) {
    let label = "";
    if (row === 0) label = compactMoney(max);
    else if (row === plotRows - 1) label = compactMoney(min);
    else if (row === Math.floor(plotRows / 2)) label = compactMoney(min + span / 2);

    lines.push(paint(ansi.dim, label.padStart(GUTTER - 1)) + " " + paint(ansi.cyan, plot[row] ?? ""));
  }

  lines.push(dateAxis(points, plotCols, paint));
  return lines;
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
function dateAxis(points: ChartPoint[], plotCols: number, paint: (code: string, text: string) => string): string {
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
