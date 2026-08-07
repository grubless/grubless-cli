import { CliError, ExitCode } from "./output.js";

/**
 * The HTTP client. Everything the CLI knows about the server goes through
 * here, including how each failure mode is turned into a message a person can
 * act on.
 *
 * Design note: this is a *thin* client. It never computes a tax figure, never
 * re-derives a balance, never interprets a report — those come from the API as
 * finished values. See docs/plan-cli.md §3 for why that boundary is
 * load-bearing rather than stylistic.
 */

export const CLI_VERSION = "0.1.0";

/**
 * The server tells us the oldest CLI it still supports via this header. Sent
 * from v1 precisely so that it exists *before* anyone is pinned to an old
 * version — retrofitting a handshake onto a fleet of already-deployed CLIs is
 * the problem this avoids (docs/plan-cli.md §7).
 */
const MIN_VERSION_HEADER = "x-grubless-min-cli-version";

export interface ClientOptions {
  apiUrl: string;
  token: string | null;
}

export class ApiClient {
  private warnedAboutVersion = false;

  constructor(private readonly opts: ClientOptions) {}

  private headers(extra: Record<string, string> = {}): Record<string, string> {
    const headers: Record<string, string> = {
      // Identifies the client in server logs and drives the version
      // handshake below.
      "user-agent": `grubless-cli/${CLI_VERSION} (node ${process.versions.node})`,
      accept: "application/json",
      ...extra,
    };
    if (this.opts.token) headers.authorization = `Bearer ${this.opts.token}`;
    return headers;
  }

  private checkVersion(res: Response): void {
    if (this.warnedAboutVersion) return;
    const min = res.headers.get(MIN_VERSION_HEADER);
    if (!min) return;
    if (compareVersions(CLI_VERSION, min) < 0) {
      this.warnedAboutVersion = true;
      process.stderr.write(
        `warning: this CLI is ${CLI_VERSION}, but the server requires ${min} or newer.\n` +
          `         Update with: npm install -g @grubless/cli\n`,
      );
    }
  }

  /**
   * Turns a non-2xx response into a CliError carrying the *server's own*
   * message wherever one exists.
   *
   * The 402 case matters more than it looks. Every report endpoint is gated
   * (`requireReportAccess`), and with BILLING_ENABLED=1 in production a free
   * account hits it routinely. The API writes that message for a human, so
   * showing it verbatim is the difference between "upgrade to download
   * reports" and `request failed (402)`, which reads like a bug in the tool.
   */
  private async fail(res: Response, method: string, path: string): Promise<never> {
    const body = await res.text();
    let parsed: { error?: unknown; readOnlyToken?: boolean } = {};
    try {
      parsed = JSON.parse(body);
    } catch {
      // Non-JSON error body (a proxy's HTML 502, say) — fall through to the
      // raw text, truncated.
    }

    const serverMessage =
      typeof parsed.error === "string" ? parsed.error : body.trim().slice(0, 500) || res.statusText;

    if (res.status === 401) {
      throw new CliError(
        `${serverMessage}\nRun \`grubless auth login\` to authenticate, or set GRUBLESS_TOKEN.`,
        ExitCode.AuthFailure,
      );
    }
    if (res.status === 403 && parsed.readOnlyToken) {
      // Distinguished from a role-based 403 on purpose: the two have
      // completely different fixes, and conflating them produces bug reports
      // about the wrong one.
      throw new CliError(
        `${serverMessage}\nThis is a token-scope problem, not a permissions problem — create a write-scoped token.`,
        ExitCode.AuthFailure,
      );
    }
    if (res.status === 403) {
      throw new CliError(`${serverMessage}\nYour account's role on this entity doesn't allow that.`, ExitCode.Failure);
    }
    if (res.status === 402) {
      throw new CliError(`${serverMessage}\nUpgrade at https://grubless.io/app`, ExitCode.UpgradeRequired);
    }
    if (res.status === 404) {
      throw new CliError(serverMessage || `Not found: ${path}`, ExitCode.Failure);
    }
    if (typeof parsed.error === "object" && parsed.error !== null) {
      // Zod's flattened issue shape — readable enough raw, and inventing a
      // prettier rendering risks hiding a field.
      throw new CliError(`${method} ${path} rejected:\n${JSON.stringify(parsed.error, null, 2)}`, ExitCode.UsageError);
    }
    throw new CliError(`${method} ${path} failed (${res.status}): ${serverMessage}`, ExitCode.Failure);
  }

