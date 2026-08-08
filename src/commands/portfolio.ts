import type { Entity, EntityTaxSettings, PortfolioHistoryPoint } from "@grubless/api-types";
import type { ApiClient } from "../client.js";
import { CliError, ExitCode, json, money, out, signed, style, useColour } from "../output.js";
import { RANGES, isRangeKey, pointsInRange, type RangeKey } from "../range.js";
import { renderPortfolio, toChartPoints } from "../tui/chart.js";
import { resolveEntityScope } from "./entities.js";

/**
 * `grubless portfolio` — value over time, the same series the TUI's Portfolio
 * tab and the web dashboard draw.
 *
 * Two audiences, two shapes:
 *
 * - **A person** gets the chart, because a portfolio's shape is the question
 *   they're asking and 900 rows of daily values is not an answer. This is the
 *   one command in the CLI whose plain output is a picture rather than a
 *   table, and that is deliberate — printing every point by default would be
 *   hostile to the interactive case for no gain to the scripted one.
 * - **A script or an agent** passes `--json` and gets every daily point, with
 *   values as the server's exact decimal strings. Nothing is resampled there:
 *   the chart drops points to fit a terminal, and a consumer that can hold the
 *   whole series should never inherit a display compromise.
 *
 * `--range` narrows the window, with the same keys the web dashboard and the
 * TUI use. It applies to `--json` too: a range is a question about the data,
 * not a display setting, and an agent asking "how did this year go" should not
 * have to slice the array itself.
 *
 * The default here is `all`, unlike the two interactive surfaces, which open
 * on the current financial year. A command reading into a pipe should hand
 * over everything it has unless told otherwise; a screen has to choose a
 * window, and the useful one is the year being filed.
 */

const CHART_ROWS = 16;
const DEFAULT_WIDTH = 80;
/** Below this the plot is narrower than its own axis labels. */
const MIN_WIDTH = 40;

export async function portfolio(
  client: ApiClient,
  opts: { entity?: string; allEntities?: boolean; json?: boolean; range?: string },
): Promise<number> {
  const range = parseRange(opts.range);
  const entities = await resolveEntityScope(client, opts);
  const documents: Array<{
    entity: { id: string; name: string };
    range: RangeKey;
    points: PortfolioHistoryPoint[];
  }> = [];

  for (const [i, entity] of entities.entries()) {
    const [all, settings] = await Promise.all([
      client.get<PortfolioHistoryPoint[]>(`/entities/${entity.id}/portfolio-history`),
      // Only the "fy" range needs this, and an entity with no settings row
      // 404s — not worth failing the command over when every other range is
      // unaffected. See the fallback in pointsInRange's caller below.
      range === "fy"
        ? client.get<EntityTaxSettings>(`/entities/${entity.id}/tax-settings`).catch(() => null)
        : Promise.resolve(null),
    ]);
    const points = pointsInRange(all, range, settings?.financialYearStartMonth ?? 7);

    if (opts.json) {
      // The range is echoed back: a consumer given a filtered array with no
      // record of the filter cannot tell a quiet year from a narrow window.
      documents.push({ entity: { id: entity.id, name: entity.name }, range, points });
      continue;
    }

    if (i > 0) out();
    printChart(entity, points, { showName: entities.length > 1, hasAnyHistory: all.length > 0, range });
  }

  // Flag-keyed, matching `report` and `sources sync`: --all-entities is always
  // an array, --entity always one object, whatever the account happens to hold
  // today.
  if (opts.json) json(opts.allEntities ? documents : (documents[0] ?? null));
  return ExitCode.Ok;
}

function parseRange(value: string | undefined): RangeKey {
  if (!value) return "all";
  if (!isRangeKey(value)) {
    throw new CliError(
      `Unknown range "${value}".\nAvailable: ${RANGES.join(", ")}`,
      ExitCode.UsageError,
    );
  }
  return value;
}

function printChart(
  entity: Entity,
  points: PortfolioHistoryPoint[],
  opts: { showName: boolean; hasAnyHistory: boolean; range: RangeKey },
): void {
  if (opts.showName) out(style.bold(entity.name));

  const last = points[points.length - 1];
  if (!last) {
    // Two different facts, and conflating them sends someone to look for a
    // sync problem that isn't there: an entity with no history at all needs a
    // source connected, while one whose history simply predates the window
    // needs a wider --range.
    out(
      style.dim(
        opts.hasAnyHistory
          ? `No portfolio history in the last ${opts.range} — try --range all.`
          : "No portfolio history yet.",
      ),
    );
    return;
  }
  // The three figures the plot cannot carry, formatted from the exact decimal
  // strings — same header as the TUI's Portfolio tab.
  out(
    style.dim(
      `${last.date}   VALUE ${money(last.value)}   UNREALISED ${signed(last.unrealizedPL)}   INCOME ${money(last.cumulativeIncome)}`,
    ),
  );

  const width = Math.max(MIN_WIDTH, process.stdout.columns ?? DEFAULT_WIDTH);
  const lines = renderPortfolio(toChartPoints(points), { width, height: CHART_ROWS, colour: useColour });
  for (const line of lines) out(line.trimEnd());
}
