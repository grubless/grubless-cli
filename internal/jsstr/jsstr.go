// Package jsstr reproduces the handful of JavaScript string and number
// semantics the Node CLI's output depends on.
//
// The port's contract is byte-identical output, and the places Go and JS
// disagree are exactly the places a column goes out of alignment or a figure
// rounds differently:
//
//   - A JS string's `.length`, `.slice` and `padStart` count UTF-16 code
//     units. A Go string's `len` counts bytes. Every width in the TS is a
//     UTF-16 width, so every width here is too.
//   - `Math.round` rounds ties toward +∞; `math.Round` rounds them away from
//     zero, so they disagree on -0.5.
//   - `toFixed` breaks exact ties upward; `strconv` breaks them to even, so
//     `(2.5).toFixed(0)` is "3" in JS and "2" from FormatFloat.
//   - `String(n)` has its own rules for when to switch to exponent form.
//
// Nothing here is clever. It exists so that each of those rules is written
// down once, in one place, rather than approximated at every call site.
package jsstr

import (
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// Len is JS `s.length`: UTF-16 code units, not bytes or runes.
func Len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

// Slice is JS `s.slice(start, end)` for non-negative, in-range arguments.
//
// Cutting through a surrogate pair leaves a lone surrogate in JS, which Node
// writes to a stream as U+FFFD. That is reproduced rather than avoided,
// because avoiding it would shift every column after it by one.
func Slice(s string, start, end int) string {
	if start < 0 {
		start = 0
	}
	in := func(unit int) bool { return unit >= start && unit < end }
	var b strings.Builder
	pos := 0
	for _, r := range s {
		if pos >= end {
			break
		}
		if utf16.RuneLen(r) == 1 {
			if in(pos) {
				b.WriteRune(r)
			}
			pos++
			continue
		}
		// A surrogate pair: whole if both halves are in range, U+FFFD if one.
		switch {
		case in(pos) && in(pos+1):
			b.WriteRune(r)
		case in(pos) || in(pos+1):
			b.WriteRune(utf8.RuneError)
		}
		pos += 2
	}
	return b.String()
}

// SliceFrom is JS `s.slice(start)`.
func SliceFrom(s string, start int) string { return Slice(s, start, Len(s)) }

// PadEnd is JS `s.padEnd(width)` with spaces.
func PadEnd(s string, width int) string {
	if gap := width - Len(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}

// PadStart is JS `s.padStart(width)` with spaces.
func PadStart(s string, width int) string {
	if gap := width - Len(s); gap > 0 {
		return strings.Repeat(" ", gap) + s
	}
	return s
}

// Round is JS `Math.round`: nearest integer, ties toward +∞.
func Round(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return x
	}
	f := math.Floor(x)
	if x-f >= 0.5 {
		return f + 1
	}
	return f
}

// ToFixed is JS `x.toFixed(digits)` for finite x below 1e21.
//
// Correct rounding of the exact binary value, as FormatFloat does, except
// that an exact tie goes up rather than to even — the spec's "if there are
// two such n, pick the larger n". big.Float is only reached to detect a tie.
func ToFixed(x float64, digits int) string {
	if math.IsNaN(x) {
		return "NaN"
	}
	if math.Abs(x) >= 1e21 {
		return Number(x)
	}
	neg := x < 0
	a := math.Abs(x)
	s := strconv.FormatFloat(a, 'f', digits, 64)

	scaled := new(big.Float).SetPrec(2048).SetFloat64(a)
	scaled.Mul(scaled, new(big.Float).SetPrec(2048).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(digits)), nil)))
	whole, _ := scaled.Int(nil)
	frac := new(big.Float).SetPrec(2048).Sub(scaled, new(big.Float).SetPrec(2048).SetInt(whole))
	if frac.Cmp(big.NewFloat(0.5)) == 0 {
		// Exact tie: round up from the truncated value, whatever FormatFloat chose.
		whole.Add(whole, big.NewInt(1))
		digitsStr := whole.String()
		if digits > 0 {
			for len(digitsStr) <= digits {
				digitsStr = "0" + digitsStr
			}
			s = digitsStr[:len(digitsStr)-digits] + "." + digitsStr[len(digitsStr)-digits:]
		} else {
			s = digitsStr
		}
	}
	// (-0.001).toFixed(2) is "-0.00" in JS: the sign survives rounding to zero.
	if neg {
		return "-" + s
	}
	return s
}

