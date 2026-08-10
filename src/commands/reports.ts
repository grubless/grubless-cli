import { createWriteStream, mkdirSync } from "node:fs";
import { Readable } from "node:stream";
import { pipeline } from "node:stream/promises";
import { join } from "node:path";
import type { Entity } from "../api-types.js";
import type { ApiClient } from "../client.js";
import { csvToTable } from "../csv.js";
import { CliError, ExitCode, json, note, style } from "../output.js";
import { resolveEntityScope } from "./entities.js";

/**
 * `grubless report …` — the command the CLI exists for.
 *
 * `--all-entities --out <dir>` is the accountant-channel headline: a firm with
 * 40 client entities pulls every report in one command, instead of 40 logins
 * across a tool that can't do entity tax anyway (docs/plan-cli.md §1).
 */

/** Mirrors the report routes in apps/api/src/routes/reports.ts, 1:1. */
export const REPORTS = [
  "capital-gains",
  "income",
  "fees",
  "expenses",
  "buy-sell",
  "gifts-donations-lost",
  "other-gains",
  "transaction-history",
  "balances-per-source",
  "beginning-of-year-holdings",
  "end-of-year-holdings",
  "highest-balance",
  "division-70-trading-stock",
  "ato-mytax",
] as const;

export type ReportName = (typeof REPORTS)[number];

/** The ZIP of every other report, built server-side by the same functions. */
const BUNDLE = "bundle";

/** Reports that aren't year-scoped — the API doesn't take `?year=` for these. */
const YEARLESS = new Set<string>(["balances-per-source"]);

/**
 * Reports the server renders as a document rather than a table: two PDFs and
 * the ZIP. `--json` has nothing to re-frame for these — a rendered PDF has no
 * rows — so it's refused up front with the flag that does work, rather than
 * emitting a JSON envelope around base64 nobody asked for.
 *
 * Mirrors the Content-Type each route actually sets, same as REPORTS mirrors
 * the routes themselves.
 */
const NOT_TABULAR = new Set<string>([BUNDLE, "ato-mytax", "division-70-trading-stock"]);

/** One entity's report, re-framed. Values are the server's strings, untouched. */
interface ReportDocument {
  entity: { id: string; name: string };
  report: string;
  year: string | null;
  columns: string[];
  rows: Record<string, string>[];
  notes?: string[];
}

function reportPath(entityId: string, name: string, year?: string): string {
  const route = name === BUNDLE ? "complete-tax" : name;
  const qs = YEARLESS.has(name) || !year ? "" : `?year=${encodeURIComponent(year)}`;
  return `/entities/${entityId}/reports/${route}${qs}`;
}

