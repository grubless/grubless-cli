import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { randomBytes } from "node:crypto";
import { existsSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { CLI, cliCommand } from "./cli-bin.js";
import { describe as vitestDescribe, it, expect, beforeAll } from "vitest";

/**
 * End-to-end tests for the CLI.
 *
 * These drive the **built `dist/index.js`** as a real subprocess against a
 * running Grubless API — not the module functions in-process. That's the
 * point: the thing users install is the bundle, and the failure modes worth
 * catching (a bad shebang, an exit code that never propagates, a wire shape
 * that moved) only exist in the artifact talking to a real server. Testing the
 * source would prove none of them.
 *
 * ## Why this tier needs a server, and why that is the honest design
 *
 * Everything else in `src/test/` is pure and runs anywhere. This file cannot
 * be, because what it exists to verify is the HTTP conversation. When this
 * lived in the Grubless monorepo it booted the API in-process against a
 * scratch database; standalone, it instead points at whatever API you give it
 * and uses **only public routes** to set itself up — register, create an
 * entity, mint tokens, add a source. No database access, no server internals.
 *
 * That constraint turned out to be a feature. A CLI is separately versioned
 * from the deployment it talks to, so the only meaningful compatibility
 * question is "does this binary work against *that* server" — which is now
 * exactly what this asks, and it can be pointed at a local stack, a staging
 * deploy, or production with a throwaway account.
 *
 * ## Running it
 *
 *     pnpm build
 *     GRUBLESS_E2E_API_URL=http://localhost:3000 pnpm test
 *
 * Skipped, loudly, when that variable is unset — so `pnpm test` on a fresh
 * clone passes without anyone standing up a stack, and nobody mistakes a
 * skipped tier for a passing one. See README.md § Tests.
 *
 * ## What it leaves behind
 *
 * A registered user and one entity per run, in whatever database it was
 * pointed at. There is no account-deletion route to clean up with, and adding
 * one just to serve a test would be the tail wagging the dog. Point this at a
 * disposable stack.
 */

const execFileAsync = promisify(execFile);
const here = dirname(fileURLToPath(import.meta.url));

const API_URL = process.env.GRUBLESS_E2E_API_URL;

/**
 * Every suite in this file needs a server, so the gate lives in one place
 * rather than on each `describe`. Shadowing the import means a suite added
 * later is covered without anyone remembering to guard it.
 */
const describe = API_URL ? vitestDescribe : vitestDescribe.skip;

if (!API_URL) {
  // Printed, not silent: a skipped tier that looks like a passing one is how
  // a suite stops being trusted.
  console.warn(
    "\n  e2e: GRUBLESS_E2E_API_URL is unset — skipping the tests that drive the built binary\n" +
      "      against a real API. Run them with:\n" +
      "        pnpm build && GRUBLESS_E2E_API_URL=http://localhost:3000 pnpm test\n",
  );
}

let baseUrl: string;
let token: string;
let entityId: string;

interface RunResult {
  code: number;
  stdout: string;
  stderr: string;
}

/** Runs the built CLI with a token, capturing output and the real exit code. */
async function run(args: string[], env: Record<string, string> = {}): Promise<RunResult> {
  try {
    const { stdout, stderr } = await execFileAsync(...cliCommand([...args, "--api-url", baseUrl]), {
      env: {
        ...process.env,
        GRUBLESS_TOKEN: token,
        // Keeps the run hermetic — without this the CLI would read (and a
        // failing test could write) the developer's own ~/.config/grubless.
        XDG_CONFIG_HOME: join(here, "..", "..", ".test-config"),
        NO_COLOR: "1",
        ...env,
      },
    });
    return { code: 0, stdout, stderr };
  } catch (err) {
    const e = err as { code?: number; stdout?: string; stderr?: string };
    return { code: e.code ?? 1, stdout: e.stdout ?? "", stderr: e.stderr ?? "" };
  }
}

/** Session cookie for the throwaway account, used only during setup. */
let cookie: string;
/** A read-scoped token, minted alongside the write one. */
let readToken: string;

/**
 * POSTs as the throwaway user and fails with the server's own message.
 *
 * Setup failures are otherwise invisible — an entity that silently wasn't
 * created shows up thirty tests later as "No entity matching", which sends you
 * looking at argument parsing.
 */
async function api<T>(path: string, body: unknown, extraHeaders: Record<string, string> = {}): Promise<T> {
  const res = await fetch(`${baseUrl}${path}`, {
    method: "POST",
    headers: { "content-type": "application/json", ...extraHeaders },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    const detail = await res.text();
    // 429 is the one worth naming: auth routes are rate-limited per IP, so a
    // few runs in quick succession exhaust the register budget and every test
    // fails for a reason that has nothing to do with the CLI.
    const hint =
      res.status === 429
        ? " — auth rate limit hit; wait, or set AUTH_RATE_LIMIT_DISABLED=1 on the API"
        : "";
    throw new Error(`e2e setup: POST ${path} → ${res.status}${hint}\n${detail}`);
  }
  return (await res.json()) as T;
}

beforeAll(async () => {
  if (!API_URL) return;
  if (!existsSync(CLI)) {
    throw new Error(`${CLI} not found — run \`pnpm build\` before the tests.`);
  }
  baseUrl = API_URL.replace(/\/$/, "");

  // A fresh account per run, so the assertions below can assume an empty
  // world without the suite needing to own the database. Tenant isolation is
  // what makes that safe: this user sees its own entity and nothing else, on
  // a stack that may well have others.
  const email = `cli-e2e-${randomBytes(6).toString("hex")}@example.com`;
  const register = await fetch(`${baseUrl}/auth/register`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ email, password: "test-password-12345" }),
  });
  if (!register.ok) {
    throw new Error(`e2e setup: register → ${register.status}\n${await register.text()}`);
  }
  cookie = (register.headers.get("set-cookie") ?? "").split(";")[0];

  const entity = await api<{ id: string }>(
    "/entities",
    { name: "Acme Trading Pty Ltd", entityType: "company", jurisdictionCode: "AU_ATO" },
    { cookie },
  );
  entityId = entity.id;

  // Minted through the real route rather than inserted — the standalone repo
  // has no database access, and going through /api-tokens exercises the same
  // path a user does.
  token = (await api<{ token: string }>("/api-tokens", { name: "cli e2e", scope: "write" }, { cookie })).token;
  readToken = (await api<{ token: string }>("/api-tokens", { name: "cli e2e ro", scope: "read" }, { cookie })).token;

  // No source is created here on purpose. Several assertions below depend on
  // the entity having none — "sources sync --all" reporting `skipped` is one
  // of them — so the one source this file needs is created by the last
  // describe, after those have run.
}, 120_000);

