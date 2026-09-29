// Package clock is the CLI's idea of "now" for anything that prints a
// date-relative answer: a portfolio range ("the last week"), the TUI's
// "3 hours ago".
//
// GRUBLESS_NOW (RFC 3339) pins it. That exists for the recorded scenario
// tests (internal/scenarios), which would otherwise go stale the day after
// they were recorded — `portfolio --range 1w` against fixed fixtures is a
// different answer every day. Nothing else should set it.
package clock

import (
	"os"
	"time"
)

func Now() time.Time {
	if v := os.Getenv("GRUBLESS_NOW"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t
		}
	}
	return time.Now()
}
