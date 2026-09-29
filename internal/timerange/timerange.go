// Package timerange holds the portfolio series' time ranges — a mirror of
// the web app's date-range.ts (apps/web/lib in the main repo). The
// same keys, day counts and "relative to today" rule, so one entity looks the
// same on every surface.
package timerange

import (
	"slices"
	"time"
)

type Key string

// Keys in display order. `fy` is the interactive default; `all` is the
// command's. They differ on purpose: a screen has to pick a window, and the
// useful one is the year being filed, as the web dashboard opens on; a
// command reading into a pipe hands over everything unless told otherwise.
var Keys = []Key{"24h", "1w", "1m", "3m", "6m", "1y", "fy", "all"}

var Label = map[Key]string{
	"24h": "24H", "1w": "1W", "1m": "1M", "3m": "3M",
	"6m": "6M", "1y": "1Y", "fy": "FY", "all": "ALL",
}

// Relative to today, not to the series' last point — a stale sync must not
// quietly redefine what "1W" means.
var days = map[Key]int{"24h": 1, "1w": 7, "1m": 30, "3m": 91, "6m": 182, "1y": 365}

func IsKey(s string) bool { return slices.Contains(Keys, Key(s)) }

// CurrentFinancialYearStart uses the entity's own start month — July for AU,
// January for US — never a constant.
func CurrentFinancialYearStart(fyStartMonth int, now time.Time) time.Time {
	now = now.UTC()
	month := int(now.Month())
	year := now.Year()
	startYear := year
	if month < fyStartMonth {
		startYear = year - 1
	}
	// time.Date normalises an out-of-range month as Date.UTC does.
	return time.Date(startYear, time.Month(fyStartMonth), 1, 0, 0, 0, 0, time.UTC)
}

// StartDate is the lower bound for a range; ok is false for all-time.
func StartDate(r Key, fyStartMonth int, now time.Time) (start time.Time, ok bool) {
	switch r {
	case "all":
		return time.Time{}, false
	case "fy":
		return CurrentFinancialYearStart(fyStartMonth, now), true
	}
	now = now.UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	// A fixed 24h per day, as the TS subtracts DAY_MS — not AddDate, which
	// would be the same in UTC anyway but says something different.
	return today.Add(-time.Duration(days[r]) * 24 * time.Hour), true
}

// Filter narrows a daily series to a range, comparing YYYY-MM-DD strings.
func Filter[T any](points []T, date func(T) string, r Key, fyStartMonth int, now time.Time) []T {
	start, ok := StartDate(r, fyStartMonth, now)
	if !ok {
		return points
	}
	cutoff := start.Format("2006-01-02")
	out := make([]T, 0, len(points))
	for _, p := range points {
		// JS `>=` on strings compares UTF-16 code units; for the ASCII dates
		// this sees, that's byte order.
		if date(p) >= cutoff {
			out = append(out, p)
		}
	}
	return out
}