describe("the shipped bundle", () => {
  it("runs and reports its version", async () => {
    const res = await run(["--version"]);
    expect(res.code).toBe(0);
    expect(res.stdout.trim()).toMatch(/^\d+\.\d+\.\d+$/);
  });

  it("prints usage to stdout for --help", async () => {
    const res = await run(["--help"]);
    expect(res.code).toBe(0);
    expect(res.stdout).toContain("USAGE");
    expect(res.stdout).toContain("--fail-on-blocking");
  });

  it("exits 2 on an unknown command", async () => {
    const res = await run(["nonsense"]);
    expect(res.code).toBe(2);
  });

  it("exits 2 on an unknown flag", async () => {
    const res = await run(["entities", "list", "--nope"]);
    expect(res.code).toBe(2);
  });
});

describe("auth", () => {
  it("exits 3 with no credential at all", async () => {
    const res = await run(["entities", "list"], { GRUBLESS_TOKEN: "" });
    expect(res.code).toBe(3);
    expect(res.stderr).toContain("Not signed in");
  });

  it("exits 3 on a rejected token, and says how to fix it", async () => {
    const res = await run(["entities", "list"], { GRUBLESS_TOKEN: "grb_wrong" });
    expect(res.code).toBe(3);
    expect(res.stderr).toContain("auth login");
  });
});

describe("entities", () => {
  it("lists them as a table on stdout", async () => {
    const res = await run(["entities", "list"]);
    expect(res.code).toBe(0);
    expect(res.stdout).toContain("Acme Trading Pty Ltd");
    expect(res.stdout).toContain("company");
  });

  it("emits parseable JSON with --json, and nothing else on stdout", async () => {
    const res = await run(["entities", "list", "--json"]);
    expect(res.code).toBe(0);
    const parsed = JSON.parse(res.stdout);
    expect(parsed).toHaveLength(1);
    expect(parsed[0].name).toBe("Acme Trading Pty Ltd");
  });

  it("resolves an entity by name prefix, not just uuid", async () => {
    const res = await run(["holdings", "--entity", "Acme"]);
    expect(res.code).toBe(0);
  });

  it("refuses an unknown entity with a usage error", async () => {
    const res = await run(["holdings", "--entity", "Nonexistent Ltd"]);
    expect(res.code).toBe(2);
    expect(res.stderr).toContain("No entity matching");
  });

  it("requires --entity or --all-entities", async () => {
    const res = await run(["holdings"]);
    expect(res.code).toBe(2);
    expect(res.stderr).toContain("--all-entities");
  });
});

