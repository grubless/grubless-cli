import type {
  UnbalancedTransferWarning,
  UncategorizedTransferWarning,
  UnpricedAssetWarning,
  ZeroCostWarning,
} from "../api-types.js";
import type { ApiClient } from "../client.js";
import { ExitCode, json, money, note, out, qty, style, table } from "../output.js";
import { resolveEntityScope } from "./entities.js";

/**
 * `grubless warnings` — the reconciliation surface, made scriptable.
 *
 * This is the command that turns the product's differentiator into something
 * a firm can enforce: with `--fail-on-blocking` it exits non-zero, so it works
 * as a **pre-filing gate** in CI. Nothing else in the category tells you
 * whether the data behind a return is actually complete
 * (PLAN-gtm.md §3.2).
 */

/**
 * Which categories block a filing, and which are advisory.
 *
 * Blocking = the reported figures are known to be wrong or unsubstantiated if
 * you file as-is:
 *  - zero-cost: a disposal with no substantiated cost basis, so the gain is
 *    computed against zero and OVER-reports tax owed.
 *  - uncategorized-transfers: a movement not yet classified, which may be an
 *    internal transfer being taxed as a disposal.
 *
 * Advisory = worth reviewing, but the figures aren't wrong because of it:
 *  - unpriced-assets: no price found, so it contributes nothing rather than
 *    something incorrect.
 *  - unbalanced-transfers: a per-source net imbalance, which is often a
 *    legitimately partial history rather than a defect.
 *
 * Deliberately a fixed split rather than a flag: the point of a gate is that
 * it means the same thing on every run, in every firm.
 */
const BLOCKING = ["zero-cost", "uncategorized-transfers"] as const;
const ADVISORY = ["unpriced-assets", "unbalanced-transfers"] as const;

type Category = (typeof BLOCKING)[number] | (typeof ADVISORY)[number];

const LABEL: Record<Category, string> = {
  "zero-cost": "Disposals with no cost basis",
  "uncategorized-transfers": "Uncategorised transfers",
  "unpriced-assets": "Assets with no price",
  "unbalanced-transfers": "Unbalanced transfers",
};

interface CategoryResult {
  category: Category;
  blocking: boolean;
  count: number;
  rows: unknown[];
}

export async function warnings(
  client: ApiClient,
  opts: { entity?: string; allEntities?: boolean; json?: boolean; failOnBlocking?: boolean },
): Promise<number> {
  const entities = await resolveEntityScope(client, opts);
  let blockingTotal = 0;
  const payload: unknown[] = [];

  for (const entity of entities) {
    const results: CategoryResult[] = [];

    for (const category of [...BLOCKING, ...ADVISORY] as Category[]) {
      const rows = await client.get<unknown[]>(`/entities/${entity.id}/warnings/${category}`);
      results.push({
        category,
        blocking: (BLOCKING as readonly string[]).includes(category),
        count: rows.length,
        rows,
      });
    }

    const entityBlocking = results.filter((r) => r.blocking).reduce((sum, r) => sum + r.count, 0);
    blockingTotal += entityBlocking;

    if (opts.json) {
      payload.push({
        entity: { id: entity.id, name: entity.name },
        blockingCount: entityBlocking,
        categories: Object.fromEntries(results.map((r) => [r.category, r.rows])),
      });
      continue;
    }

    if (entities.length > 1) out(style.bold(entity.name));
    renderEntity(results);
    if (entityBlocking === 0) {
      note(style.green("✓") + " No blocking issues.");
    } else {
      note(style.yellow("!") + ` ${entityBlocking} blocking issue(s) — review before filing.`);
    }
    if (entities.length > 1) out();
  }

  if (opts.json) json(entities.length === 1 ? payload[0] : payload);

  if (opts.failOnBlocking && blockingTotal > 0) return ExitCode.BlockingWarnings;
  return ExitCode.Ok;
}

function renderEntity(results: CategoryResult[]): void {
  table(results, [
    { header: "CATEGORY", value: (r) => LABEL[r.category] },
    { header: "COUNT", value: (r) => String(r.count), align: "right" as const },
    { header: "", value: (r) => (r.count === 0 ? "" : r.blocking ? style.yellow("blocking") : style.dim("advisory")) },
  ]);

  // A few concrete examples per non-empty blocking category. A bare count
  // tells someone there's a problem; it doesn't help them start fixing it,
  // and "go and look in the web app" is a poor answer from a tool they ran
  // to avoid doing exactly that.
  for (const result of results) {
    if (!result.blocking || result.count === 0) continue;
    out();
    out(style.dim(`${LABEL[result.category]} — first ${Math.min(5, result.count)} of ${result.count}:`));
    if (result.category === "zero-cost") {
      const rows = result.rows as ZeroCostWarning[];
      table(rows.slice(0, 5), [
        { header: "DATE", value: (r) => r.ts.slice(0, 10) },
        { header: "ASSET", value: (r) => r.assetSymbol },
        { header: "QUANTITY", value: (r) => qty(r.quantity), align: "right" as const },
        { header: "PROCEEDS", value: (r) => money(r.proceedsAmount), align: "right" as const },
        { header: "SOURCE", value: (r) => r.sourceLabel },
      ]);
    } else {
      const rows = result.rows as UncategorizedTransferWarning[];
      table(rows.slice(0, 5), [
        { header: "DATE", value: (r) => r.ts.slice(0, 10) },
        { header: "DIR", value: (r) => r.direction },
        { header: "ASSET", value: (r) => r.assetSymbol },
        { header: "AMOUNT", value: (r) => qty(r.amount), align: "right" as const },
      ]);
    }
  }
}

/** Kept exported so the shapes stay referenced and drift shows up at build time. */
export type AnyWarning =
  | ZeroCostWarning
  | UncategorizedTransferWarning
  | UnpricedAssetWarning
  | UnbalancedTransferWarning;
