#!/usr/bin/env node
/**
 * Runs the Node bundle and the Go binary side by side against a stub API and
 * diffs what a user (or a CI script) would see: stdout, stderr, exit code,
 * and the config file left on disk.
 *
 * The e2e suite checks each build against a real server; this checks the two
 * builds against *each other*, across failure modes a real server can't be
 * made to produce on demand (a 402, a proxy's HTML 502, a Zod error).
 *
 *     pnpm build && (cd go && go build -o bin/grubless ./cmd/grubless)
 *     node go/scripts/parity.mjs
 *
 * Every scenario runs with PATH emptied, so neither build can reach a real
 * keychain — `auth login` here must never write to the developer's own.
 */

import { createServer } from "node:http";
import { execFile } from "node:child_process";
import { mkdtempSync, readFileSync, existsSync, chmodSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..", "..");
const NODE_CLI = join(root, "dist", "index.js");
const GO_CLI = join(root, "go", "bin", "grubless");

const ENTITIES = [
  { id: "7f0c2a52-1d7e-4d0a-9d1b-3c1f2b8e9a01", name: "Acme Trading Pty Ltd", entityType: "company", role: "owner", createdAt: "2025-07-01T00:00:00.000Z", extraServerField: { nested: [1, 2] } },
  { id: "0b6e8a3c-55f1-4c2e-8f7a-9e2d1c4b6a02", name: "Société Générale <Test> & Co", entityType: "trust", role: "preparer", createdAt: "2025-07-02T00:00:00.000Z" },
];

/** The token picks the scenario, so one server serves every case. */
const SCENARIOS = {
  grb_ok: (res) => send(res, 200, ENTITIES),
  grb_empty: (res) => send(res, 200, []),
  grb_401: (res) => send(res, 401, { error: "Invalid or revoked token" }),
  grb_402: (res) => send(res, 402, { error: "Reports require a paid plan." }),
  grb_403ro: (res) => send(res, 403, { error: "This token is read-only", readOnlyToken: true }),
  grb_403: (res) => send(res, 403, { error: "Forbidden" }),
  grb_404: (res) => send(res, 404, { error: "Not found" }),
  grb_400: (res) => send(res, 400, { error: { formErrors: [], fieldErrors: { year: ["Required"], name: ["Too short"] } } }),
  grb_502: (res) => { res.writeHead(502, { "content-type": "text/html" }); res.end("<html><body>Bad Gateway</body></html>"); },
  grb_oldcli: (res) => { res.setHeader("x-grubless-min-cli-version", "99.0.0"); send(res, 200, ENTITIES); },
};

function send(res, status, body) {
  res.writeHead(status, { "content-type": "application/json" });
  res.end(JSON.stringify(body));
}

const server = createServer((req, res) => {
  const token = (req.headers.authorization ?? "").replace(/^Bearer /, "");
  const scenario = SCENARIOS[token] ?? SCENARIOS.grb_401;
  // whoami's second request: tokens list. Fails for most scenarios, which is
  // the path where whoami must still succeed.
  if (req.url === "/api-tokens" && token === "grb_ok") return send(res, 200, [{ id: "t1", name: "laptop", scope: "read", lastUsedAt: null, expiresAt: null, createdAt: "2025-07-01T00:00:00.000Z" }]);
  scenario(res);
});
await new Promise((r) => server.listen(0, "127.0.0.1", r));
const API = `http://127.0.0.1:${server.address().port}`;

/** [label, argv, env] — env.GRUBLESS_TOKEN picks the scenario. */
const CASES = [
  ["version", ["--version"], {}],
  ["version short", ["-v"], {}],
  ["help", ["--help"], {}],
  ["no command, stdin not a tty", [], {}],
  ["unknown command, signed in", ["nonsense"], { GRUBLESS_TOKEN: "grb_ok" }],
  ["unknown command, signed out", ["nonsense"], {}],
  ["unknown flag", ["entities", "list", "--nope"], { GRUBLESS_TOKEN: "grb_ok" }],
  ["flag missing its value", ["entities", "list", "--entity"], { GRUBLESS_TOKEN: "grb_ok" }],
  ["flag value looks like a flag", ["entities", "list", "--entity", "--json"], { GRUBLESS_TOKEN: "grb_ok" }],
  ["boolean given a value", ["entities", "list", "--json=yes"], { GRUBLESS_TOKEN: "grb_ok" }],
  ["bad --timeout", ["entities", "list", "--timeout", "soon"], { GRUBLESS_TOKEN: "grb_ok" }],
  ["NaN --timeout", ["entities", "list", "--timeout", "NaN"], { GRUBLESS_TOKEN: "grb_ok" }],
  ["no credential", ["entities", "list"], {}],
  ["whitespace credential", ["entities", "list"], { GRUBLESS_TOKEN: "   " }],
  ["entities list", ["entities", "list"], { GRUBLESS_TOKEN: "grb_ok" }],
  ["entities (no sub)", ["entities"], { GRUBLESS_TOKEN: "grb_ok" }],
  ["entities list --json", ["entities", "list", "--json"], { GRUBLESS_TOKEN: "grb_ok" }],
  ["--json before the command", ["--json", "entities", "list"], { GRUBLESS_TOKEN: "grb_ok" }],
  ["--api-url=form", ["entities", "list", `--api-url=${API}`], { GRUBLESS_TOKEN: "grb_ok" }],
  ["entities unknown sub", ["entities", "delete"], { GRUBLESS_TOKEN: "grb_ok" }],
  ["empty list", ["entities", "list"], { GRUBLESS_TOKEN: "grb_empty" }],
  ["empty list --json", ["entities", "list", "--json"], { GRUBLESS_TOKEN: "grb_empty" }],
  ["401", ["entities", "list"], { GRUBLESS_TOKEN: "grb_401" }],
  ["402", ["entities", "list"], { GRUBLESS_TOKEN: "grb_402" }],
  ["403 read-only token", ["entities", "list"], { GRUBLESS_TOKEN: "grb_403ro" }],
  ["403 role", ["entities", "list"], { GRUBLESS_TOKEN: "grb_403" }],
  ["404", ["entities", "list"], { GRUBLESS_TOKEN: "grb_404" }],
  ["400 zod", ["entities", "list"], { GRUBLESS_TOKEN: "grb_400" }],
  ["502 html", ["entities", "list"], { GRUBLESS_TOKEN: "grb_502" }],
  ["server requires newer cli", ["entities", "list"], { GRUBLESS_TOKEN: "grb_oldcli" }],
  ["whoami", ["auth", "whoami"], { GRUBLESS_TOKEN: "grb_ok" }],
  ["whoami --json", ["auth", "whoami", "--json"], { GRUBLESS_TOKEN: "grb_ok" }],
  ["whoami, token listing fails", ["auth", "whoami", "--json"], { GRUBLESS_TOKEN: "grb_empty" }],
  ["auth unknown sub", ["auth", "frobnicate"], { GRUBLESS_TOKEN: "grb_ok" }],
  ["login without a tty", ["auth", "login"], {}],
  ["login with a rejected token", ["auth", "login", "--token", "grb_401"], {}],
  ["unreachable host", ["entities", "list", "--api-url", "http://127.0.0.1:1"], { GRUBLESS_TOKEN: "grb_ok" }],
];

/**
 * Stateful: login writes the config file, the next commands read it, logout
 * clears it. Run as one sequence per build in its own XDG dir.
 */
const SEQUENCE = [
  ["login", ["auth", "login", "--token", "grb_ok"]],
  ["entities list from the file", ["entities", "list"]],
  ["whoami from the file", ["auth", "whoami", "--json"]],
  ["world-readable config warns", ["entities", "list"], "chmod644"],
  ["logout", ["auth", "logout"]],
  ["signed out after logout", ["entities", "list"]],
];

function run(bin, args, env, xdg) {
  const isNode = bin.endsWith(".js");
  const [file, argv] = isNode ? [process.execPath, [bin, ...args]] : [bin, args];
  const needsApi = !args.some((a) => a.startsWith("--api-url"));
  return new Promise((resolve) => {
    execFile(
      file,
      needsApi ? [...argv, "--api-url", API] : argv,
      // stdin is /dev/null, never a TTY, so nothing ever waits on a prompt.
      { env: { PATH: "", HOME: xdg, XDG_CONFIG_HOME: xdg, NO_COLOR: "1", ...env } },
      (err, stdout, stderr) => resolve({ code: err ? (err.code ?? 1) : 0, stdout, stderr }),
    ).stdin.end();
  });
}

/** Known, accepted differences: normalised away and listed in the report. */
function normalise(text) {
  return text
    // Node says "fetch failed"; Go names the syscall. Both name the host,
    // which is the part the message exists for.
    .replace(/(Could not reach \S+): .*/g, "$1: <transport error>")
    .replaceAll(API, "<api>");
}

let failures = 0;
function compare(label, a, b) {
  const diffs = [];
  if (a.code !== b.code) diffs.push(`exit: node ${a.code}, go ${b.code}`);
  for (const stream of ["stdout", "stderr"]) {
    if (normalise(a[stream]) !== normalise(b[stream])) {
      diffs.push(`${stream}:\n    node: ${JSON.stringify(normalise(a[stream]))}\n    go:   ${JSON.stringify(normalise(b[stream]))}`);
    }
  }
  if (diffs.length) {
    failures++;
    console.log(`✗ ${label}\n  ${diffs.join("\n  ")}`);
  } else {
    console.log(`✓ ${label}  (exit ${a.code})`);
  }
}

for (const [label, args, env] of CASES) {
  const dirs = [mkdtempSync(join(tmpdir(), "parity-")), mkdtempSync(join(tmpdir(), "parity-"))];
  const [a, b] = await Promise.all([run(NODE_CLI, args, env, dirs[0]), run(GO_CLI, args, env, dirs[1])]);
  compare(label, a, b);
  dirs.forEach((d) => rmSync(d, { recursive: true, force: true }));
}

const seqDirs = { node: mkdtempSync(join(tmpdir(), "parity-")), go: mkdtempSync(join(tmpdir(), "parity-")) };
for (const [label, args, action] of SEQUENCE) {
  for (const dir of Object.values(seqDirs)) {
    const file = join(dir, "grubless", "config.json");
    if (action === "chmod644" && existsSync(file)) chmodSync(file, 0o644);
  }
  const a = await run(NODE_CLI, args, {}, seqDirs.node);
  const b = await run(GO_CLI, args, {}, seqDirs.go);
  // The paths differ only by temp dir; the file's contents must not.
  a.stderr = a.stderr.replaceAll(seqDirs.node, "<xdg>");
  b.stderr = b.stderr.replaceAll(seqDirs.go, "<xdg>");
  compare(`[sequence] ${label}`, a, b);

  const read = (d) => (existsSync(join(d, "grubless", "config.json")) ? readFileSync(join(d, "grubless", "config.json"), "utf8") : "<none>");
  const [fa, fb] = [read(seqDirs.node).replaceAll(API, "<api>"), read(seqDirs.go).replaceAll(API, "<api>")];
  if (fa !== fb) {
    failures++;
    console.log(`✗ [sequence] ${label}: config file differs\n    node: ${JSON.stringify(fa)}\n    go:   ${JSON.stringify(fb)}`);
  }
}
Object.values(seqDirs).forEach((d) => rmSync(d, { recursive: true, force: true }));

server.close();
console.log(`\n${failures === 0 ? "parity: identical" : `parity: ${failures} difference(s)`} across ${CASES.length + SEQUENCE.length} scenarios`);
process.exitCode = failures === 0 ? 0 : 1;
