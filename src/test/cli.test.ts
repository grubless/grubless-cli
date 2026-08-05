import { execSync } from "node:child_process";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { createHash, randomBytes } from "node:crypto";
import { existsSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import postgres from "postgres";
import { describe, it, expect, beforeAll, afterAll } from "vitest";
import type { FastifyInstance } from "fastify";

/**
 * End-to-end tests for the CLI.
 *
 * These drive the **built `dist/index.js`** as a real subprocess against a
 * real API on a real port, backed by a scratch database — not the module
 * functions in-process. That's the point: the thing users install is the
 * bundle, and the failure modes worth catching (a bad shebang, an unbundled
 * workspace import, an exit code that never propagates) only exist in the
 * artifact. Testing the source would prove none of them.
 *
 * Never touches a real database: same scratch-DB discipline as
 * apps/api/src/test/harness.ts.
 */

const execFileAsync = promisify(execFile);
const here = dirname(fileURLToPath(import.meta.url));
const CLI = join(here, "..", "..", "dist", "index.js");

const ADMIN_URL = process.env.TEST_DATABASE_ADMIN_URL ?? "postgres://grubbertax:changeme@localhost:5432/postgres";

let app: FastifyInstance;
let baseUrl: string;
let dbName: string;
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
    const { stdout, stderr } = await execFileAsync(process.execPath, [CLI, ...args, "--api-url", baseUrl], {
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

beforeAll(async () => {
  if (!existsSync(CLI)) {
    throw new Error(`${CLI} not found — run \`pnpm --filter @grubless/cli build\` before the tests.`);
  }

  dbName = `grubless_cli_${process.pid}`;
  const admin = postgres(ADMIN_URL, { max: 1 });
  await admin.unsafe(`DROP DATABASE IF EXISTS "${dbName}"`);
  await admin.unsafe(`CREATE DATABASE "${dbName}"`);
  await admin.end();

  const base = ADMIN_URL.slice(0, ADMIN_URL.lastIndexOf("/"));
  const databaseUrl = `${base}/${dbName}`;

  execSync("pnpm --filter @grubless/db migrate", {
    env: { ...process.env, DATABASE_URL: databaseUrl },
    stdio: "pipe",
  });

  process.env.DATABASE_URL = databaseUrl;
  process.env.REDIS_URL = process.env.TEST_REDIS_URL ?? "redis://localhost:6379/15";
  process.env.AUTH_RATE_LIMIT_DISABLED = "1";

  const { buildServer } = await import("@grubless/api/src/server.js");
  app = await buildServer();
  app.log.level = "silent";
  // A real port: the CLI speaks HTTP, so app.inject() would bypass exactly
  // the layer under test.
  await app.listen({ host: "127.0.0.1", port: 0 });
  const address = app.server.address();
  if (!address || typeof address === "string") throw new Error("no port");
  baseUrl = `http://127.0.0.1:${address.port}`;

  // Register + create an entity through the real routes.
  const register = await fetch(`${baseUrl}/auth/register`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ email: "cli@example.com", password: "test-password-12345" }),
  });
  const cookie = (register.headers.get("set-cookie") ?? "").split(";")[0];
  const userId = ((await register.json()) as { id: string }).id;

  const entity = await fetch(`${baseUrl}/entities`, {
    method: "POST",
    headers: { "content-type": "application/json", cookie },
    body: JSON.stringify({ name: "Acme Trading Pty Ltd", entityType: "company", jurisdictionCode: "AU_ATO" }),
  });
  entityId = ((await entity.json()) as { id: string }).id;

  // Seed a write-scoped token directly — same hashing as auth/api-token.ts.
  token = "grb_" + randomBytes(32).toString("base64url");
  const sql = postgres(databaseUrl, { max: 1 });
  await sql`
    insert into api_tokens (user_id, token_hash, name, scope)
    values (${userId}, ${createHash("sha256").update(token).digest("hex")}, 'cli test', 'write')
  `;
  await sql.end();
}, 120_000);

afterAll(async () => {
  await app?.close();
  const admin = postgres(ADMIN_URL, { max: 1 });
  await admin.unsafe(`DROP DATABASE IF EXISTS "${dbName}" WITH (FORCE)`);
  await admin.end();
});

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

  it("refuses --all-entities without --out rather than interleaving CSVs", async () => {
    const res = await run(["report", "capital-gains", "--all-entities", "--year", "2025"]);
    expect(res.code).toBe(2);
    expect(res.stderr).toContain("--out");
  });

  it("streams a CSV to stdout", async () => {
    const res = await run(["report", "capital-gains", "--entity", entityId, "--year", "2025"]);
    expect(res.code).toBe(0);
    // Header row of a real (empty) capital-gains report.
    expect(res.stdout.length).toBeGreaterThan(0);
    expect(res.stdout.split("\n")[0]).toContain(",");
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

describe("version handshake", () => {
  /**
   * Driven against a stub rather than the real API, because the floor is read
   * from MIN_CLI_VERSION once at buildServer() time — changing it per-request
   * isn't possible, and standing up a second full API just to move one header
   * is disproportionate. What's under test is the CLI's reaction, not the
   * server's ability to set a header (verified by hand against the container).
   */
  let stub: import("node:http").Server;
  let stubUrl: string;

  beforeAll(async () => {
    const { createServer } = await import("node:http");
    stub = createServer((_req, res) => {
      res.setHeader("content-type", "application/json");
      res.setHeader("x-grubless-min-cli-version", "99.0.0");
      res.end("[]");
    });
    await new Promise<void>((resolve) => stub.listen(0, "127.0.0.1", resolve));
    const addr = stub.address();
    if (!addr || typeof addr === "string") throw new Error("no port");
    stubUrl = `http://127.0.0.1:${addr.port}`;
  });

  afterAll(async () => {
    await new Promise<void>((resolve) => stub.close(() => resolve()));
  });

  it("warns on stderr when the server requires a newer CLI, without failing", async () => {
    const { stdout, stderr } = await execFileAsync(
      process.execPath,
      [CLI, "entities", "list", "--json", "--api-url", stubUrl],
      { env: { ...process.env, GRUBLESS_TOKEN: token, NO_COLOR: "1" } },
    );
    expect(stderr).toContain("99.0.0");
    expect(stderr).toContain("npm install -g @grubless/cli");
    // Advisory, not fatal: a raised floor must not break a firm's nightly job
    // at 2am. The command still ran and still produced its data.
    expect(JSON.parse(stdout)).toEqual([]);
  });
});

describe("read-only token scope", () => {
  it("blocks a mutating command at the API, with a scope-specific message", async () => {
    const readToken = "grb_" + randomBytes(32).toString("base64url");
    const sql = postgres(process.env.DATABASE_URL!, { max: 1 });
    const [user] = await sql<{ id: string }[]>`select id from users limit 1`;
    await sql`
      insert into api_tokens (user_id, token_hash, name, scope)
      values (${user.id}, ${createHash("sha256").update(readToken).digest("hex")}, 'read only', 'read')
    `;

    const [source] = await sql`
      insert into sources (entity_id, source_type, adapter_key, label, config)
      values (${entityId}, 'csv_import', 'csv_generic', 'Test CSV', '{}'::jsonb)
      returning id
    `;
    await sql.end();

    const res = await run(["sources", "sync", "--entity", entityId, "--source", String(source.id)], {
      GRUBLESS_TOKEN: readToken,
    });
    expect(res.code).toBe(3);
    expect(res.stderr).toContain("token-scope problem");
  });
});
