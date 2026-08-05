/**
 * Output formatting and exit codes.
 *
 * Two rules drive everything here:
 *
 * 1. **stdout is data, stderr is commentary.** `grubless report … | tee` and
 *    `grubless holdings --json | jq` have to work, which means progress
 *    messages, warnings and errors never touch stdout.
 * 2. **No colour when stdout isn't a TTY.** Escape codes in a redirected file
 *    or a CI log are noise at best and break downstream parsing at worst.
 */

export const ExitCode = {
  Ok: 0,
  /** The operation ran and failed (server error, sync failure, not found). */
  Failure: 1,
  /** The command line itself was wrong — bad flag, missing argument. */
  UsageError: 2,
  /** No credential, or the credential was rejected. */
  AuthFailure: 3,
  /** `--fail-on-blocking` found blocking issues. Not an error: an answer. */
  BlockingWarnings: 4,
  /** A paid plan is required. Distinct from Failure so CI can treat it differently. */
  UpgradeRequired: 5,
} as const;

export type ExitCodeValue = (typeof ExitCode)[keyof typeof ExitCode];

/** An error already phrased for a user — printed as-is, with no stack trace. */
export class CliError extends Error {
  constructor(
    message: string,
    readonly exitCode: ExitCodeValue = ExitCode.Failure,
  ) {
    super(message);
    this.name = "CliError";
  }
}

const useColour = process.stdout.isTTY && !process.env.NO_COLOR;

const codes = {
  dim: "\u001b[2m",
  bold: "\u001b[1m",
  red: "\u001b[31m",
  yellow: "\u001b[33m",
  green: "\u001b[32m",
  reset: "\u001b[0m",
};

function paint(code: string, text: string): string {
  return useColour ? `${code}${text}${codes.reset}` : text;
}

export const style = {
  dim: (t: string) => paint(codes.dim, t),
  bold: (t: string) => paint(codes.bold, t),
  red: (t: string) => paint(codes.red, t),
  yellow: (t: string) => paint(codes.yellow, t),
  green: (t: string) => paint(codes.green, t),
};

/** Data. Always stdout. */
export function out(line = ""): void {
  process.stdout.write(line + "\n");
}

/** Commentary — progress, warnings, errors. Always stderr. */
export function note(line = ""): void {
  process.stderr.write(line + "\n");
}

export function json(value: unknown): void {
  out(JSON.stringify(value, null, 2));
}

export interface Column<T> {
  header: string;
  value: (row: T) => string;
  /** Right-align — for numbers, where a ragged decimal column is unreadable. */
  align?: "left" | "right";
}

/**
 * Plain aligned columns, not box-drawing. A table a user can pipe into `grep`
 * or `awk` is more useful in a terminal than one that looks prettier, and
 * box characters wrap badly in narrow windows.
 */
export function table<T>(rows: T[], columns: Column<T>[]): void {
  if (rows.length === 0) return;

  const cells = rows.map((row) => columns.map((c) => c.value(row)));
  const widths = columns.map((c, i) => Math.max(c.header.length, ...cells.map((r) => r[i].length)));

  const pad = (text: string, width: number, align: "left" | "right" = "left") =>
    align === "right" ? text.padStart(width) : text.padEnd(width);

  out(style.dim(columns.map((c, i) => pad(c.header, widths[i], c.align)).join("  ")));
  for (const row of cells) {
    out(row.map((text, i) => pad(text, widths[i], columns[i].align)).join("  "));
  }
}

/**
 * Decimal-string formatting for HUMAN tables only.
 *
 * The API returns exact values from a Postgres `numeric` — a tax figure
 * arrives as "254142.028769325140330000". That precision is the whole point
 * on the wire and must never be rounded away in `--json`, which is what a
 * script or a downstream ledger consumes. But 24 decimal places in a terminal
 * column is unreadable, and unreadable is its own kind of wrong for a tool an
 * accountant is meant to check figures with.
 *
 * So: format at the presentation layer, never at the data layer. `--json`
 * emits the server's strings verbatim.
 *
 * String manipulation rather than `Number()` throughout — parsing a 24-digit
 * decimal into an IEEE-754 double to print it is exactly how cents go missing.
 */

