package output

import (
	"strings"
	"testing"
)

// A port of src/test/format.test.ts. The formatters are string-only on
// purpose — a tax figure through a float64 is how cents go missing — which
// makes carry and rounding hand-rolled, which is exactly the code that needs
// tests rather than confidence. Values are from the live dataset.

type pair struct{ in, want string }

func checkAll(t *testing.T, name string, f func(*string) string, cases []pair) {
	t.Helper()
	for _, c := range cases {
		if got := f(Str(c.in)); got != c.want {
			t.Errorf("%s(%q) = %q, want %q", name, c.in, got, c.want)
		}
	}
}

func TestMoney(t *testing.T) {
	checkAll(t, "money", Money, []pair{
		// Fixed 2dp with thousands separators.
		{"254142.028769325140330000", "254,142.03"},
		{"1071559.886401792102100000", "1,071,559.89"},
		{"0.000000000000000000", "0.00"},
		// Half-up on the third decimal.
		{"1.005", "1.01"}, {"1.004", "1.00"}, {"0.999", "1.00"},
		// Carries into the integer part without touching a float.
		{"9.999", "10.00"}, {"999.999", "1,000.00"}, {"1999999.995", "2,000,000.00"},
		// Keeps precision a double would lose.
		{"8.454236583100000300", "8.45"}, {"98636.422322129450373000", "98,636.42"},
		// Negatives, including a carry.
		{"-41434.915835700685932000", "-41,434.92"}, {"-9.999", "-10.00"},
		{"42", "42.00"},
		// Never prints raw exponential notation.
		{"2e-9", "0.00"}, {"1.5e3", "1,500.00"}, {"-2.5e-1", "-0.25"},
	})
	// Absent is an em dash, not zero: "no price yet" isn't "worth nothing".
	if Money(nil) != "—" || Money(Str("")) != "—" {
		t.Error("absent money should render as —")
	}
}

func TestQty(t *testing.T) {
	checkAll(t, "qty", Qty, []pair{
		{"985.967271550000000000", "985.96727155"},
		// Truncated at 8dp before trailing zeros go, so ...976023 loses its 3.
		{"311.108976023000000000", "311.10897602"},
		// 8 decimals — a satoshi, not a cent.
		{"0.318136000000000000", "0.318136"}, {"0.313763290115432553", "0.31376329"},
		{"300.000000000000000000", "300"}, {"0.000000000000000000", "0"},
		{"56282.264678000000000000", "56,282.264678"},
		{"-2.209850000000000000", "-2.20985"},
		// Exponential from the wire (decimal.js emits it below a threshold).
		{"2e-9", "<0.00000001"}, {"1.5e-3", "0.0015"}, {"1.2345e2", "123.45"}, {"5e3", "5,000"},
		// Dust says so, rather than claiming the position is empty.
		{"0.000000002", "<0.00000001"}, {"-0.000000002", ">-0.00000001"},
		{"0.00000001", "0.00000001"}, {"0", "0"},
	})
	if Qty(nil) != "—" {
		t.Error("absent qty should render as —")
	}
}

func TestSigned(t *testing.T) {
	checkAll(t, "signed", Signed, []pair{
		{"12345.67", "+12,345.67"}, {"-987.65", "-987.65"},
		// Zero stays bare: "+0.00" claims a gain that isn't there.
		{"0", "0.00"},
	})
}

func TestHeldIn(t *testing.T) {
	cases := []struct {
		name   string
		chain  *string
		labels []string
		width  int
		want   string
	}{
		{"uses the chain when there is one", Str("solana"), []string{"sol-flex"}, 28, "solana"},
		// A Hyperliquid or Kraken balance is exchange-native and has NO chain.
		{"falls back to the source", nil, []string{"hyperliquid"}, 28, "hyperliquid"},
		{"lists every source", nil, []string{"hyperliquid", "Kraken"}, 28, "hyperliquid, Kraken"},
		{"a dash only with nothing to say", nil, nil, 28, "—"},
	}
	for _, c := range cases {
		if got := HeldIn(c.chain, c.labels, c.width); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}

	// An overflowing list becomes "first +N", never a mid-label cut: the
	// count is the fact worth seeing.
	out := HeldIn(nil, []string{"evm-wallet-a (Ethereum)", "Kraken-trading", "evm-wallet-b (Ethereum)", "evm-wallet-c (Ethereum)"}, 28)
	if !strings.HasSuffix(out, " +3") || len([]rune(out)) > 28 {
		t.Errorf("overflow = %q, want a \"first +3\" form within 28", out)
	}

	// A single long label is cut to the column so later columns stay aligned.
	out = HeldIn(nil, []string{"Kraken-trading-account"}, 12)
	if len([]rune(out)) != 12 || !strings.HasSuffix(out, "…") {
		t.Errorf("long label = %q, want 12 wide ending in …", out)
	}
}

func TestEllipsize(t *testing.T) {
	if Ellipsize("USDC", 20) != "USDC" {
		t.Error("text within the cap should be untouched")
	}
	// An unresolved symbol carries its 42-char contract address.
	out := Ellipsize("0xccef6bdd7534f750eb1f494367493b5fd65c905d", 20)
	if len([]rune(out)) != 20 || !strings.HasPrefix(out, "0xccef6bdd") || !strings.HasSuffix(out, "…") {
		t.Errorf("Ellipsize(address, 20) = %q", out)
	}
	if Ellipsize("abc", 0) != "" {
		t.Error("a non-positive width should give nothing")
	}
}
