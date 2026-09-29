import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { existsSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { CLI, cliCommand } from "./cli-bin.js";
import { describe, it, expect, beforeAll, afterAll } from "vitest";

/**
 * The `x-grubless-min-cli-version` handshake, driven against a stub server.
 *
 * This drives the **built bundle** like `e2e.test.ts` does, but needs no
 * Grubless deployment: what's under test is the CLI's reaction to a header,
 * not a server's ability to set one. A real API reads its floor from
 * `MIN_CLI_VERSION` once at startup, so it cannot vary the header per request
 * anyway — a stub is not a compromise here, it's the only way to test the
 * interesting value.
 *
 * Kept out of `e2e.test.ts` deliberately. That file skips wholesale without a
 * server, and this case would have been skipped along with it for no reason —
 * losing the one piece of version-skew behaviour that a fresh clone can check
 * on its own.
 */

const execFileAsync = promisify(execFile);
const here = dirname(fileURLToPath(import.meta.url));

/** Any well-formed token: the stub never looks at it. */
const TOKEN = "grb_stub_token_not_verified_by_the_stub_server";

describe("version handshake", () => {
  let stub: import("node:http").Server;
  let stubUrl: string;

  beforeAll(async () => {
    if (!existsSync(CLI)) {
      throw new Error(`${CLI} not found — run \`pnpm build\` before the tests.`);
    }
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
      ...cliCommand(["entities", "list", "--json", "--api-url", stubUrl]),
      {
        env: {
          ...process.env,
          GRUBLESS_TOKEN: TOKEN,
          // Hermetic: without this the CLI reads the developer's own
          // ~/.config/grubless, and a stored api-url could win over the flag.
          XDG_CONFIG_HOME: join(here, "..", "..", ".test-config"),
          NO_COLOR: "1",
        },
      },
    );
    expect(stderr).toContain("99.0.0");
    expect(stderr).toContain("npm install -g @grubless/cli");
    // Advisory, not fatal: a raised floor must not break a firm's nightly job
    // at 2am. The command still ran and still produced its data.
    expect(JSON.parse(stdout)).toEqual([]);
  });
});
