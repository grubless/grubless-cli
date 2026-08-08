import { describe, it, expect } from "vitest";
import { currentFinancialYearStart, isRangeKey, pointsInRange, rangeStartDate } from "../range.js";

/**
 * These pin the parity with `apps/web/lib/date-range.ts`. The two files are
 * duplicated on purpose (the CLI is published to npm and cannot import from
 * the web app), so the thing worth testing is that they still agree: same
 * keys, same day counts, same "relative to today" rule.
 *
 * `now` is injected everywhere rather than mocked — these are date boundaries,
 * and a test that only passes in one month is worse than no test.
 */

const NOW = new Date("2026-08-09T04:00:00.000Z");

const series = (dates: string[]) => dates.map((date) => ({ date, value: "1" }));

describe("rangeStartDate", () => {
  it("has no lower bound for all-time", () => {
    expect(rangeStartDate("all", 7, NOW)).toBeNull();
  });

  it("counts days back from today, not from the last data point", () => {
    // A sync that stalled two months ago must not silently redefine "1W" as
    // "the week before whatever we last saw".
    expect(rangeStartDate("1w", 7, NOW)?.toISOString().slice(0, 10)).toBe("2026-08-02");
    expect(rangeStartDate("24h", 7, NOW)?.toISOString().slice(0, 10)).toBe("2026-08-08");
    expect(rangeStartDate("1y", 7, NOW)?.toISOString().slice(0, 10)).toBe("2025-08-09");
  });
});

describe("currentFinancialYearStart", () => {
  it("uses the entity's own start month, not a constant", () => {
    // August 2026, AU (July start) → the FY that began 2026-07-01.
    expect(currentFinancialYearStart(7, NOW).toISOString().slice(0, 10)).toBe("2026-07-01");
    // Same instant, US (January start) → 2026-01-01. Hardcoding either one
    // puts the boundary in the wrong place for half an account's entities.
    expect(currentFinancialYearStart(1, NOW).toISOString().slice(0, 10)).toBe("2026-01-01");
  });

  it("looks back a year when the month is before the FY start", () => {
    const march = new Date("2026-03-15T00:00:00.000Z");
    expect(currentFinancialYearStart(7, march).toISOString().slice(0, 10)).toBe("2025-07-01");
  });
});

describe("pointsInRange", () => {
  it("returns everything for all-time", () => {
    const points = series(["2020-01-01", "2026-08-09"]);
    expect(pointsInRange(points, "all", 7, NOW)).toHaveLength(2);
  });

  it("drops points before the window", () => {
    // The reported case: a large one-off funding step early in the series
    // renders as a cliff that flattens everything after it. Excluding it is
    // the whole point of a range.
    const points = series(["2024-01-01", "2026-07-05", "2026-08-09"]);
    expect(pointsInRange(points, "fy", 7, NOW).map((p) => p.date)).toEqual(["2026-07-05", "2026-08-09"]);
  });

  it("includes a point landing exactly on the boundary", () => {
    expect(pointsInRange(series(["2026-07-01"]), "fy", 7, NOW)).toHaveLength(1);
  });

  it("can legitimately return nothing", () => {
    // An entity whose history stopped last year, viewed at 1W. Empty is the
    // right answer, and the caller says "no history in this range" rather
    // than drawing an empty chart as if it were a flat one.
    expect(pointsInRange(series(["2025-01-01"]), "1w", 7, NOW)).toEqual([]);
  });
});

describe("isRangeKey", () => {
  it("accepts every documented key", () => {
    for (const key of ["24h", "1w", "1m", "3m", "6m", "1y", "fy", "all"]) {
      expect(isRangeKey(key)).toBe(true);
    }
  });

  it("rejects anything else", () => {
    expect(isRangeKey("2w")).toBe(false);
    expect(isRangeKey("")).toBe(false);
    expect(isRangeKey("ALL")).toBe(false);
  });
});
