import { describe, it, expect } from "vitest";
import { csvToTable, parseCsv } from "../csv.js";

/**
 * These assert the *inverse* of the server's `csvField` (apps/api/src/routes/
 * reports.ts): whatever it quotes, this must unquote to the identical string.
 * The values here are real report shapes — money with thousands separators,
 * asset names with commas, notes with quotes.
 */

describe("parseCsv", () => {
  it("reads a plain CRLF table", () => {
    expect(parseCsv("a,b\r\n1,2\r\n")).toEqual([
      ["a", "b"],
      ["1", "2"],
    ]);
  });

  it("reads LF-only input too", () => {
    expect(parseCsv("a,b\n1,2\n")).toEqual([
      ["a", "b"],
      ["1", "2"],
    ]);
  });

  it("keeps a comma inside a quoted field", () => {
    // The server quotes exactly this case, and a split(\",\") would shear the
    // row in half — shifting every later column by one for that row only.
    expect(parseCsv('name,amount\r\n"Smith, Robert",1000\r\n')).toEqual([
      ["name", "amount"],
      ["Smith, Robert", "1000"],
    ]);
  });

  it("unescapes a doubled quote", () => {
    expect(parseCsv('note\r\n"he said ""no"""\r\n')).toEqual([["note"], ['he said "no"']]);
  });

  it("keeps a newline inside a quoted field", () => {
    expect(parseCsv('note,amount\r\n"line one\nline two",5\r\n')).toEqual([
      ["note", "amount"],
      ["line one\nline two", "5"],
    ]);
  });

  it("preserves empty fields, including a trailing one", () => {
    expect(parseCsv("a,b,c\r\n1,,\r\n")).toEqual([
      ["a", "b", "c"],
      ["1", "", ""],
    ]);
  });

  it("does not invent a row from the trailing newline", () => {
    expect(parseCsv("a\r\n1\r\n")).toHaveLength(2);
  });

  it("returns nothing for an empty body", () => {
    expect(parseCsv("")).toEqual([]);
  });
});

describe("csvToTable", () => {
  it("keys rows by column name", () => {
    const table = csvToTable("Asset,Proceeds\r\nBTC,1234.56\r\nETH,7.89\r\n");
    expect(table.columns).toEqual(["Asset", "Proceeds"]);
    expect(table.rows).toEqual([
      { Asset: "BTC", Proceeds: "1234.56" },
      { Asset: "ETH", Proceeds: "7.89" },
    ]);
  });

  it("keeps amounts as the server's exact strings", () => {
    // The whole point: a decimal that goes through a double loses cents, and
    // this is a tax figure. Never Number().
    const table = csvToTable("Asset,Proceeds\r\nBTC,0.000000010000000001\r\n");
    expect(table.rows[0].Proceeds).toBe("0.000000010000000001");
  });

  it("surfaces a prose line instead of dropping it", () => {
    // A report with nothing to say says so in a sentence. Swallowing it turns
    // "we couldn't compute this" into what looks like a clean, empty year.
    const table = csvToTable("Field,Value\r\nNo cached tax summary found for this financial year.\r\n");
    expect(table.rows).toHaveLength(0);
    expect(table.notes).toEqual(["No cached tax summary found for this financial year."]);
  });

  it("keeps a surplus field rather than discarding it", () => {
    const table = csvToTable("a,b\r\n1,2,3\r\n");
    expect(table.rows[0]).toEqual({ a: "1", b: "2", _extra: ["3"] });
  });

  it("fills a short row rather than shifting later columns", () => {
    expect(csvToTable("a,b,c\r\n1,2\r\n").rows[0]).toEqual({ a: "1", b: "2", c: "" });
  });

  it("returns an empty table for an empty body", () => {
    expect(csvToTable("")).toEqual({ columns: [], rows: [], notes: [] });
  });

  it("returns headers with no rows for a report that found nothing", () => {
    // The common case for a clean FY — an empty `rows` array is the right
    // answer, and the columns still tell a consumer what the report is.
    const table = csvToTable("Asset,Proceeds,Cost Basis\r\n");
    expect(table.columns).toHaveLength(3);
    expect(table.rows).toEqual([]);
  });
});
