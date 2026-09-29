package output

import (
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/grubless/grubless-cli/internal/jsstr"
)

// Decimal-string formatting for HUMAN tables only. `--json` emits the
// server's strings verbatim. String manipulation throughout, never float64:
// parsing a 24-digit decimal into a double to print it is how cents go
// missing. A line-for-line port of the TS build's formatters.
//
// Formatters take *string because the TS distinguishes a value from
// null/undefined ("—") — which, in a column someone is checking figures in,
// is the difference between "no price yet" and "worth nothing".

// Str is a convenience for passing a plain string where *string is taken.
func Str(s string) *string { return &s }

var exponential = regexp.MustCompile(`^(-?)(\d+)(?:\.(\d+))?[eE]([+-]?\d+)$`)

// expandExponential turns "2e-9" into "0.000000002". Not hypothetical: the
// holdings endpoint returns exactly that for a dust BTC balance.
func expandExponential(value string) string {
	m := exponential.FindStringSubmatch(value)
	if m == nil {
		return value
	}
	sign, whole, frac := m[1], m[2], m[3]
	exp, _ := strconv.Atoi(m[4])
	digits := whole + frac
	point := len(whole) + exp

	switch {
	case point <= 0:
		return sign + "0." + strings.Repeat("0", -point) + digits
	case point >= len(digits):
		return sign + digits + strings.Repeat("0", point-len(digits))
	}
	return sign + digits[:point] + "." + digits[point:]
}

var nonZeroDigit = regexp.MustCompile(`[1-9]`)

// splitDecimal is `value.replace(/^-/, "").split(".")` with the TS's
// destructuring defaults: whole defaults to "0" only when absent.
func splitDecimal(value string) (whole, frac string) {
	parts := strings.Split(strings.TrimPrefix(value, "-"), ".")
	whole = parts[0]
	if len(parts) > 1 {
		frac = parts[1]
	}
	return whole, frac
}

func isDustBelow(plain string, places int) bool {
	whole, frac := splitDecimal(plain)
	nonZero := nonZeroDigit.MatchString(whole) || nonZeroDigit.MatchString(frac)
	visible := nonZeroDigit.MatchString(whole) || nonZeroDigit.MatchString(jsstr.Slice(frac, 0, places))
	return nonZero && !visible
}

// Money is fixed 2dp with thousands separators, rounding half-up on the third
// decimal and carrying by hand.
func Money(value *string) string {
	if value == nil || *value == "" {
		return "—"
	}
	v := expandExponential(*value)
	negative := strings.HasPrefix(v, "-")
	whole, frac := splitDecimal(v)

	cents := jsstr.Slice(frac+"00", 0, 2)
	if len(frac) > 2 && frac[2] >= '5' && frac[2] <= '9' {
		bumped := bigInt(cents)
		bumped.Add(bumped, big.NewInt(1))
		b := jsstr.PadStart(bumped.String(), 2)
		b = strings.ReplaceAll(b, " ", "0")
		if len(b) > 2 {
			w := bigInt(whole)
			w.Add(w, big.NewInt(1))
			return sign(negative) + group(w.String()) + ".00"
		}
		cents = b
	}
	return sign(negative) + group(whole) + "." + cents
}

// bigInt is BigInt(s) for the digit strings money() handles; "" is 0n.
func bigInt(s string) *big.Int {
	n, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return new(big.Int)
	}
	return n
}

// Signed is Money with an explicit "+" on a gain. Zero stays bare.
func Signed(value *string) string {
	text := Money(value)
	if strings.HasPrefix(text, "-") || !nonZeroDigit.MatchString(text) {
		return text
	}
	return "+" + text
}

// Qty trims trailing zeros and caps at 8 decimals — a satoshi, not a cent.
func Qty(value *string) string {
	if value == nil || *value == "" {
		return "—"
	}
	plain := expandExponential(*value)

	// A holding of 2e-9 BTC is real. Rendering it as "0" would state that
	// the position is empty.
	if isDustBelow(plain, 8) {
		if strings.HasPrefix(plain, "-") {
			return ">-0.00000001"
		}
		return "<0.00000001"
	}

	negative := strings.HasPrefix(plain, "-")
	whole, frac := splitDecimal(plain)
	trimmed := strings.TrimRight(jsstr.Slice(frac, 0, 8), "0")
	out := sign(negative) + group(whole)
	if trimmed != "" {
		out += "." + trimmed
	}
	return out
}

func sign(negative bool) string {
	if negative {
		return "-"
	}
	return ""
}

// group is `digits.replace(/\B(?=(\d{3})+(?!\d))/g, ",")`, literally: a comma
// at every non-boundary position followed by a multiple of three digits and
// then a non-digit. For the all-digit strings it meets, that's ordinary
// thousands grouping.
func group(s string) string {
	isWord := func(c byte) bool {
		return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
	}
	isDigit := func(c byte) bool { return c >= '0' && c <= '9' }

	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if i > 0 && isWord(s[i-1]) == isWord(s[i]) {
			run := 0
			for j := i; j < len(s) && isDigit(s[j]); j++ {
				run++
			}
			if run > 0 && run%3 == 0 {
				b.WriteByte(',')
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// Ellipsize hard-truncates plain text so a following column stays aligned.
// Widths are UTF-16 units, as in the TS.
func Ellipsize(text string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if jsstr.Len(text) <= maxWidth {
		return text
	}
	return jsstr.Slice(text, 0, maxWidth-1) + "…"
}

// HeldIn is where a holding lives: its chain, or the source(s) holding it.
// A chainless holding isn't a network we failed to identify — a Kraken or
// Hyperliquid balance is exchange-native — so the source is the fact worth
// showing. An overflowing list becomes "first +N", never a mid-label cut.
func HeldIn(chain *string, sourceLabels []string, maxWidth int) string {
	if chain != nil && *chain != "" {
		return Ellipsize(*chain, maxWidth)
	}
	if len(sourceLabels) == 0 {
		return "—"
	}
	joined := strings.Join(sourceLabels, ", ")
	if jsstr.Len(joined) <= maxWidth {
		return joined
	}
	if len(sourceLabels) == 1 {
		return Ellipsize(sourceLabels[0], maxWidth)
	}
	suffix := " +" + strconv.Itoa(len(sourceLabels)-1)
	return Ellipsize(sourceLabels[0], max(1, maxWidth-jsstr.Len(suffix))) + suffix
}

// ShortDate is an ISO timestamp as its UTC date, or an em dash.
func ShortDate(value *string) string {
	if value == nil || *value == "" {
		return "—"
	}
	t, ok := jsstr.ParseDate(*value)
	if !ok {
		return "—"
	}
	return jsstr.ISODay(t)
}

// ParseTime is `new Date(value)` for an ISO timestamp, for callers that need
// the instant rather than ShortDate's day.
func ParseTime(value string) (time.Time, bool) { return jsstr.ParseDate(value) }
