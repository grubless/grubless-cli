/**
 * A minimal RFC 4180 reader.
 *
 * Its only job is to **re-frame** a report the server already produced — CSV
 * in, the same values out as JSON. Nothing here computes, rounds, coerces or
 * re-derives: every field stays the exact string the API emitted, including
 * decimal amounts, which is the same rule the rest of the CLI follows (see
 * output.ts's header on why a tax figure must never touch a double).
 *
 * Why parse at all, when the server is the source of truth? Because the one
 * thing CSV cannot do is nest, and `--all-entities` to stdout needs a frame
 * that separates one client's report from the next. Concatenated CSVs are an
 * unusable blob; a JSON array is self-delimiting. Adding a JSON mode to
 * fourteen server routes would be the alternative, and this is the smaller,
 * reversible half of it — if the API grows `?format=json` later, this file
 * deletes.
 *
 * Hand-rolled because the CLI publishes with **zero runtime dependencies** on
 * purpose (see index.ts): this binary holds a token that reaches an
 * accounting firm's entire client list, and a CSV parser is not worth a
 * supply-chain edge.
 */

/**
 * Splits CSV text into rows of raw fields.
 *
 * Handles the three things a naive `split(",")` gets wrong, all of which
 * occur in real reports: quoted fields containing commas (asset names,
 * transaction notes), doubled quotes inside a quoted field, and newlines
 * inside a quoted field. The server's own `csvField` quotes for exactly these
 * cases, so this is its inverse.
 */
export function parseCsv(text: string): string[][] {
  const rows: string[][] = [];
  let row: string[] = [];
  let field = "";
  let quoted = false;
  // Whether anything at all has been seen on this line — distinguishes a
  // trailing newline (end of data) from a genuine empty final field.
  let started = false;

  for (let i = 0; i < text.length; i++) {
    const char = text[i];

    if (quoted) {
      if (char === '"') {
        // A doubled quote inside a quoted field is one literal quote.
        if (text[i + 1] === '"') {
          field += '"';
          i++;
        } else {
          quoted = false;
        }
      } else {
        field += char;
      }
      continue;
    }

    if (char === '"' && field === "") {
      quoted = true;
      started = true;
      continue;
    }
    if (char === ",") {
      row.push(field);
      field = "";
      started = true;
      continue;
    }
    if (char === "\r") continue; // CRLF: the \n does the work
    if (char === "\n") {
      if (started || field !== "" || row.length > 0) {
        row.push(field);
        rows.push(row);
      }
      row = [];
      field = "";
      started = false;
      continue;
    }
    field += char;
    started = true;
  }

  if (started || field !== "" || row.length > 0) {
    row.push(field);
    rows.push(row);
  }
  return rows;
}

export interface CsvTable {
  columns: string[];
  rows: Record<string, string>[];
  /**
   * Lines that aren't table rows. A report with nothing to say says so in a
   * sentence rather than emitting a bare header ("No cached tax summary found
   * for this financial year — …"), and swallowing that would turn a message
   * the user needs to read into an empty result that looks like a clean year.
   */
  notes: string[];
}

/**
 * Header row + data rows → objects keyed by column name.
 *
 * A row longer than the header keeps its surplus fields under `_extra`
 * instead of dropping them. That should not happen against a server that
 * builds every row from one column list, and precisely because it shouldn't,
 * silently discarding data if it ever does is the wrong failure: this is
 * someone's tax export.
 */
export function csvToTable(text: string): CsvTable {
  const raw = parseCsv(text);
  if (raw.length === 0) return { columns: [], rows: [], notes: [] };

  const [columns, ...body] = raw;
  const rows: Record<string, string>[] = [];
  const notes: string[] = [];

  for (const fields of body) {
    // A single-field line in a multi-column table is prose, not data.
    if (columns.length > 1 && fields.length === 1) {
      if (fields[0].trim() !== "") notes.push(fields[0]);
      continue;
    }

    const row: Record<string, string> = {};
    columns.forEach((column, i) => {
      row[column] = fields[i] ?? "";
    });
    if (fields.length > columns.length) {
      (row as Record<string, unknown>)._extra = fields.slice(columns.length);
    }
    rows.push(row);
  }

  return { columns, rows, notes };
}
