import type { Entity, PortfolioHistoryPoint } from "@grubless/api-types";
import type { ApiClient } from "../client.js";
import { ExitCode, json, money, out, signed, style, useColour } from "../output.js";
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
 */

const CHART_ROWS = 16;
const DEFAULT_WIDTH = 80;
/** Below this the plot is narrower than its own axis labels. */
const MIN_WIDTH = 40;

export async function portfolio(
  client: ApiClient,
  opts: { entity?: string; allEntities?: boolean; json?: boolean },
): Promise<number> {
  const entities = await resolveEntityScope(client, opts);
  const documents: Array<{ entity: { id: string; name: string }; points: PortfolioHistoryPoint[] }> = [];

  for (const [i, entity] of entities.entries()) {
    const points = await client.get<PortfolioHistoryPoint[]>(`/entities/${entity.id}/portfolio-history`);

    if (opts.json) {
      documents.push({ entity: { id: entity.id, name: entity.name }, points });
      continue;
    }

    if (i > 0) out();
    printChart(entity, points, entities.length > 1);
  }

  // Flag-keyed, matching `report` and `sources sync`: --all-entities is always
  // an array, --entity always one object, whatever the account happens to hold
  // today.
  if (opts.json) json(opts.allEntities ? documents : (documents[0] ?? null));
  return ExitCode.Ok;
}

function printChart(entity: Entity, points: PortfolioHistoryPoint[], showName: boolean): void {
  if (showName) out(style.bold(entity.name));

  const last = points[points.length - 1];
  if (last) {
    // The three figures the plot cannot carry, formatted from the exact
    // decimal strings — same header as the TUI's Portfolio tab.
    out(
      style.dim(
        `${last.date}   VALUE ${money(last.value)}   UNREALISED ${signed(last.unrealizedPL)}   INCOME ${money(last.cumulativeIncome)}`,
      ),
    );
  }

  const width = Math.max(MIN_WIDTH, process.stdout.columns ?? DEFAULT_WIDTH);
  const lines = renderPortfolio(toChartPoints(points), { width, height: CHART_ROWS, colour: useColour });
  for (const line of lines) out(line.trimEnd());
}