describe("warnings", () => {
  it("reports a clean entity and exits 0 under --fail-on-blocking", async () => {
    // A brand-new entity has no data, so nothing can be blocking. The gate
    // must stay quiet here — a CI check that fails on an empty entity would
    // be untrustworthy from day one.
    const res = await run(["warnings", "--entity", entityId, "--fail-on-blocking"]);
    expect(res.code).toBe(0);
    expect(res.stderr).toContain("No blocking issues");
  });

  it("returns every category under --json", async () => {
    const res = await run(["warnings", "--entity", entityId, "--json"]);
    expect(res.code).toBe(0);
    const parsed = JSON.parse(res.stdout);
    expect(parsed.blockingCount).toBe(0);
    expect(Object.keys(parsed.categories).sort()).toEqual([
      "unbalanced-transfers",
      "uncategorized-transfers",
      "unpriced-assets",
      "zero-cost",
    ]);
  });
});

describe("reports", () => {
  it("rejects an unknown report name and lists the real ones", async () => {
    const res = await run(["report", "made-up", "--entity", entityId, "--year", "2025"]);
    expect(res.code).toBe(2);
    expect(res.stderr).toContain("capital-gains");
  });

  it("requires --year", async () => {
    const res = await run(["report", "capital-gains", "--entity", entityId]);
    expect(res.code).toBe(2);
    expect(res.stderr).toContain("--year");
  });

  it("refuses --all-entities without --out or --json rather than interleaving CSVs", async () => {
    const res = await run(["report", "capital-gains", "--all-entities", "--year", "2025"]);
    expect(res.code).toBe(2);
    expect(res.stderr).toContain("--out");
    expect(res.stderr).toContain("--json");
  });

  it("streams a CSV to stdout", async () => {
    const res = await run(["report", "capital-gains", "--entity", entityId, "--year", "2025"]);
    expect(res.code).toBe(0);
    // Header row of a real (empty) capital-gains report.
    expect(res.stdout.length).toBeGreaterThan(0);
    expect(res.stdout.split("\n")[0]).toContain(",");
  });

  it("emits one JSON object for a single entity", async () => {
    const res = await run(["report", "capital-gains", "--entity", entityId, "--year", "2025", "--json"]);
    expect(res.code).toBe(0);
    const doc = JSON.parse(res.stdout);
    expect(doc.entity.id).toBe(entityId);
    expect(doc.report).toBe("capital-gains");
    expect(doc.year).toBe("2025");
    // Columns come from the report's own header row, so this proves the CSV
    // was actually parsed rather than passed through.
    expect(Array.isArray(doc.columns)).toBe(true);
    expect(doc.columns.length).toBeGreaterThan(0);
    expect(Array.isArray(doc.rows)).toBe(true);
  });

  it("emits an array for --all-entities, which CSV to stdout cannot do", async () => {
    const res = await run(["report", "capital-gains", "--all-entities", "--year", "2025", "--json"]);
    expect(res.code).toBe(0);
    const docs = JSON.parse(res.stdout);
    expect(Array.isArray(docs)).toBe(true);
    expect(docs[0].entity.id).toBe(entityId);
  });

  it("keeps the shape keyed on the flag, not the entity count", async () => {
    // A one-client firm and a forty-client firm must get the same shape from
    // the same flags, or a script breaks the day they sign their second.
    const single = await run(["report", "capital-gains", "--entity", entityId, "--year", "2025", "--json"]);
    const all = await run(["report", "capital-gains", "--all-entities", "--year", "2025", "--json"]);
    expect(Array.isArray(JSON.parse(single.stdout))).toBe(false);
    expect(Array.isArray(JSON.parse(all.stdout))).toBe(true);
  });

  it("emits nothing but JSON on stdout, so a pipe stays parseable", async () => {
    const res = await run(["report", "capital-gains", "--all-entities", "--year", "2025", "--json"]);
    expect(() => JSON.parse(res.stdout)).not.toThrow();
  });

  it("refuses --json for a report that is a PDF or a ZIP", async () => {
    for (const name of ["bundle", "ato-mytax", "division-70-trading-stock"]) {
      const res = await run(["report", name, "--entity", entityId, "--year", "2025", "--json"]);
      expect(res.code).toBe(2);
      expect(res.stderr).toContain("--out");
    }
  });

  it("refuses --json together with --out rather than guessing a layout", async () => {
    const res = await run(["report", "capital-gains", "--entity", entityId, "--year", "2025", "--json", "--out", "x.json"]);
    expect(res.code).toBe(2);
    expect(res.stderr).toContain("stdout");
  });
});

