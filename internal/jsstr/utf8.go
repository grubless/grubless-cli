package jsstr

import (
	"strings"
	"unicode/utf8"
)

// DecodeUTF8 decodes bytes the way Node turns them into a JS string: `fetch`'s
// `res.text()` and `res.json()` (stripBOM true), or `readFileSync(p, "utf8")`
// (stripBOM false — Buffer decoding keeps a leading U+FEFF).
//
// The BOM matters more than it looks: report CSVs are often written with one
// for Excel's sake, and without stripping it the first column of every
// `report --json` would be named "\uFEFFDate" instead of "Date".
//
// Invalid bytes become U+FFFD per the WHATWG "maximal subpart" rule, which
// can emit fewer replacement characters than decoding byte by byte.
func DecodeUTF8(b []byte, stripBOM bool) string {
	if stripBOM && len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		b = b[3:]
	}
	if utf8.Valid(b) {
		return string(b)
	}
	var out strings.Builder
	for i := 0; i < len(b); {
		r, size := utf8.DecodeRune(b[i:])
		if r != utf8.RuneError || size > 1 {
			out.WriteRune(r)
			i += size
			continue
		}
		out.WriteRune(utf8.RuneError)
		i += maximalSubpart(b[i:])
	}
	return out.String()
}

// maximalSubpart is how many bytes of an invalid sequence one U+FFFD
// replaces: the longest prefix that could still have begun a valid sequence,
// or 1.
func maximalSubpart(b []byte) int {
	lead := b[0]
	var need int
	lo, hi := byte(0x80), byte(0xBF)
	switch {
	case lead >= 0xC2 && lead <= 0xDF:
		need = 1
	case lead == 0xE0:
		need, lo = 2, 0xA0
	case lead >= 0xE1 && lead <= 0xEC, lead == 0xEE, lead == 0xEF:
		need = 2
	case lead == 0xED:
		need, hi = 2, 0x9F
	case lead == 0xF0:
		need, lo = 3, 0x90
	case lead >= 0xF1 && lead <= 0xF3:
		need = 3
	case lead == 0xF4:
		need, hi = 3, 0x8F
	default:
		return 1
	}
	n := 1
	for n <= need && n < len(b) {
		if b[n] < lo || b[n] > hi {
			break
		}
		lo, hi = 0x80, 0xBF // only the first continuation byte is constrained
		n++
	}
	return n
}
