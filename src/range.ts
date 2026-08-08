/**
 * Time ranges for the portfolio series.
 *
 * A deliberate mirror of `apps/web/lib/date-range.ts` — same keys, same day
 * counts, same "relative to today" rule — so the same entity looks the same in
 * both surfaces. It is duplicated rather than shared because the CLI is
 * published to npm and may not import from the web app; the cost is that the
 * two files have to be changed together, which is why they say so.
 *
 * The default matters more than it looks. The web dashboard opens on **this
 * financial year**, and the TUI opening on all-time made the same portfolio
 * look wildly different in the two places: an entity whose first month
 * includes an initial funding step shows that step as a cliff that flattens
 * everything after it. Neither view is wrong, but they should agree.
 */

const DAY_MS = 24 * 60 * 60 * 1000;

export const RANGES = ["24h", "1w", "1m", "3m", "6m", "1y", "fy", "all"] as const;
export type RangeKey = (typeof RANGES)[number];

export const RANGE_LABEL: Record<RangeKey, string> = {
  "24h": "24H",
  "1w": "1W",
  "1m": "1M",
  "3m": "3M",
  "6m": "6M",
  "1y": "1Y",
  fy: "FY",
  all: "ALL",
};

// Relative to today, not to the series' last point — a stale sync must not
// quietly redefine what "1W" means.
const RANGE_DAYS: Partial<Record<RangeKey, number>> = {
  "24h": 1,
  "1w": 7,
  "1m": 30,
  "3m": 91,
  "6m": 182,
  "1y": 365,
};

export function isRangeKey(value: string): value is RangeKey {
  return (RANGES as readonly string[]).includes(value);
}

function dayKey(date: Date): string {
  return date.toISOString().slice(0, 10);
}

/**
 * Start of the current financial year.
 *
 * `fyStartMonth` comes from the entity's own tax settings, not a constant: an
 * AU entity starts in July and a US one in January, and hardcoding either
 * would put the boundary in the wrong place for half the entities on the
 * account. Mirrors financialYearFor's bucketing in
 * apps/api/src/routes/tax-summary.ts.
 */
export function currentFinancialYearStart(fyStartMonth: number, now = new Date()): Date {
  const month = now.getUTCMonth() + 1;
  const year = now.getUTCFullYear();
  const startYear = month >= fyStartMonth ? year : year - 1;
  return new Date(Date.UTC(startYear, fyStartMonth - 1, 1));
}

/** The lower bound for a range, or null for "all time". */
export function rangeStartDate(range: RangeKey, fyStartMonth: number, now = new Date()): Date | null {
  if (range === "all") return null;
  if (range === "fy") return currentFinancialYearStart(fyStartMonth, now);
  const days = RANGE_DAYS[range]!;
  const todayStart = new Date(dayKey(now) + "T00:00:00.000Z");
  return new Date(todayStart.getTime() - days * DAY_MS);
}

/**
 * Narrows a daily series to a range.
 *
 * Dates are compared as `YYYY-MM-DD` strings, which sort lexicographically —
 * the points already carry that form, and parsing each one to a Date just to
 * compare it would be work and a timezone hazard for nothing.
 */
export function pointsInRange<T extends { date: string }>(
  points: T[],
  range: RangeKey,
  fyStartMonth: number,
  now = new Date(),
): T[] {
  const start = rangeStartDate(range, fyStartMonth, now);
  if (!start) return points;
  const cutoff = dayKey(start);
  return points.filter((p) => p.date >= cutoff);
}
