import { describe, it, expect } from "vitest";
import { ellipsize, heldIn, money, qty } from "../output.js";

/**
 * The formatters are string-only on purpose — the API returns exact Postgres
 * `numeric` values and parsing them through an IEEE-754 double to print them
 * is how cents go missing. That makes carry/rounding hand-rolled, which is
 * precisely the kind of code that needs tests rather than confidence.
 *
 * Real values below are taken from the live dataset.
 */

describe("money", () => {
  it("fixes to 2dp with thousands separators", () => {
    expect(money("254142.028769325140330000")).toBe("254,142.03");
    expect(money("1071559.886401792102100000")).toBe("1,071,559.89");
    expect(money("0.000000000000000000")).toBe("0.00");
  });

  it("rounds half-up on the third decimal", () => {
    expect(money("1.005")).toBe("1.01");
    expect(money("1.004")).toBe("1.00");
    expect(money("0.999")).toBe("1.00");
  });

  it("carries into the integer part without touching a float", () => {
    // The case a naive `.slice(0,2)` truncation gets wrong, and the case a
    // float round-trip gets wrong differently.
    expect(money("9.999")).toBe("10.00");
    expect(money("999.999")).toBe("1,000.00");
    expect(money("1999999.995")).toBe("2,000,000.00");
  });

  it("keeps precision that a double would lose", () => {
    // 0.1 + 0.2 territory: this value is exactly representable as a decimal
    // string and not as a double.
    expect(money("8.454236583100000300")).toBe("8.45");
    expect(money("98636.422322129450373000")).toBe("98,636.42");
  });

  it("handles negatives, including a carry", () => {
    expect(money("-41434.915835700685932000")).toBe("-41,434.92");
    expect(money("-9.999")).toBe("-10.00");
  });

  it("renders absent values as an em dash, not as zero", () => {
    // "no price cached yet" and "worth nothing" are different statements and
    // must not look identical in a column someone is checking figures in.
    expect(money(null)).toBe("—");
    expect(money(undefined)).toBe("—");
    expect(money("")).toBe("—");
  });

  it("handles a bare integer with no decimal point", () => {
    expect(money("42")).toBe("42.00");
  });
});

describe("qty", () => {
  it("trims trailing zeros", () => {
    expect(qty("985.967271550000000000")).toBe("985.96727155");
    // Truncated at 8 decimals before the trailing zeros are stripped, so the
    // final "3" of ...976023 is dropped rather than rounded.
    expect(qty("311.108976023000000000")).toBe("311.10897602");
  });

  it("caps at 8 decimals — a satoshi, not a cent", () => {
    expect(qty("0.318136000000000000")).toBe("0.318136");
    expect(qty("0.313763290115432553")).toBe("0.31376329");
  });

  it("drops the point entirely for whole numbers", () => {
    expect(qty("300.000000000000000000")).toBe("300");
    expect(qty("0.000000000000000000")).toBe("0");
  });

  it("groups large quantities", () => {
    expect(qty("56282.264678000000000000")).toBe("56,282.264678");
  });

  it("handles negatives and absent values", () => {
    expect(qty("-2.209850000000000000")).toBe("-2.20985");
    expect(qty(null)).toBe("—");
  });

  it("expands exponential notation rather than printing it raw", () => {
    // Real value from GET /holdings — decimal.js emits exponential below a
    // threshold, so this arrives on the wire exactly like this.
    expect(qty("2e-9")).toBe("<0.00000001");
    expect(qty("1.5e-3")).toBe("0.0015");
    expect(qty("1.2345e2")).toBe("123.45");
    expect(qty("5e3")).toBe("5,000");
  });

  it("says dust is dust instead of claiming the position is empty", () => {
    expect(qty("0.000000002")).toBe("<0.00000001");
    expect(qty("-0.000000002")).toBe(">-0.00000001");
    // Exactly at the 8dp boundary is representable, so it renders normally.
    expect(qty("0.00000001")).toBe("0.00000001");
    // Genuine zero must still read as zero, not as dust.
    expect(qty("0")).toBe("0");
    expect(qty("0.000000000000000000")).toBe("0");
  });
});

describe("money with exponential input", () => {
  it("never prints raw exponential notation", () => {
    expect(money("2e-9")).toBe("0.00");
    expect(money("1.5e3")).toBe("1,500.00");
    expect(money("-2.5e-1")).toBe("-0.25");
  });
});

describe("heldIn", () => {
  it("uses the chain when the asset has one", () => {
    expect(heldIn({ chain: "solana", sources: [{ label: "sol-flex" }] })).toBe("solana");
  });

  it("falls back to the source for a chainless asset", () => {
    // A Hyperliquid or Kraken balance is exchange-native and has NO chain —
    // it isn't a network we failed to identify. "—" stated nothing here,
    // while the useful fact was already in the payload.
    expect(heldIn({ chain: null, sources: [{ label: "hyperliquid" }] })).toBe("hyperliquid");
  });

  it("lists every source when a chainless asset spans more than one", () => {
    expect(heldIn({ chain: null, sources: [{ label: "hyperliquid" }, { label: "Kraken" }] })).toBe(
      "hyperliquid, Kraken",
    );
  });

  it("collapses an overflowing list to \"first +N\" rather than cutting mid-label", () => {
    // Real row: ETH across seven sources. A raw truncation reads as one
    // mangled name and hides how many places the asset actually lives in.
    const out = heldIn({
      chain: null,
      sources: [
        { label: "evm-ellipal (Ethereum)" },
        { label: "Kraken-nodeintegration" },
        { label: "evm-nano-s (Ethereum)" },
        { label: "evm-metamask (Ethereum)" },
      ],
    });
    expect(out).toMatch(/ \+3$/);
    expect(out.length).toBeLessThanOrEqual(28);
  });

  it("falls back to an em dash only when there is genuinely nothing to say", () => {
    expect(heldIn({ chain: null, sources: [] })).toBe("—");
    expect(heldIn({ chain: null })).toBe("—");
  });

  it("truncates a single long label to the column so later columns stay aligned", () => {
    const out = heldIn({ chain: null, sources: [{ label: "Kraken-nodeintegration" }] }, 12);
    expect(out).toHaveLength(12);
    expect(out.endsWith("…")).toBe(true);
  });
});

describe("ellipsize", () => {
  it("leaves text within the cap untouched", () => {
    expect(ellipsize("USDC", 20)).toBe("USDC");
  });

  it("caps to exactly the width, ellipsis included", () => {
    // Real case: an asset whose symbol never resolved carries its 42-char
    // contract address, and one such row would pad ASSET for every other row.
    const address = "0xccef6bdd7534f750eb1f494367493b5fd65c905d";
    const out = ellipsize(address, 20);
    expect(out).toHaveLength(20);
    expect(out.startsWith("0xccef6bdd")).toBe(true);
    expect(out.endsWith("…")).toBe(true);
  });

  it("returns nothing for a non-positive width", () => {
    expect(ellipsize("abc", 0)).toBe("");
  });
});
