import { describe, it, expect } from "vitest";
import { compactMoney, niceCeil, renderChart, resample, type ChartPoint } from "../tui/chart.js";
import { stripAnsi, visibleWidth } from "../tui/terminal.js";

/**
 * The chart is pure geometry — points in, `string[]` out — so all of it is
 * testable without a terminal. What's worth asserting is the frame contract
 * (exactly `height` lines, never wider than `width`), because `views.ts`
 * slots the result straight into a fixed viewport and a single extra line
 * would push the status bar off the bottom of the screen.
 */

const series = (values: number[]): ChartPoint[] =>
  values.map((value, i) => ({ date: `2026-01-${String(i + 1).padStart(2, "0")}`, value }));

describe("niceCeil", () => {
  it("rounds up to a readable axis bound", () => {
    expect(niceCeil(721_022.17)).toBe(1_000_000);
    expect(niceCeil(180_000)).toBe(200_000);
    expect(niceCeil(21)).toBe(25);
    expect(niceCeil(7)).toBe(10);
  });

  it("leaves an already-clean bound alone", () => {
    expect(niceCeil(500)).toBe(500);
    expect(niceCeil(1000)).toBe(1000);
  });

  it("returns a positive bound for a non-positive input, so the span is never zero", () => {
    expect(niceCeil(0)).toBe(1);
    expect(niceCeil(-5)).toBe(1);
  });
});

describe("compactMoney", () => {
  it("shortens by magnitude", () => {
    expect(compactMoney(1_234_567)).toBe("1.2M");
    expect(compactMoney(721_022.17)).toBe("721.0K");
    expect(compactMoney(45)).toBe("45");
    expect(compactMoney(0.25)).toBe("0.25");
  });

  it("keeps the sign", () => {
    expect(compactMoney(-2_500_000)).toBe("-2.5M");
  });

  it("writes the axis origin bare", () => {
    // "0.00" under a "1.0M" claims a precision the axis doesn't have.
    expect(compactMoney(0)).toBe("0");
  });
});

describe("resample", () => {
  it("leaves a series shorter than the target untouched", () => {
    const points = series([1, 2, 3]);
    expect(resample(points, 10)).toBe(points);
  });

  it("keeps the first and last points", () => {
    // "What is it worth now" is the question the chart is usually being
    // asked — dropping the final sample answers a different one.
    const out = resample(series(Array.from({ length: 900 }, (_, i) => i)), 50);
    expect(out).toHaveLength(50);
    expect(out[0].value).toBe(0);
    expect(out[out.length - 1].value).toBe(899);
  });

  it("keeps a real peak rather than averaging it away", () => {
    const values = Array(101).fill(10);
    values[50] = 1000;
    const out = resample(series(values), 101);
    expect(Math.max(...out.map((p) => p.value))).toBe(1000);
  });
});

describe("renderChart", () => {
  it("returns exactly the requested height", () => {
    for (const height of [3, 8, 16]) {
      expect(renderChart(series([1, 5, 3, 9]), { width: 60, height })).toHaveLength(height);
    }
  });

  it("returns exactly the requested height with no points at all", () => {
    // A brand-new entity with nothing priced yet. Short-returning here would
    // shift every line below the chart up the screen.
    const lines = renderChart([], { width: 60, height: 10 });
    expect(lines).toHaveLength(10);
    expect(stripAnsi(lines[0])).toContain("No portfolio history yet");
  });

  it("never emits a line wider than the terminal", () => {
    const lines = renderChart(series([0, 1_000_000, 250_000, 900_000]), { width: 40, height: 10 });
    for (const line of lines) expect(visibleWidth(line)).toBeLessThanOrEqual(40);
  });

  it("draws something for a series that moves", () => {
    const lines = renderChart(series([1, 2, 3, 4, 5]), { width: 40, height: 8 }).map(stripAnsi);
    expect(lines.join("")).toMatch(/[⠀-⣿]/);
  });

  it("draws a rising series low-left and high-right", () => {
    const plot = renderChart(series([0, 100]), { width: 40, height: 9 }).map(stripAnsi).slice(0, 8);
    const dotted = (line: string) => [...line].findIndex((c) => c >= "⠁" && c <= "⣿");
    const top = plot.findIndex((l) => /[⠁-⣿]/.test(l));
    const bottom = plot.map((l) => /[⠁-⣿]/.test(l)).lastIndexOf(true);
    // The topmost dotted row must have its ink further right than the
    // bottommost one — an inverted y-axis is the classic way to draw this
    // upside down, and it would misreport a gain as a loss.
    expect(dotted(plot[top])).toBeGreaterThan(dotted(plot[bottom]));
  });

  it("labels the axis from zero, not from the running minimum", () => {
    // A portfolio that never went below 900K still starts its axis at 0:
    // otherwise ordinary noise renders as a cliff.
    const lines = renderChart(series([900_000, 950_000, 1_000_000]), { width: 60, height: 10 }).map(stripAnsi);
    expect(lines[lines.length - 2]).toContain("0");
    expect(lines[0]).toContain("1.0M");
  });

  it("extends the axis below zero only when the series goes negative", () => {
    const lines = renderChart(series([-500, 200]), { width: 60, height: 10 }).map(stripAnsi);
    expect(lines[lines.length - 2]).toMatch(/-500/);
  });

  it("labels the first and last dates under the plot", () => {
    const lines = renderChart(series([1, 2, 3]), { width: 80, height: 8 }).map(stripAnsi);
    const axis = lines[lines.length - 1];
    expect(axis).toContain("2026-01-01");
    expect(axis).toContain("2026-01-03");
  });

  it("falls back to a span when the width can't hold three dates", () => {
    const lines = renderChart(series([1, 2, 3]), { width: 34, height: 6 }).map(stripAnsi);
    const axis = lines[lines.length - 1];
    expect(axis).toContain("→");
    expect(visibleWidth(axis)).toBeLessThanOrEqual(34);
  });

  it("survives a flat series", () => {
    // span would be 0 without the guard, and every y would be NaN.
    const lines = renderChart(series([100, 100, 100]), { width: 40, height: 8 }).map(stripAnsi);
    expect(lines.join("")).toMatch(/[⠀-⣿]/);
    expect(lines.join("")).not.toContain("NaN");
  });

  it("survives an all-zero series", () => {
    const lines = renderChart(series([0, 0]), { width: 40, height: 8 }).map(stripAnsi);
    expect(lines.join("")).not.toContain("NaN");
  });

  it("emits no escape codes at all under colour: false", () => {
    // `grubless portfolio` redirected to a file must not carry escapes —
    // output.ts's second rule. Every line, not just the plot: the gutter and
    // the date axis dim themselves separately.
    const lines = renderChart(series([1, 5, 3]), { width: 60, height: 8, colour: false });
    for (const line of lines) expect(line).not.toMatch(/\u001b/);
  });

  it("emits nothing but the message under colour: false with no points", () => {
    const lines = renderChart([], { width: 60, height: 6, colour: false });
    expect(lines[0]).toBe("No portfolio history yet.");
  });

  it("still colours by default, for the TUI", () => {
    expect(renderChart(series([1, 2]), { width: 60, height: 8 }).join("")).toMatch(/\u001b/);
  });

  it("survives a single point", () => {
    const lines = renderChart(series([42]), { width: 40, height: 8 });
    expect(lines).toHaveLength(8);
    expect(stripAnsi(lines.join(""))).not.toContain("NaN");
  });
});
