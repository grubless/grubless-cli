import type { TaxYearSummary } from "../api-types.js";
import type { ApiClient } from "../client.js";
import { ExitCode, json, money, note, out, style, table } from "../output.js";
import { resolveEntityScope } from "./entities.js";

export async function taxSummary(
  client: ApiClient,
  opts: { entity?: string; allEntities?: boolean; year?: string; json?: boolean },
): Promise<number> {
  const entities = await resolveEntityScope(client, opts);

  for (const entity of entities) {
    const all = await client.get<TaxYearSummary[]>(`/entities/${entity.id}/tax-summary`);
    // `startYear` is the stable identifier — `financialYear` is a display
    // label containing an en dash, which nobody is going to type correctly.
    const rows = opts.year ? all.filter((s) => String(s.startYear) === opts.year) : all;

    if (opts.json) {
      json({ entity: { id: entity.id, name: entity.name }, summaries: rows });
      continue;
    }

    if (entities.length > 1) out(style.bold(`${entity.name} (${entity.entityType})`));
    if (rows.length === 0) {
      note(opts.year ? `No tax summary for ${opts.year}.` : "No tax summary yet.");
      continue;
    }

    table(rows, [
      { header: "FY", value: (s) => s.financialYear },
      { header: s(rows).incomeLabel.toUpperCase(), value: (r) => money(r.income), align: "right" as const },
      { header: "EXPENSES", value: (r) => money(r.expenses), align: "right" as const },
      { header: "NET CGT", value: (r) => money(r.netCapitalGainLoss), align: "right" as const },
      { header: "TAXABLE", value: (r) => money(r.taxableAmount), align: "right" as const },
      {
        // A pass-through entity (trust, partnership) has no entity-level tax
        // — printing "0.00" would read as "nothing owed" rather than "this
        // isn't computed here", which is a materially different statement to
        // put in front of someone preparing a return.
        header: "TAX",
        value: (r) => (r.applicable ? money(r.taxPayable) : "n/a (pass-through)"),
        align: "right" as const,
      },
      { header: "CCY", value: (r) => r.currency.toUpperCase() },
    ]);

    const carried = rows.filter((r) => r.carriedForwardLoss !== "0" && Number(r.carriedForwardLoss) !== 0);
    for (const r of carried) {
      note(style.dim(`  ${r.financialYear}: ${money(r.carriedForwardLoss)} capital loss carried forward`));
    }
    if (entities.length > 1) out();
  }
  return ExitCode.Ok;
}

/** First row, for a header label that varies by entity structure. */
function s(rows: TaxYearSummary[]): TaxYearSummary {
  return rows[0];
}