/**
 * Expands exponential notation into plain decimal digits.
 *
 * Not hypothetical: `GET /holdings` returns `"2e-9"` for a dust BTC balance on
 * real data. decimal.js's `toString()` switches to exponential below a
 * threshold, so any sufficiently small quantity arrives this way. Printing it
 * raw put a literal `2e-9` in a QUANTITY column, which reads as a bug in the
 * tool rather than as a tiny number.
 *
 * String-based, like everything else here — the whole point is never to put
 * one of these through a double.
 */
function expandExponential(value: string): string {
  const match = /^(-?)(\d+)(?:\.(\d+))?[eE]([+-]?\d+)$/.exec(value);
  if (!match) return value;

  const [, sign, whole, frac = "", expText] = match;
  const exp = Number.parseInt(expText, 10);
  const digits = whole + frac;
  // Where the decimal point lands once the exponent is applied.
  const point = whole.length + exp;

  if (point <= 0) return `${sign}0.${"0".repeat(-point)}${digits}`;
  if (point >= digits.length) return `${sign}${digits}${"0".repeat(point - digits.length)}`;
  return `${sign}${digits.slice(0, point)}.${digits.slice(point)}`;
}

/** True for a value that is non-zero but too small to show at `places` dp. */
function isDustBelow(plain: string, places: number): boolean {
  const [whole = "0", frac = ""] = plain.replace(/^-/, "").split(".");
  const nonZero = /[1-9]/.test(whole) || /[1-9]/.test(frac);
  const visible = /[1-9]/.test(whole) || /[1-9]/.test(frac.slice(0, places));
  return nonZero && !visible;
}

/** Money: fixed 2dp with thousands separators. */
export function money(value: string | null | undefined): string {
  if (value == null || value === "") return "—";
  value = expandExponential(value);
  const negative = value.startsWith("-");
  const [whole = "0", frac = ""] = value.replace(/^-/, "").split(".");

  // Round half-up on the third decimal, carrying into the integer part by
  // hand so no float is ever involved.
  let cents = (frac + "00").slice(0, 2);
  if (Number(frac[2] ?? "0") >= 5) {
    const bumped = String(BigInt(cents) + 1n).padStart(2, "0");
    if (bumped.length > 2) {
      return sign(negative) + group(String(BigInt(whole) + 1n)) + ".00";
    }
    cents = bumped;
  }
  return sign(negative) + group(whole) + "." + cents;
}

/**
 * Quantities: trailing zeros trimmed, capped at 8 decimals.
 *
 * 8 rather than 2 because crypto amounts genuinely need them (a satoshi is
 * 1e-8), and rounding a holding to cents would misreport a real position.
 */
export function qty(value: string | null | undefined): string {
  if (value == null || value === "") return "—";
  const plain = expandExponential(value);

  // A holding of 2e-9 BTC is real, just far below 8dp. Rendering it as "0"
  // would state something false — that the position is empty — on the exact
  // screen someone uses to check whether a balance reconciles. Say "smaller
  // than we're showing" instead.
  if (isDustBelow(plain, 8)) return plain.startsWith("-") ? ">-0.00000001" : "<0.00000001";

  const negative = plain.startsWith("-");
  const [whole = "0", frac = ""] = plain.replace(/^-/, "").split(".");
  const trimmed = frac.slice(0, 8).replace(/0+$/, "");
  return sign(negative) + group(whole) + (trimmed ? "." + trimmed : "");
}

function sign(negative: boolean): string {
  return negative ? "-" : "";
}

function group(digits: string): string {
  return digits.replace(/\B(?=(\d{3})+(?!\d))/g, ",");
}

/** ISO timestamp → a short local date, or an em dash for null. */
export function shortDate(value: string | null | undefined): string {
  if (!value) return "—";
  const d = new Date(value);
  return Number.isNaN(d.getTime()) ? "—" : d.toISOString().slice(0, 10);
}
