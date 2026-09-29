package output

import "math/big"

// SumDecimals adds decimal strings exactly — the gain/loss across a
// transaction's legs, say — and returns nil when there was nothing to add,
// so the caller can show "—" rather than a confident zero.
//
// big.Rat, not float64: these are Postgres numerics, and a sum of doubles is
// how a total ends up a cent away from the figures it's the total of.
// Anything that doesn't parse is skipped rather than guessed at.
func SumDecimals(values ...*string) *string {
	sum := new(big.Rat)
	any := false
	for _, v := range values {
		if v == nil || *v == "" {
			continue
		}
		r, ok := new(big.Rat).SetString(*v)
		if !ok {
			continue
		}
		sum.Add(sum, r)
		any = true
	}
	if !any {
		return nil
	}
	// Exact as a terminating decimal for any sum of decimal inputs; 30
	// places is beyond the 24 the API sends, and Money rounds from there.
	s := sum.FloatString(30)
	return &s
}
