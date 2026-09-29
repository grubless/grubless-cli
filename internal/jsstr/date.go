package jsstr

import (
	"regexp"
	"strconv"
	"time"
)

// isoDate is V8's date-time string format, as the server emits it
// (`toISOString`) plus the variations V8 also accepts on that path:
// reduced precision, a space or lowercase `t` separator, a colon-less offset.
var isoDate = regexp.MustCompile(`^(\d{4})(?:-(\d{2})(?:-(\d{2}))?)?(?:[Tt ](\d{2}):(\d{2})(?::(\d{2})(?:\.(\d+))?)?([Zz]|[+-]\d{2}:?\d{2})?)?$`)

// ParseDate is `new Date(s)` for the ISO forms, returning false where JS
// would give an Invalid Date.
//
// Like V8, the day is range-checked only against 1–31 and then allowed to
// overflow — "2025-02-30" is 2 March, not an error. Also like V8, a date-only
// form is UTC and a date-time with no offset is local time.
//
// Not covered: V8's legacy fallback parser ("July 1, 2025", "2025-7-1"),
// which the API never emits. Those return false here, where Node would
// guess a local-time date.
func ParseDate(s string) (time.Time, bool) {
	m := isoDate.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}, false
	}
	num := func(str string, fallback int) int {
		if str == "" {
			return fallback
		}
		n, _ := strconv.Atoi(str)
		return n
	}
	year := num(m[1], 0)
	month := num(m[2], 1)
	day := num(m[3], 1)
	hour, minute, second := num(m[4], 0), num(m[5], 0), num(m[6], 0)
	ms := 0
	if m[7] != "" {
		frac := (m[7] + "00")[:3] // milliseconds, truncated
		ms, _ = strconv.Atoi(frac)
	}

	if month < 1 || month > 12 || day < 1 || day > 31 || minute > 59 || second > 59 {
		return time.Time{}, false
	}
	if hour > 24 || (hour == 24 && (minute != 0 || second != 0 || ms != 0)) {
		return time.Time{}, false
	}

	loc := time.UTC
	hasTime := m[4] != ""
	switch offset := m[8]; {
	case offset == "Z" || offset == "z":
	case offset != "":
		sign := 1
		if offset[0] == '-' {
			sign = -1
		}
		digits := offset[1:]
		if len(digits) == 5 { // HH:MM
			digits = digits[:2] + digits[3:]
		}
		h, _ := strconv.Atoi(digits[:2])
		mm, _ := strconv.Atoi(digits[2:])
		loc = time.FixedZone("", sign*(h*3600+mm*60))
	case hasTime:
		loc = time.Local
	}
	return time.Date(year, time.Month(month), day, hour, minute, second, ms*int(time.Millisecond), loc), true
}

// ISODay is `date.toISOString().slice(0, 10)`.
func ISODay(t time.Time) string { return t.UTC().Format("2006-01-02") }