export async function report(
  client: ApiClient,
  name: string,
  opts: { entity?: string; allEntities?: boolean; year?: string; out?: string; json?: boolean },
): Promise<number> {
  if (name !== BUNDLE && !(REPORTS as readonly string[]).includes(name)) {
    throw new CliError(
      `Unknown report "${name}".\nAvailable: ${[...REPORTS, BUNDLE].join(", ")}`,
      ExitCode.UsageError,
    );
  }
  if (!opts.year && !YEARLESS.has(name)) {
    throw new CliError("Specify --year <startYear>, e.g. --year 2025.", ExitCode.UsageError);
  }
  if (opts.json && NOT_TABULAR.has(name)) {
    throw new CliError(
      `"${name}" is a ${name === BUNDLE ? "ZIP archive" : "rendered PDF"}, so there are no rows to emit as JSON.\n` +
        `Write it to a file instead: --out <path>.`,
      ExitCode.UsageError,
    );
  }
  if (opts.json && opts.out) {
    // Not merely redundant: --all-entities --json is ONE document, while
    // --all-entities --out is a directory of files. Supporting both at once
    // would need a third layout rule for the same two flags.
    throw new CliError("--json writes to stdout; drop --out, or redirect it.", ExitCode.UsageError);
  }

  // Keyed on the FLAG, not on how many entities happen to exist right now.
  // Writing several CSV bodies to one stdout would interleave them into an
  // unusable blob — but more importantly, a rule that depends on the current
  // entity count means the same command behaves differently for an accountant
  // who signs their second client. `--all-entities` means "directory of
  // results" on every run, from the first entity onward.
  //
  // --json is the exception, and the reason it exists: JSON nests, so one
  // document can hold forty clients' reports without them running together.
  if (opts.allEntities && !opts.out && !opts.json) {
    throw new CliError(
      "--all-entities needs somewhere to put the results: --out <dir> to write files, or --json for one document on stdout.",
      ExitCode.UsageError,
    );
  }

  const entities = await resolveEntityScope(client, opts);

  const documents: ReportDocument[] = [];
  let failures = 0;
  for (const entity of entities) {
    try {
      // Same reasoning: the per-entity subdirectory layout is a property of
      // --all-entities, not of the count, so a one-client firm gets the same
      // structure a forty-client firm does.
      if (opts.json) documents.push(await readOne(client, entity, name, opts));
      else await writeOne(client, entity, name, opts, Boolean(opts.allEntities));
    } catch (err) {
      // One client's report failing must not abandon the other 39. Collect
      // and report at the end with a non-zero exit — a silent partial run is
      // the failure mode that gets noticed at filing time.
      if (err instanceof CliError && err.exitCode === ExitCode.UpgradeRequired) throw err;
      failures++;
      note(style.red("✗") + ` ${entity.name}: ${err instanceof Error ? err.message : String(err)}`);
    }
  }

  // Printed even when some entities failed, so a partial run still yields
  // usable data — the non-zero exit and the stderr lines are what say it was
  // partial. Shape follows the flag, not the count: --all-entities is always
  // an array, --entity always a single object, so a script written against a
  // one-client firm keeps working when they sign their second.
  if (opts.json) {
    json(opts.allEntities ? documents : (documents[0] ?? null));
  }

  if (failures > 0) {
    note(style.red(`${failures} of ${entities.length} entities failed.`));
    return ExitCode.Failure;
  }
  return ExitCode.Ok;
}

/** Fetches one entity's report and re-frames it. See ../csv.ts on why here and not server-side. */
async function readOne(
  client: ApiClient,
  entity: Entity,
  name: string,
  opts: { year?: string },
): Promise<ReportDocument> {
  const text = await client.getText(reportPath(entity.id, name, opts.year));
  const { columns, rows, notes } = csvToTable(text);
  return {
    entity: { id: entity.id, name: entity.name },
    report: name,
    year: YEARLESS.has(name) ? null : (opts.year ?? null),
    columns,
    rows,
    ...(notes.length > 0 ? { notes } : {}),
  };
}

async function writeOne(
  client: ApiClient,
  entity: Entity,
  name: string,
  opts: { year?: string; out?: string },
  multi: boolean,
): Promise<void> {
  const { body, filename } = await client.getStream(reportPath(entity.id, name, opts.year));

  // No --out: straight to stdout, so `grubless report capital-gains … > x.csv`
  // and piping into other tools both work.
  if (!opts.out) {
    await pipeline(Readable.fromWeb(body as Parameters<typeof Readable.fromWeb>[0]), process.stdout, { end: false });
    return;
  }

  // A directory when the run covers several entities, otherwise the literal
  // path given. Honour the server's Content-Disposition filename — it already
  // encodes the FY label the report was actually built for.
  const target = multi
    ? join(opts.out, safeDirName(entity.name), filename ?? defaultFilename(name, opts.year))
    : opts.out;

  if (multi) mkdirSync(join(opts.out, safeDirName(entity.name)), { recursive: true });

  await pipeline(Readable.fromWeb(body as Parameters<typeof Readable.fromWeb>[0]), createWriteStream(target));
  note(style.green("✓") + ` ${entity.name} → ${target}`);
}

function defaultFilename(name: string, year?: string): string {
  const ext = name === BUNDLE ? "zip" : "csv";
  return year ? `${name}-${year}.${ext}` : `${name}.${ext}`;
}

/**
 * Entity names come from users and land in a filesystem path — "Smith & Co
 * (Trust) / 2025" would otherwise create surprise nesting or fail outright.
 */
function safeDirName(name: string): string {
  return name.replace(/[^\w.-]+/g, "-").replace(/^-+|-+$/g, "") || "entity";
}