  private async send(method: string, path: string, init: RequestInit = {}): Promise<Response> {
    const url = `${this.opts.apiUrl.replace(/\/$/, "")}${path}`;
    let res: Response;
    try {
      res = await fetch(url, { ...init, method, headers: this.headers(init.headers as Record<string, string>) });
    } catch (err) {
      // A connection-level failure names the host, because the single most
      // common cause is pointing at the wrong one (a stale --api-url, or a
      // local dev server that isn't running).
      throw new CliError(
        `Could not reach ${this.opts.apiUrl}: ${err instanceof Error ? err.message : String(err)}`,
        ExitCode.Failure,
      );
    }
    this.checkVersion(res);
    if (!res.ok) await this.fail(res, method, path);
    return res;
  }

  async get<T>(path: string): Promise<T> {
    const res = await this.send("GET", path);
    return (await res.json()) as T;
  }

  async post<T>(path: string, body?: unknown): Promise<T> {
    const res = await this.send("POST", path, {
      body: body === undefined ? undefined : JSON.stringify(body),
      headers: body === undefined ? {} : { "content-type": "application/json" },
    });
    if (res.status === 204) return undefined as T;
    const text = await res.text();
    return (text ? JSON.parse(text) : undefined) as T;
  }

  /**
   * A text body, for the report endpoints under `--json` — the CSV has to be
   * whole before it can be re-framed, so there's nothing to stream.
   *
   * Buffering is fine here and not elsewhere: `--json` produces one JSON
   * document, which cannot be emitted incrementally anyway. `--out` keeps the
   * streaming path below, which is the one that matters for a large
   * transaction history.
   */
  async getText(path: string): Promise<string> {
    const res = await this.send("GET", path, { headers: { accept: "text/csv, */*" } });
    return res.text();
  }

  /**
   * Report endpoints return `text/csv` (or a ZIP for the bundle), not JSON.
   * Returned as a stream so `--out` can write straight to disk — the
   * complete-tax bundle is a ZIP built by `archiver`, and buffering an entity's
   * full transaction history in memory to then write it out is pointless work
   * on the one command most likely to be run over a large dataset.
   */
  async getStream(path: string): Promise<{ body: ReadableStream<Uint8Array>; filename: string | null }> {
    const res = await this.send("GET", path, { headers: { accept: "*/*" } });
    if (!res.body) throw new CliError(`No response body for ${path}`, ExitCode.Failure);
    return { body: res.body, filename: filenameFromDisposition(res.headers.get("content-disposition")) };
  }
}

/** Honours the server's own suggested filename rather than inventing one. */
function filenameFromDisposition(header: string | null): string | null {
  if (!header) return null;
  const match = /filename\*?=(?:UTF-8'')?"?([^";]+)"?/i.exec(header);
  return match ? decodeURIComponent(match[1]) : null;
}

/** Numeric-segment comparison; enough for the x.y.z the handshake uses. */
function compareVersions(a: string, b: string): number {
  const pa = a.split(".").map((n) => Number.parseInt(n, 10) || 0);
  const pb = b.split(".").map((n) => Number.parseInt(n, 10) || 0);
  for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
    const diff = (pa[i] ?? 0) - (pb[i] ?? 0);
    if (diff !== 0) return diff < 0 ? -1 : 1;
  }
  return 0;
}