// Number is JS `String(n)` for a number.
func Number(x float64) string {
	switch {
	case math.IsNaN(x):
		return "NaN"
	case math.IsInf(x, 1):
		return "Infinity"
	case math.IsInf(x, -1):
		return "-Infinity"
	case x == 0:
		return "0" // including -0, which String() prints bare
	}
	sign := ""
	if x < 0 {
		sign = "-"
		x = -x
	}
	// Shortest round-tripping digits, as the spec requires, then JS's layout.
	e := strconv.FormatFloat(x, 'e', -1, 64) // d.ddddde±XX
	mant, expStr, _ := strings.Cut(e, "e")
	digits := strings.Replace(mant, ".", "", 1)
	exp, _ := strconv.Atoi(expStr)
	k := len(digits)
	n := exp + 1

	switch {
	case k <= n && n <= 21:
		return sign + digits + strings.Repeat("0", n-k)
	case 0 < n && n <= 21:
		return sign + digits[:n] + "." + digits[n:]
	case -6 < n && n <= 0:
		return sign + "0." + strings.Repeat("0", -n) + digits
	}
	expSign := "+"
	if n-1 < 0 {
		expSign = "-"
	}
	mantissa := digits[:1]
	if k > 1 {
		mantissa += "." + digits[1:]
	}
	return sign + mantissa + "e" + expSign + strconv.Itoa(abs(n-1))
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

var decimalLiteral = regexp.MustCompile(`^[+-]?(Infinity|(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?)$`)
var radixLiteral = regexp.MustCompile(`^0([xXoObB])([0-9a-fA-F]+)$`)

// ParseNumber is JS `Number(s)` for a string: whitespace-trimmed, empty is 0,
// anything unparseable is NaN.
func ParseNumber(s string) float64 {
	s = strings.TrimFunc(s, isJSSpace)
	if s == "" {
		return 0
	}
	if m := radixLiteral.FindStringSubmatch(s); m != nil {
		base := map[byte]int{'x': 16, 'X': 16, 'o': 8, 'O': 8, 'b': 2, 'B': 2}[m[1][0]]
		n, ok := new(big.Int).SetString(m[2], base)
		if !ok {
			return math.NaN()
		}
		f, _ := new(big.Float).SetInt(n).Float64()
		return f
	}
	if !decimalLiteral.MatchString(s) {
		return math.NaN()
	}
	switch s {
	case "Infinity", "+Infinity":
		return math.Inf(1)
	case "-Infinity":
		return math.Inf(-1)
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		// Out of range: ParseFloat still returns the correctly signed ±Inf or
		// ±0, which is what Number() gives too.
		if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
			return f
		}
		return math.NaN()
	}
	return f
}

// isJSSpace is JS's WhiteSpace + LineTerminator set, as used by trim().
func isJSSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xA0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}

// Trim is JS `s.trim()`.
func Trim(s string) string { return strings.TrimFunc(s, isJSSpace) }

// TrimEnd is JS `s.trimEnd()`.
func TrimEnd(s string) string { return strings.TrimRightFunc(s, isJSSpace) }

// Max is JS `Math.max(...xs)`: NaN if any is NaN, -Infinity for none.
func Max(xs ...float64) float64 {
	m := math.Inf(-1)
	for _, x := range xs {
		if math.IsNaN(x) {
			return math.NaN()
		}
		if x > m {
			m = x
		}
	}
	return m
}

// Min is JS `Math.min(...xs)`: NaN if any is NaN, +Infinity for none.
func Min(xs ...float64) float64 {
	m := math.Inf(1)
	for _, x := range xs {
		if math.IsNaN(x) {
			return math.NaN()
		}
		if x < m {
			m = x
		}
	}
	return m
}
