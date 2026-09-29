package timerange

import (
	"slices"
	"testing"
	"time"
)

// A port of src/test/range.test.ts, which pins parity with the web app's
// date-range.ts. `now` is injected: these are date boundaries, and a test
// that only passes in one month is worse than none.

var now = time.Date(2026, 8, 9, 4, 0, 0, 0, time.UTC)

func day(t time.Time) string { return t.Format("2006-01-02") }

func dates(points ...string) []string { return points }

func filter(points []string, r Key, month int, at time.Time) []string {
	return Filter(points, func(s string) string { return s }, r, month, at)
}

func TestStartDate(t *testing.T) {
	if _, ok := StartDate("all", 7, now); ok {
		t.Error("all-time has no lower bound")
	}
	// Relative to today, not the last data point: a stalled sync mustn't
	// redefine "1W".
	for r, want := range map[Key]string{"1w": "2026-08-02", "24h": "2026-08-08", "1y": "2025-08-09"} {
		if start, _ := StartDate(r, 7, now); day(start) != want {
			t.Errorf("%s starts %s, want %s", r, day(start), want)
		}
	}
}

func TestCurrentFinancialYearStart(t *testing.T) {
	// August 2026: AU (July) → 2026-07-01; US (January) → 2026-01-01.
	if got := day(CurrentFinancialYearStart(7, now)); got != "2026-07-01" {
		t.Errorf("AU = %s", got)
	}
	if got := day(CurrentFinancialYearStart(1, now)); got != "2026-01-01" {
		t.Errorf("US = %s", got)
	}
	// Before the FY's start month, it's last year's.
	if got := day(CurrentFinancialYearStart(7, time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC))); got != "2025-07-01" {
		t.Errorf("March = %s", got)
	}
}

func TestFilter(t *testing.T) {
	if got := filter(dates("2020-01-01", "2026-08-09"), "all", 7, now); len(got) != 2 {
		t.Errorf("all = %v", got)
	}
	// An early funding step is a cliff that flattens everything after it;
	// excluding it is the point of a range.
	if got := filter(dates("2024-01-01", "2026-07-05", "2026-08-09"), "fy", 7, now); !slices.Equal(got, dates("2026-07-05", "2026-08-09")) {
		t.Errorf("fy = %v", got)
	}
	if got := filter(dates("2026-07-01"), "fy", 7, now); len(got) != 1 {
		t.Error("a point on the boundary is included")
	}
	// Empty is a legitimate answer.
	if got := filter(dates("2025-01-01"), "1w", 7, now); len(got) != 0 {
		t.Errorf("1w = %v", got)
	}
}

func TestIsKey(t *testing.T) {
	for _, k := range []string{"24h", "1w", "1m", "3m", "6m", "1y", "fy", "all"} {
		if !IsKey(k) {
			t.Errorf("%q should be a key", k)
		}
	}
	for _, k := range []string{"2w", "", "ALL"} {
		if IsKey(k) {
			t.Errorf("%q should not be a key", k)
		}
	}
}