describe("portfolio", () => {
  it("prints a chart, with no escape codes when stdout is redirected", async () => {
    const res = await run(["portfolio", "--entity", entityId]);
    expect(res.code).toBe(0);
    // The scratch entity has no priced history, which is the empty case.
    expect(res.stdout).toContain("No portfolio history yet");
    // `run` captures a pipe, not a TTY — output.ts's second rule.
    expect(res.stdout).not.toMatch(/\u001b/);
  });

  it("emits the series as JSON, one object per entity flag", async () => {
    const single = await run(["portfolio", "--entity", entityId, "--json"]);
    expect(single.code).toBe(0);
    const doc = JSON.parse(single.stdout);
    expect(doc.entity.id).toBe(entityId);
    expect(Array.isArray(doc.points)).toBe(true);

    const all = await run(["portfolio", "--all-entities", "--json"]);
    expect(Array.isArray(JSON.parse(all.stdout))).toBe(true);
  });

  it("is listed in the help", async () => {
    const res = await run(["--help"]);
    expect(res.stdout).toContain("portfolio");
    expect(res.stdout).toContain("--range");
  });

  it("narrows the series with --range, and says which range it used", async () => {
    // Echoed back because a consumer handed a filtered array with no record of
    // the filter cannot tell a quiet year from a narrow window.
    const res = await run(["portfolio", "--entity", entityId, "--range", "1m", "--json"]);
    expect(res.code).toBe(0);
    expect(JSON.parse(res.stdout).range).toBe("1m");
  });

  it("defaults to the whole series on the command line", async () => {
    // Unlike the TUI and the web dashboard, which open on the current FY: a
    // command reading into a pipe hands over everything unless told otherwise.
    const res = await run(["portfolio", "--entity", entityId, "--json"]);
    expect(JSON.parse(res.stdout).range).toBe("all");
  });

  it("rejects an unknown range and lists the real ones", async () => {
    const res = await run(["portfolio", "--entity", entityId, "--range", "2w"]);
    expect(res.code).toBe(2);
    expect(res.stderr).toContain("24h");
  });
});

describe("sources sync --json", () => {
  it("emits a result document on stdout, keeping progress on stderr", async () => {
    // The entity has no sources, which is the "skipped" case — recorded
    // rather than omitted, so a caller iterating clients can tell "nothing to
    // sync" apart from "this entity never ran".
    const res = await run(["sources", "sync", "--entity", entityId, "--all", "--json"]);
    expect(res.code).toBe(0);
    const doc = JSON.parse(res.stdout);
    expect(doc.entity.id).toBe(entityId);
    expect(doc.status).toBe("skipped");
    expect(doc.queued).toEqual([]);
  });

  it("emits an array under --all-entities", async () => {
    const res = await run(["sources", "sync", "--all-entities", "--all", "--json"]);
    expect(res.code).toBe(0);
    expect(Array.isArray(JSON.parse(res.stdout))).toBe(true);
  });
});

describe("sources", () => {
  it("reports an empty source list without failing", async () => {
    const res = await run(["sources", "list", "--entity", entityId]);
    expect(res.code).toBe(0);
  });

  it("requires --source or --all to sync", async () => {
    const res = await run(["sources", "sync", "--entity", entityId]);
    expect(res.code).toBe(2);
    expect(res.stderr).toContain("--all");
  });

  it("validates --timeout is a positive number", async () => {
    const res = await run(["sources", "sync", "--entity", entityId, "--all", "--timeout", "abc"]);
    expect(res.code).toBe(2);
    expect(res.stderr).toContain("--timeout");
  });
});

describe("read-only token scope", () => {
  it("blocks a mutating command at the API, with a scope-specific message", async () => {
    // Created here rather than in beforeAll: an entity with a source would
    // invalidate the "nothing to sync" assertions above.
    const source = await api<{ id: string | number }>(
      `/entities/${entityId}/sources`,
      { adapterKey: "csv_generic", label: "Test CSV" },
      { cookie },
    );

    const res = await run(["sources", "sync", "--entity", entityId, "--source", String(source.id)], {
      GRUBLESS_TOKEN: readToken,
    });
    expect(res.code).toBe(3);
    expect(res.stderr).toContain("token-scope problem");
  });
});
