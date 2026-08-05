import { readFileSync } from "node:fs";
import type { Entity, Source } from "@grubless/api-types";
import type { ApiClient } from "../client.js";
import { CliError, ExitCode, json, money, note, out, qty, shortDate, style, table } from "../output.js";
import { resolveEntity, resolveEntityScope } from "./entities.js";
import { DEFAULT_WAIT_TIMEOUT_MS, reportWaitResult, waitForActivity } from "./wait.js";

export async function sourcesList(
  client: ApiClient,
  opts: { entity?: string; allEntities?: boolean; json?: boolean },
): Promise<number> {
  const entities = await resolveEntityScope(client, opts);
  const rows: Array<Source & { entityName: string }> = [];
  for (const entity of entities) {
    const sources = await client.get<Source[]>(`/entities/${entity.id}/sources`);
    rows.push(...sources.map((s) => ({ ...s, entityName: entity.name })));
  }

  if (opts.json) {
    json(rows);
    return ExitCode.Ok;
  }
  if (rows.length === 0) {
    note("No sources.");
    return ExitCode.Ok;
  }

  table(rows, [
    { header: "ID", value: (s) => s.id },
    ...(entities.length > 1 ? [{ header: "ENTITY", value: (s: (typeof rows)[number]) => s.entityName }] : []),
    { header: "LABEL", value: (s) => s.label },
    { header: "ADAPTER", value: (s) => s.adapterKey },
    { header: "TXNS", value: (s) => String(s.transactionCount), align: "right" as const },
    { header: "SYNCED", value: (s) => shortDate(s.lastSyncedAt) },
    {
      header: "STATUS",
      // A disabled source reads as "off", not "idle" — idle implies it will
      // sync on the next run, and this one never will until re-enabled.
      value: (s) => (!s.syncEnabled ? "off" : s.syncStatus === "error" ? style.red("error") : s.syncStatus),
    },
  ]);

  const errored = rows.filter((s) => s.syncStatus === "error");
  for (const s of errored) note(style.red(`✗ ${s.label}: ${s.syncError ?? "sync failed"}`));
  return ExitCode.Ok;
}

export async function sourcesSync(
  client: ApiClient,
  opts: {
    entity?: string;
    allEntities?: boolean;
    source?: string;
    all?: boolean;
    full?: boolean;
    wait?: boolean;
    timeoutMinutes?: number;
  },
): Promise<number> {
  if (!opts.source && !opts.all) {
    throw new CliError("Specify --source <id>, or --all to sync every source on the entity.", ExitCode.UsageError);
  }

  const entities = await resolveEntityScope(client, opts);
  let worstExit: number = ExitCode.Ok;

  for (const entity of entities) {
    const sources = await client.get<Source[]>(`/entities/${entity.id}/sources`);
    const targets = opts.all
      ? sources.filter((s) => s.syncEnabled)
      : sources.filter((s) => s.id === opts.source || s.label === opts.source);

    if (targets.length === 0) {
      if (opts.all) {
        note(`${entity.name}: no syncable sources.`);
        continue;
      }
      throw new CliError(`No source matching "${opts.source}" on ${entity.name}.`, ExitCode.UsageError);
    }

    // Captured BEFORE triggering, so a job that finishes between the trigger
    // and the first poll is still observed rather than missed.
    const since = new Date();

    for (const source of targets) {
      // skipPriceBackfill when firing several at once: each sync otherwise
      // runs its own entity-wide backfill against the same rate-limited
      // providers. The API/worker already model this — see
      // SyncSourceJobData.skipPriceBackfill.
      await client.post(`/sources/${source.id}/sync`, {
        full: opts.full ?? false,
        skipPriceBackfill: targets.length > 1,
      });
      note(`${style.dim("→")} queued ${source.label}${opts.full ? " (full resync)" : ""}`);
    }

    if (!opts.wait) continue;

    const result = await waitForActivity(client, entity.id, since, {
      timeoutMs: opts.timeoutMinutes ? opts.timeoutMinutes * 60_000 : DEFAULT_WAIT_TIMEOUT_MS,
    });
    const exit = reportWaitResult(result, `${entity.name}: sync finished`);
    if (exit !== ExitCode.Ok) worstExit = exit;
  }

  if (!opts.wait) {
    note(style.dim("Queued. Pass --wait to block until they finish."));
  }
  return worstExit;
}

export async function sourcesImport(
  client: ApiClient,
  filePath: string,
  opts: { entity?: string; source?: string; wait?: boolean; timeoutMinutes?: number },
): Promise<number> {
  if (!opts.source) throw new CliError("Specify --source <id> to import into.", ExitCode.UsageError);
  if (!opts.entity) throw new CliError("Specify --entity <id|name>.", ExitCode.UsageError);

  const entity: Entity = await resolveEntity(client, opts.entity);

  let fileContent: string;
  try {
    fileContent = readFileSync(filePath, "utf8");
  } catch (err) {
    throw new CliError(`Could not read ${filePath}: ${err instanceof Error ? err.message : String(err)}`, ExitCode.UsageError);
  }
  // Matches csvImportSchema's own ceiling in routes/sources.ts — caught here
  // so a 20MB upload fails instantly rather than after the transfer.
  if (fileContent.length > 20 * 1024 * 1024) {
    throw new CliError(`${filePath} is larger than the 20MB import limit.`, ExitCode.UsageError);
  }

  const since = new Date();
  await client.post(`/sources/${opts.source}/csv-import`, { fileContent });
  note(`${style.dim("→")} uploaded ${filePath} (${Math.round(fileContent.length / 1024)}KB)`);

  if (!opts.wait) {
    note(style.dim("Queued. Pass --wait to block until the import finishes."));
    return ExitCode.Ok;
  }

  const result = await waitForActivity(client, entity.id, since, {
    timeoutMs: opts.timeoutMinutes ? opts.timeoutMinutes * 60_000 : DEFAULT_WAIT_TIMEOUT_MS,
  });
  return reportWaitResult(result, "Import finished");
}

export async function holdings(
  client: ApiClient,
  opts: { entity?: string; allEntities?: boolean; json?: boolean },
): Promise<number> {
  const entities = await resolveEntityScope(client, opts);

  for (const entity of entities) {
    const rows = await client.get<import("@grubless/api-types").Holding[]>(`/entities/${entity.id}/holdings`);
    if (opts.json) {
      json({ entity: { id: entity.id, name: entity.name }, holdings: rows });
      continue;
    }
    if (entities.length > 1) out(style.bold(entity.name));
    // Spam is excluded from the default view for the same reason the web app
    // hides it — a wallet airdropped 200 phishing tokens shouldn't have its
    // real position buried. --json returns everything, unfiltered.
    const visible = rows.filter((h) => !h.isSpam);
    table(visible, [
      { header: "ASSET", value: (h) => h.symbol },
      { header: "CHAIN", value: (h) => h.chain ?? "—" },
      { header: "QUANTITY", value: (h) => qty(h.quantity), align: "right" as const },
      { header: "VALUE", value: (h) => money(h.value), align: "right" as const },
      // The reconciliation signal: a reported-vs-calculated mismatch is the
      // thing that means "this figure may be wrong", and it's the product's
      // whole differentiator. It does not belong hidden behind --json.
      { header: "", value: (h) => (h.hasMismatch ? style.yellow("mismatch") : "") },
    ]);
    const mismatched = visible.filter((h) => h.hasMismatch).length;
    if (mismatched > 0) {
      note(style.yellow(`! ${mismatched} asset(s) disagree with the source's reported balance.`));
    }
    if (entities.length > 1) out();
  }
  return ExitCode.Ok;
}
