import type { Entity } from "@grubless/api-types";
import type { ApiClient } from "../client.js";
import { CliError, ExitCode, json, table } from "../output.js";

export async function entitiesList(client: ApiClient, asJson: boolean): Promise<number> {
  const entities = await client.get<Entity[]>("/entities");
  if (asJson) {
    json(entities);
    return ExitCode.Ok;
  }
  if (entities.length === 0) {
    // stderr, not stdout: an empty list piped into jq should be empty, not
    // carry a sentence of prose.
    process.stderr.write("No entities.\n");
    return ExitCode.Ok;
  }
  table(entities, [
    { header: "ID", value: (e) => e.id },
    { header: "NAME", value: (e) => e.name },
    { header: "TYPE", value: (e) => e.entityType },
    { header: "ROLE", value: (e) => e.role },
  ]);
  return ExitCode.Ok;
}

/**
 * Turns whatever the user typed into an entity id.
 *
 * Accepts a uuid, an exact name, or an unambiguous case-insensitive prefix —
 * because nobody wants to paste a uuid to pull one report, and an accountant
 * with 40 client entities least of all. Ambiguity is an error rather than a
 * "first match wins" guess: silently picking the wrong client's entity would
 * produce a confident, correct-looking, completely wrong tax report.
 */
export async function resolveEntity(client: ApiClient, needle: string): Promise<Entity> {
  const entities = await client.get<Entity[]>("/entities");

  const byId = entities.find((e) => e.id === needle);
  if (byId) return byId;

  const lower = needle.toLowerCase();
  const exact = entities.filter((e) => e.name.toLowerCase() === lower);
  if (exact.length === 1) return exact[0];
  if (exact.length > 1) throw ambiguous(needle, exact);

  const prefix = entities.filter((e) => e.name.toLowerCase().startsWith(lower));
  if (prefix.length === 1) return prefix[0];
  if (prefix.length > 1) throw ambiguous(needle, prefix);

  throw new CliError(
    `No entity matching "${needle}".\nRun \`grubless entities list\` to see what this account can reach.`,
    ExitCode.UsageError,
  );
}

function ambiguous(needle: string, matches: Entity[]): CliError {
  const lines = matches.map((e) => `  ${e.id}  ${e.name}`).join("\n");
  return new CliError(`"${needle}" matches ${matches.length} entities:\n${lines}\nUse the id.`, ExitCode.UsageError);
}

/**
 * The entity set a command should act on: one resolved entity, or all of
 * them for `--all-entities`. The bulk form is the whole reason the CLI exists
 * for the accountant channel (docs/plan-cli.md §1).
 */
export async function resolveEntityScope(
  client: ApiClient,
  opts: { entity?: string; allEntities?: boolean },
): Promise<Entity[]> {
  if (opts.allEntities) {
    const entities = await client.get<Entity[]>("/entities");
    if (entities.length === 0) throw new CliError("This account has no entities.", ExitCode.Failure);
    return entities;
  }
  if (!opts.entity) {
    throw new CliError("Specify --entity <id|name>, or --all-entities.", ExitCode.UsageError);
  }
  return [await resolveEntity(client, opts.entity)];
}
