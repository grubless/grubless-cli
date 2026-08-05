import type { EntityActivity } from "@grubless/api-types";
import type { ApiClient } from "../client.js";
import { CliError, ExitCode, note, style } from "../output.js";

/**
 * Waits for queued work to finish by polling `/entities/:id/activity`.
 *
 * There is no SSE or websocket in the API, so polling is the mechanism. Two
 * things shape the defaults:
 *
 * - A full sync legitimately runs for **up to ~30 minutes** — that's why the
 *   worker sets `lockDuration: 30 * 60 * 1000`, after a real Kraken resync of
 *   17k+ transactions was throttled into a half-hour fetch. A default timeout
 *   that can't accommodate that would report a healthy sync as a failure.
 * - We poll, and never re-POST the trigger. `POST /sources/:id/sync` has a
 *   set-if-not-syncing claim on the server, so a retry is a no-op at best and
 *   confusing at worst. Retrying the *observation* is safe; retrying the
 *   *action* is not.
 */

const POLL_INTERVAL_MS = 2000;
export const DEFAULT_WAIT_TIMEOUT_MS = 35 * 60 * 1000;

const TERMINAL = new Set(["success", "degraded", "error"]);

export interface WaitResult {
  activity: EntityActivity[];
  failed: EntityActivity[];
  degraded: EntityActivity[];
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

/**
 * Blocks until every activity row started at or after `since` for this entity
 * has reached a terminal state.
 *
 * `since` is captured by the caller *before* it triggers the job, so a job
 * that finishes between the trigger and the first poll is still observed —
 * polling for "is anything running now" instead would race and report success
 * for work that never started.
 */
export async function waitForActivity(
  client: ApiClient,
  entityId: string,
  since: Date,
  opts: { timeoutMs?: number; quiet?: boolean } = {},
): Promise<WaitResult> {
  const timeoutMs = opts.timeoutMs ?? DEFAULT_WAIT_TIMEOUT_MS;
  const deadline = Date.now() + timeoutMs;
  let lastMessage = "";

  for (;;) {
    const all = await client.get<EntityActivity[]>(`/entities/${entityId}/activity`);
    const relevant = all.filter((a) => new Date(a.startedAt).getTime() >= since.getTime() - 1000);

    const running = relevant.filter((a) => !TERMINAL.has(a.status));

    if (!opts.quiet && running.length > 0) {
      // The worker updates this field as it goes ("Fetching page 12…",
      // "Recalculating tax lots…"), which is the only progress signal that
      // exists — surfacing it is the difference between a live command and
      // one that looks hung for twenty minutes.
      const message = running[0].message ?? running[0].jobType;
      if (message !== lastMessage) {
        note(style.dim(`  ${message}`));
        lastMessage = message;
      }
    }

    if (relevant.length > 0 && running.length === 0) {
      return {
        activity: relevant,
        failed: relevant.filter((a) => a.status === "error"),
        degraded: relevant.filter((a) => a.status === "degraded"),
      };
    }

    if (Date.now() > deadline) {
      throw new CliError(
        `Timed out after ${Math.round(timeoutMs / 60000)} minutes waiting for the job to finish.\n` +
          "The job is still running server-side — this only stopped watching it.\n" +
          "Check progress in the web app, or re-run with --timeout <minutes>.",
        ExitCode.Failure,
      );
    }

    await sleep(POLL_INTERVAL_MS);
  }
}

/**
 * Turns a wait result into an exit code and a summary line.
 *
 * `degraded` is reported but does NOT fail the command: it means the work
 * completed with a caveat (a price provider ran out of budget, say), and
 * treating that as a hard failure would make a nightly job flap over
 * something that isn't broken. `error` does fail — a partial import is the
 * dangerous case, because the figures look plausible but are computed from
 * incomplete data.
 */
export function reportWaitResult(result: WaitResult, label: string): number {
  if (result.failed.length > 0) {
    for (const a of result.failed) {
      note(style.red("✗") + ` ${a.source?.label ?? a.jobType}: ${a.error ?? a.message ?? "failed"}`);
    }
    return ExitCode.Failure;
  }
  for (const a of result.degraded) {
    note(style.yellow("!") + ` ${a.source?.label ?? a.jobType}: ${a.message ?? "completed with warnings"}`);
  }
  note(style.green("✓") + ` ${label}`);
  return ExitCode.Ok;
}
