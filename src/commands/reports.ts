import { createWriteStream, mkdirSync } from "node:fs";
import { Readable } from "node:stream";
import { pipeline } from "node:stream/promises";
import { join } from "node:path";
import type { Entity } from "@grubless/api-types";
import type { ApiClient } from "../client.js";
import { CliError, ExitCode, note, style } from "../output.js";
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

function reportPath(entityId: string, name: string, year?: string): string {
  const route = name === BUNDLE ? "complete-tax" : name;
  const qs = YEARLESS.has(name) || !year ? "" : `?year=${encodeURIComponent(year)}`;
  return `/entities/${entityId}/reports/${route}${qs}`;
}

export async function report(
  client: ApiClient,
  name: string,
  opts: { entity?: string; allEntities?: boolean; year?: string; out?: string },
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

  // Keyed on the FLAG, not on how many entities happen to exist right now.
  // Writing several CSV bodies to one stdout would interleave them into an
  // unusable blob — but more importantly, a rule that depends on the current
  // entity count means the same command behaves differently for an accountant
  // who signs their second client. `--all-entities` means "directory of
  // results" on every run, from the first entity onward.
  if (opts.allEntities && !opts.out) {
    throw new CliError("--all-entities needs --out <dir> to write into.", ExitCode.UsageError);
  }

  const entities = await resolveEntityScope(client, opts);

  let failures = 0;
  for (const entity of entities) {
    try {
      // Same reasoning: the per-entity subdirectory layout is a property of
      // --all-entities, not of the count, so a one-client firm gets the same
      // structure a forty-client firm does.
      await writeOne(client, entity, name, opts, Boolean(opts.allEntities));
    } catch (err) {
      // One client's report failing must not abandon the other 39. Collect
      // and report at the end with a non-zero exit — a silent partial run is
      // the failure mode that gets noticed at filing time.
      if (err instanceof CliError && err.exitCode === ExitCode.UpgradeRequired) throw err;
      failures++;
      note(style.red("✗") + ` ${entity.name}: ${err instanceof Error ? err.message : String(err)}`);
    }
  }

  if (failures > 0) {
    note(style.red(`${failures} of ${entities.length} entities failed.`));
    return ExitCode.Failure;
  }
  return ExitCode.Ok;
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
