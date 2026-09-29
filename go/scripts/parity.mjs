#!/usr/bin/env node
/**
 * Runs the Node bundle and the Go binary side by side against a stub API and
 * diffs everything a user or a CI script can observe: stdout, stderr, the
 * exit code, the config file left behind, and every file `--out` writes.
 *
 * The e2e suite checks each build against a real server; this checks the two
 * builds against *each other*, including the failure modes a real server
 * can't be made to produce on demand — a 402, a proxy's HTML 502, a report
 * with a malformed filename, one client's report failing mid-run.
 *
 *     pnpm build && (cd go && go build -o bin/grubless ./cmd/grubless)
 *     node go/scripts/parity.mjs [filter]
 *
 * Scenarios run one build at a time, with the stub's state reset in between:
 * `--wait` polls an activity feed that advances per request, and two builds
 * polling it at once would each see half of it. Every scenario runs with PATH
 * emptied, so neither build can reach a real keychain.
 */

import { createServer } from "node:http";
import { execFile } from "node:child_process";
import { mkdtempSync, readFileSync, readdirSync, statSync, existsSync, chmodSync, rmSync, writeFileSync, mkdirSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname, relative } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..", "..");
const NODE_CLI = join(root, "dist", "index.js");
const GO_CLI = join(root, "go", "bin", "grubless");
const FILTER = process.argv[2] ?? "";

// ---------- fixtures ----------

const ACME = "7f0c2a52-1d7e-4d0a-9d1b-3c1f2b8e9a01";
const SOC = "0b6e8a3c-55f1-4c2e-8f7a-9e2d1c4b6a02";
const ENTITIES = [
  { id: ACME, name: "Acme Trading Pty Ltd", entityType: "company", role: "owner", createdAt: "2025-07-01T00:00:00.000Z", extraServerField: { nested: [1, 2], "10": "int-like key" } },
  { id: SOC, name: "Société Générale <Test> & Co / 2025", entityType: "trust", role: "preparer", createdAt: "2025-07-02T00:00:00.000Z" },
];

const DOTS_ENTITY = { id: "d0d0d0d0-0000-4000-8000-000000000000", name: "..", entityType: "trust", role: "viewer", createdAt: "2025-07-03T00:00:00.000Z" };

const SOURCES = {
  [ACME]: [
    { id: "src-kraken", entityId: ACME, sourceType: "exchange_api", adapterKey: "kraken", label: "Kraken", config: {}, lastSyncedAt: "2026-08-01T10:00:00.000Z", syncStatus: "idle", syncError: null, syncEnabled: true, createdAt: "2025-07-01T00:00:00.000Z", transactionCount: 17342, lastTransactionAt: null },
    { id: "src-evm", entityId: ACME, sourceType: "on_chain", adapterKey: "evm", label: "evm-wallet-a (Ethereum)", config: { address: "0xabc" }, lastSyncedAt: null, syncStatus: "error", syncError: "RPC rate limited", syncEnabled: true, createdAt: "2025-07-01T00:00:00.000Z", transactionCount: 12, lastTransactionAt: null },
    { id: "src-csv", entityId: ACME, sourceType: "csv_import", adapterKey: "csv", label: "Old exchange CSV", config: {}, lastSyncedAt: "2025-01-01T00:00:00+10:00", syncStatus: "idle", syncError: null, syncEnabled: false, createdAt: "2025-07-01T00:00:00.000Z", transactionCount: 0, lastTransactionAt: null },
  ],
  [SOC]: [],
};

const HOLDINGS = [
  { assetId: "a1", symbol: "BTC", chain: "bitcoin", imageUrl: null, quantity: "1.234500000000000000", value: "154321.987654321", hasMismatch: false, isSpam: false, sources: [{ sourceId: "src-kraken", label: "Kraken", calculatedQuantity: "1.2345", reportedQuantity: "1.2345", mismatch: false }] },
  { assetId: "a2", symbol: "USDC", chain: null, imageUrl: null, quantity: "2500.5", value: "2500.50", hasMismatch: true, isSpam: false, sources: [{ label: "hyperliquid" }, { label: "Kraken" }] },
  { assetId: "a3", symbol: "0xccef6bdd7534f750eb1f494367493b5fd65c905d", chain: null, imageUrl: null, quantity: "2e-9", value: null, hasMismatch: false, isSpam: false, sources: [{ label: "evm-wallet-a (Ethereum)" }, { label: "evm-wallet-b (Ethereum)" }, { label: "evm-wallet-c (Ethereum)" }] },
  { assetId: "a4", symbol: "SCAM", chain: "ethereum", imageUrl: null, quantity: "1000000", value: "0", hasMismatch: false, isSpam: true, sources: [] },
];

const TAX = [
  { financialYear: "2024–25", startYear: 2024, startDate: "2024-07-01", endDate: "2025-06-30", currency: "aud", incomeLabel: "Assessable income", income: "1071559.886401792102100000", rewardIncome: "0", miningIncome: "0", incomeByCategory: { staking: "1.5", "2024": "odd key" }, expenses: "1200", netIncome: "0", rawCapitalGainLoss: "0", bridgingGainLoss: "0", wrappingGainLoss: "0", capitalGainLossByCategory: {}, discountedCapitalGainLoss: "0", netCapitalGainLoss: "43459.49", feesPaid: "0", feesByCategory: {}, broughtForwardLoss: "0", carriedForwardLoss: "-41434.915835700685932000", taxableAmount: "1016568.12", taxPayable: "254142.028769325140330000", rate: "0.25", applicable: true },
  { financialYear: "2025–26", startYear: 2025, startDate: "2025-07-01", endDate: "2026-06-30", currency: "aud", incomeLabel: "Assessable income", income: "0", rewardIncome: "0", miningIncome: "0", incomeByCategory: {}, expenses: "0", netIncome: "0", rawCapitalGainLoss: "0", bridgingGainLoss: "0", wrappingGainLoss: "0", capitalGainLossByCategory: {}, discountedCapitalGainLoss: "0", netCapitalGainLoss: "0", feesPaid: "0", feesByCategory: {}, broughtForwardLoss: "0", carriedForwardLoss: "0.000", taxableAmount: "0", taxPayable: "0", rate: null, applicable: false, notes: "pass-through" },
];

const WARNINGS = {
  "zero-cost": Array.from({ length: 7 }, (_, i) => ({ disposalId: `d${i}`, txEventId: `t${i}`, eventType: "sell", ts: `2025-0${(i % 9) + 1}-15T04:05:06.000Z`, assetSymbol: i % 2 ? "ETH" : "BTC", assetChain: null, sourceLabel: "Kraken", sourceAdapterKey: "kraken", quantity: `0.${i}5`, proceedsAmount: `${1000 * i}.555`, gainLossAmount: "1", currency: "aud" })),
  "uncategorized-transfers": [{ txEventId: "u1", eventType: "transfer", ts: "2025-03-01T00:00:00.000Z", direction: "out", assetSymbol: "USDC", assetChain: "ethereum", amount: "1e3" }],
  "unpriced-assets": [{ assetId: "x", symbol: "OBSCURE", chain: "solana", contractOrMintAddress: null, legCount: 3, incomeLegs: 1, netQuantity: "5", firstSeen: "2025-01-01", lastSeen: "2025-02-01" }],
  "unbalanced-transfers": [],
};

const HISTORY = Array.from({ length: 420 }, (_, i) => {
  const date = new Date(Date.UTC(2025, 5, 1) + i * 86400000).toISOString().slice(0, 10);
  const value = 100000 + Math.sin(i / 20) * 20000 + i * 150;
  return { date, value: value.toFixed(6), cumulativeIncome: (i * 12.5).toFixed(2), unrealizedPL: (Math.cos(i / 15) * 5000).toFixed(4) };
});

const TX_ASSETS = [
  { id: "asset-sol", chain: "solana", symbol: "SOL" },
  { id: "asset-usdc", chain: "solana", symbol: "USDC" },
];
const TX_EVENTS = Array.from({ length: 150 }, (_, i) => ({
  id: `${String(i).padStart(8, "0")}-1d7e-4d0a-9d1b-3c1f2b8e9a01`,
  eventType: i % 5 === 0 ? "transfer" : "trade",
  isManuallyCategorized: i % 7 === 0,
  isInternalTransfer: i % 5 === 0,
  ts: new Date(Date.UTC(2026, 8, 29, 1, 30) - i * 3600000).toISOString(),
  description: null, notes: i === 1 ? "Rebalance after the audit" : null, detectedProtocol: i % 2 ? "jupiter" : null,
  taxTreatment: i % 5 === 0 ? "non_taxable" : "capital_gain_loss",
  legs: [
    { assetId: "asset-sol", direction: "out", role: "primary", amount: `${i + 1}.5`, value: `${(i + 1) * 250}.125`, currency: "aud", proceeds: `${(i + 1) * 250}.125`, costBasis: `${(i + 1) * 200}`, gainLoss: i % 5 === 0 ? null : `${(i + 1) * 50}.125` },
    ...(i % 5 === 0 ? [] : [{ assetId: "asset-usdc", direction: "in", role: "primary", amount: `${(i + 1) * 170}`, value: `${(i + 1) * 250}`, currency: "aud", proceeds: null, costBasis: `${(i + 1) * 250}`, gainLoss: null }]),
    { assetId: "asset-sol", direction: "out", role: "fee", amount: "0.000005", value: "0.001", currency: "aud", proceeds: "0.001", costBasis: "0.0009", gainLoss: "0.0001" },
  ],
  source: { id: "src-kraken", label: "Kraken" },
  tags: i % 3 ? [{ label: "Swap" }] : [],
}));

// Report bodies: a BOM, quoting, a prose note, surplus fields, an integer-like
// column — the things a re-framer can get subtly wrong.
const REPORT_CSV = '﻿Date,Asset,"Proceeds, AUD",2025\r\n2025-07-01,BTC,"1,234.56",x\r\n2025-07-02,"He said ""hi""",7.89,y,surplus\r\n"multi\nline",ETH,0.000000010000000001,z\r\n';
const NOTE_CSV = "Field,Value\r\nNo cached tax summary found for this financial year — run a sync first.\r\n";
const BINARY = Buffer.from(Array.from({ length: 4096 }, (_, i) => (i * 7919) % 256));

// ---------- stub server ----------

let activityPolls = 0;
let syncStartedAt = null;
let posts = [];

function resetStub() {
  activityPolls = 0;
  syncStartedAt = null;
  posts = [];
}

function send(res, status, body, headers = {}) {
  res.writeHead(status, { "content-type": "application/json", ...headers });
  res.end(typeof body === "string" ? body : JSON.stringify(body));
}

const server = createServer((req, res) => {
  const token = (req.headers.authorization ?? "").replace(/^Bearer /, "");
  const url = new URL(req.url, "http://stub");
  const path = url.pathname;

  // Token-selected failure modes, for any route.
  const failures = {
    grb_401: [401, { error: "Invalid or revoked token" }],
    grb_402: [402, { error: "Reports require a paid plan." }],
    grb_403ro: [403, { error: "This token is read-only", readOnlyToken: true }],
    grb_403: [403, { error: "Forbidden" }],
    grb_404: [404, { error: "Not found" }],
    grb_400: [400, { error: { formErrors: [], fieldErrors: { year: ["Required"], name: ["Too short"] } } }],
  };
  if (failures[token]) return send(res, ...failures[token]);
  if (token === "grb_502") return send(res, 502, "<html><body>Bad Gateway</body></html>", { "content-type": "text/html" });
  if (token === "grb_badjson") return send(res, 200, "{not json");
  if (!token.startsWith("grb_ok") && token !== "grb_empty" && token !== "grb_oldcli") return send(res, 401, { error: "Invalid token" });
  if (token === "grb_oldcli") res.setHeader("x-grubless-min-cli-version", "99.0.0");

  const empty = token === "grb_empty";
  let body = "";
  req.on("data", (c) => (body += c));
  req.on("end", () => {
    if (req.method === "POST") {
      posts.push({ path, body: JSON.parse(body || "null") });
      if (path.endsWith("/sync") || path.endsWith("/csv-import")) {
        syncStartedAt ??= new Date().toISOString();
        return send(res, 202, { queued: true });
      }
      return send(res, 404, { error: "no such route" });
    }

    // "grb_ok_dots" can also see an entity named "..", as someone sharing an
    // entity could have named it.
    if (path === "/entities") return send(res, 200, empty ? [] : token === "grb_ok_dots" ? [...ENTITIES, DOTS_ENTITY] : ENTITIES);
    if (path === "/api-tokens") return empty ? send(res, 500, { error: "boom" }) : send(res, 200, [{ id: "t1", name: "laptop", scope: "read", lastUsedAt: null, expiresAt: null, createdAt: "2025-07-01T00:00:00.000Z" }]);

    const m = /^\/entities\/([^/]+)\/(.+)$/.exec(path);
    if (!m) return send(res, 404, { error: "Not found" });
    const [, id, rest] = m;

    if (rest === "sources") return send(res, 200, SOURCES[id] ?? []);
    if (rest === "holdings") return send(res, 200, id === ACME ? HOLDINGS : []);
    if (rest === "tax-summary") return send(res, 200, id === ACME ? TAX : []);
    if (rest.startsWith("warnings/")) return send(res, 200, id === ACME ? WARNINGS[rest.slice(9)] ?? [] : []);
    if (rest === "portfolio-history") return send(res, 200, id === ACME ? HISTORY : []);
    if (rest === "tax-settings") return id === ACME ? send(res, 200, { entityId: id, baseCurrency: "aud", financialYearStartMonth: 7 }) : send(res, 404, { error: "no settings" });

    // The overview's extras (Go TUI only). The breakdown is per day and
    // category, as the API's; the stub's covers the history's span.
    if (rest === "activity-breakdown") {
      if (id !== ACME) return send(res, 200, { currency: "aud", totalEvents: 0, labels: {}, category: [], source: [], tag: [] });
      const kinds = ["trade", "staking_reward", "transfer", "send", "receive", "fee", "airdrop", "income"];
      const category = HISTORY.flatMap((p, i) => kinds.slice(0, 1 + (i % kinds.length)).map((k, j) => ({ d: p.date, k, v: 1000 / (j + 1) + (i % 7) * 10 })));
      return send(res, 200, { currency: "aud", totalEvents: 1234, labels: {}, category, source: [], tag: [] });
    }
    if (rest === "price-coverage") return send(res, 200, id === ACME ? { total: 415, priced: 412, missing: 3 } : { total: 0, priced: 0, missing: 0 });

    if (rest === "tx-events") {
      // Production's shape (probed 2026-09-30): {events, assets, nextCursor},
      // cursor-paged. Only the Go build calls this; see GO_ONLY below.
      const limit = Number(url.searchParams.get("limit"));
      if (!(limit >= 1 && limit <= 2000)) return send(res, 400, { error: { formErrors: [], fieldErrors: { limit: ["Expected number, received nan"] } } });
      const start = Number((url.searchParams.get("cursor") ?? "cur-0").slice(4));
      // The filters the API applies, applied the same way here.
      const q = url.searchParams;
      const categories = q.get("category")?.split(",");
      let all = (id === ACME ? TX_EVENTS : []).filter(
        (e) =>
          (!categories || categories.includes(e.eventType)) &&
          (!q.get("direction") || e.legs.some((l) => l.role !== "fee" && l.direction === q.get("direction"))) &&
          (!q.get("sourceId") || e.source.id === q.get("sourceId")) &&
          (!q.get("assetId") || e.legs.some((l) => l.assetId === q.get("assetId"))) &&
          (!q.get("from") || e.ts >= new Date(q.get("from")).toISOString()) &&
          (!q.get("to") || e.ts <= new Date(q.get("to")).toISOString()) &&
          (!q.get("q") || JSON.stringify(e).toLowerCase().includes(q.get("q").toLowerCase())),
      );
      if (q.get("sort") === "asc") all = [...all].reverse();
      const page = all.slice(start, start + limit);
      return send(res, 200, { events: page, assets: TX_ASSETS, nextCursor: start + limit < all.length ? `cur-${start + limit}` : null });
    }

    if (rest === "activity") {
      // Advances per poll: queued, running (two messages), then finished.
      // "grb_ok_fail" ends in an error; "grb_ok_degraded" in a caveat.
      activityPolls++;
      const startedAt = syncStartedAt ?? "2020-01-01T00:00:00.000Z";
      const status = activityPolls === 1 ? "queued" : activityPolls < 4 ? "running" : token === "grb_ok_fail" ? "error" : token === "grb_ok_degraded" ? "degraded" : "success";
      const message = activityPolls === 1 ? null : activityPolls < 4 ? `Fetching page ${activityPolls - 1}…` : status === "degraded" ? "Price provider budget exhausted" : null;
      return send(res, 200, [
        { id: "act1", entityId: id, sourceId: "src-kraken", jobType: "sync_source", status, message, error: status === "error" ? "Kraken returned 500" : null, startedAt, updatedAt: startedAt, finishedAt: null, source: { label: "Kraken" } },
        { id: "old", entityId: id, sourceId: null, jobType: "recalculate", status: "running", message: "ancient", error: null, startedAt: "2020-01-01T00:00:00.000Z", updatedAt: "2020-01-01T00:00:00.000Z", finishedAt: null, source: null },
      ]);
    }

    const r = /^reports\/([^?]+)$/.exec(rest);
    if (r) {
      const name = r[1];
      const year = url.searchParams.get("year");
      if (id === SOC && name === "income") return send(res, 500, { error: "Report generation failed" });
      if (name === "complete-tax") return send(res, 200, BINARY.toString("latin1"), { "content-type": "application/zip", "content-disposition": `attachment; filename="complete-tax-FY${year}.zip"` });
      if (name === "ato-mytax") return send(res, 200, BINARY.toString("latin1"), { "content-type": "application/pdf", "content-disposition": `attachment; filename="ato-mytax.pdf"` });
      if (name === "fees") return send(res, 200, REPORT_CSV, { "content-type": "text/csv", "content-disposition": "attachment; filename*=UTF-8''fees%E2%82%AC-FY.csv" });
      if (name === "expenses") return send(res, 200, REPORT_CSV, { "content-type": "text/csv", "content-disposition": "attachment; filename*=UTF-8''bad%E2.csv" });
      if (name === "gifts-donations-lost") return send(res, 200, NOTE_CSV, { "content-type": "text/csv" });
      // Hostile filenames: each tries to climb out of --out.
      const hostile = {
        "highest-balance": 'attachment; filename="../../escape.csv"',
        "end-of-year-holdings": "attachment; filename*=UTF-8''..%2F..%2Fencoded.csv",
        "beginning-of-year-holdings": 'attachment; filename="..\\..\\windows.csv"',
        "division-70-trading-stock": 'attachment; filename=".."',
      };
      if (hostile[name]) return send(res, 200, REPORT_CSV, { "content-type": "text/csv", "content-disposition": hostile[name] });
      if (name === "other-gains") return send(res, 200, "", { "content-type": "text/csv" });
      return send(res, 200, REPORT_CSV, { "content-type": "text/csv", "content-disposition": `attachment; filename="${name}${year ? "-FY" + year : ""}.csv"` });
    }
    send(res, 404, { error: "Not found" });
  });
});
await new Promise((r) => server.listen(0, "127.0.0.1", r));
const API = `http://127.0.0.1:${server.address().port}`;

// `--serve`: just run the stub, for go/scripts/tty-parity.py. Its state
// resets on SIGUSR2, so each run of the TUI starts with a fresh activity feed.
if (process.argv[2] === "--serve") {
  process.on("SIGUSR2", resetStub);
  console.log(API);
  await new Promise(() => {});
}

// ---------- scenarios ----------

const OK = { GRUBLESS_TOKEN: "grb_ok" };
const csvFile = join(mkdtempSync(join(tmpdir(), "parity-in-")), "trades.csv");
writeFileSync(csvFile, "﻿date,amount\n2025-01-01,1.5\nSociété,\xff\n");

/** [label, argv, env, opts] — opts.out gives each build a fresh --out directory. */
const CASES = [
  // Basics and argument handling
  ["version", ["--version"], {}],
  ["help", ["--help"], {}],
  ["no command, stdin not a tty", [], {}],
  ["unknown command, signed in", ["nonsense"], OK],
  ["unknown command, signed out", ["nonsense"], {}],
  ["unknown flag", ["entities", "list", "--nope"], OK],
  ["flag missing its value", ["holdings", "--entity"], OK],
  ["flag value looks like a flag", ["holdings", "--entity", "--json"], OK],
  ["boolean given a value", ["entities", "list", "--json=yes"], OK],
  ["bad --timeout", ["sources", "sync", "--timeout", "soon"], OK],
  ["hex --timeout accepted", ["sources", "list", "--entity", "acme", "--timeout", "0x10"], OK],
  ["no credential", ["entities", "list"], {}],
  ["401", ["entities", "list"], { GRUBLESS_TOKEN: "grb_401" }],
  ["402", ["entities", "list"], { GRUBLESS_TOKEN: "grb_402" }],
  ["403 read-only", ["entities", "list"], { GRUBLESS_TOKEN: "grb_403ro" }],
  ["403 role", ["entities", "list"], { GRUBLESS_TOKEN: "grb_403" }],
  ["404", ["entities", "list"], { GRUBLESS_TOKEN: "grb_404" }],
  ["400 zod", ["entities", "list"], { GRUBLESS_TOKEN: "grb_400" }],
  ["502 html", ["entities", "list"], { GRUBLESS_TOKEN: "grb_502" }],
  ["server requires newer cli", ["entities", "list"], { GRUBLESS_TOKEN: "grb_oldcli" }],
  ["unreachable host", ["entities", "list", "--api-url", "http://127.0.0.1:1"], OK],

  // entities / auth
  ["entities list", ["entities", "list"], OK],
  ["entities list --json", ["entities", "list", "--json"], OK],
  ["entities list, empty", ["entities", "list"], { GRUBLESS_TOKEN: "grb_empty" }],
  ["entities unknown sub", ["entities", "delete"], OK],
  ["whoami", ["auth", "whoami"], OK],
  ["whoami --json", ["auth", "whoami", "--json"], OK],
  ["whoami, token listing fails", ["auth", "whoami", "--json"], { GRUBLESS_TOKEN: "grb_empty" }],
  ["login without a tty", ["auth", "login"], {}],
  ["login with a rejected token", ["auth", "login", "--token", "grb_401"], {}],

  // entity resolution
  ["resolve by prefix", ["holdings", "--entity", "acme"], OK],
  ["resolve by exact name, case-insensitive", ["holdings", "--entity", "ACME TRADING PTY LTD"], OK],
  ["resolve by uuid", ["holdings", "--entity", SOC], OK],
  ["resolve unknown", ["holdings", "--entity", "Nonexistent"], OK],
  ["resolve ambiguous", ["holdings", "--entity", ""], OK],
  ["scope required", ["holdings"], OK],
  ["all-entities with none", ["holdings", "--all-entities"], { GRUBLESS_TOKEN: "grb_empty" }],

  // sources
  ["sources list", ["sources", "list", "--entity", "acme"], OK],
  ["sources (no sub)", ["sources", "--entity", "acme"], OK],
  ["sources list --all-entities", ["sources", "list", "--all-entities"], OK],
  ["sources list --json", ["sources", "list", "--all-entities", "--json"], OK],
  ["sources list, none", ["sources", "list", "--entity", "soc"], OK],
  ["sources unknown sub", ["sources", "frob", "--entity", "acme"], OK],
  ["sync needs a target", ["sources", "sync", "--entity", "acme"], OK],
  ["sync unknown source", ["sources", "sync", "--entity", "acme", "--source", "nope"], OK],
  ["sync one source by label", ["sources", "sync", "--entity", "acme", "--source", "Kraken"], OK],
  ["sync --all --full --json", ["sources", "sync", "--entity", "acme", "--all", "--full", "--json"], OK],
  ["sync --all-entities --all --json", ["sources", "sync", "--all-entities", "--all", "--json"], OK],
  ["sync --wait", ["sources", "sync", "--entity", "acme", "--source", "src-kraken", "--wait"], OK, { slow: true }],
  ["sync --wait --json", ["sources", "sync", "--entity", "acme", "--all", "--wait", "--json"], OK, { slow: true }],
  ["sync --wait, job fails", ["sources", "sync", "--entity", "acme", "--all", "--wait"], { GRUBLESS_TOKEN: "grb_ok_fail" }, { slow: true }],
  ["sync --wait, job degraded", ["sources", "sync", "--entity", "acme", "--all", "--wait"], { GRUBLESS_TOKEN: "grb_ok_degraded" }, { slow: true }],
  ["sync --wait times out", ["sources", "sync", "--entity", "acme", "--all", "--wait", "--timeout", "0.03"], OK, { slow: true }],

  // import
  ["import needs a file", ["import"], OK],
  ["import needs --source", ["import", csvFile, "--entity", "acme"], OK],
  ["import needs --entity", ["import", csvFile, "--source", "src-csv"], OK],
  ["import missing file", ["import", "/nonexistent/x.csv", "--entity", "acme", "--source", "src-csv"], OK],
  ["import a directory", ["import", tmpdir(), "--entity", "acme", "--source", "src-csv"], OK],
  ["import", ["import", csvFile, "--entity", "acme", "--source", "src-csv", "--json"], OK],
  ["import --wait --json", ["import", csvFile, "--entity", "acme", "--source", "src-csv", "--wait", "--json"], OK, { slow: true }],

  // holdings / tax / warnings
  ["holdings", ["holdings", "--entity", "acme"], OK],
  ["holdings --all-entities", ["holdings", "--all-entities"], OK],
  ["holdings --json", ["holdings", "--all-entities", "--json"], OK],
  ["tax-summary", ["tax-summary", "--entity", "acme"], OK],
  ["tax-summary --year", ["tax-summary", "--entity", "acme", "--year", "2024"], OK],
  ["tax-summary unknown year", ["tax-summary", "--entity", "acme", "--year", "1999"], OK],
  ["tax-summary --all-entities", ["tax-summary", "--all-entities"], OK],
  ["tax-summary --json", ["tax-summary", "--entity", "acme", "--json"], OK],
  ["warnings", ["warnings", "--entity", "acme"], OK],
  ["warnings --fail-on-blocking", ["warnings", "--entity", "acme", "--fail-on-blocking"], OK],
  ["warnings clean entity gate", ["warnings", "--entity", "soc", "--fail-on-blocking"], OK],
  ["warnings --all-entities", ["warnings", "--all-entities"], OK],
  ["warnings --json (one)", ["warnings", "--entity", "acme", "--json"], OK],
  ["warnings --json (all)", ["warnings", "--all-entities", "--json"], OK],

  // portfolio
  ["portfolio", ["portfolio", "--entity", "acme"], OK],
  ["portfolio --range fy", ["portfolio", "--entity", "acme", "--range", "fy"], OK],
  ["portfolio --range 1w", ["portfolio", "--entity", "acme", "--range", "1w"], OK],
  ["portfolio no history", ["portfolio", "--entity", "soc"], OK],
  ["portfolio --range fy, no settings", ["portfolio", "--entity", "soc", "--range", "fy"], OK],
  ["portfolio bad range", ["portfolio", "--entity", "acme", "--range", "2w"], OK],
  ["portfolio --all-entities", ["portfolio", "--all-entities"], OK],
  ["portfolio --json", ["portfolio", "--entity", "acme", "--range", "1m", "--json"], OK],
  ["portfolio --json --all-entities", ["portfolio", "--all-entities", "--range", "3m", "--json"], OK],

  // reports
  ["report, none named", ["report"], OK],
  ["report, two named", ["report", "fees", "income"], OK],
  ["report unknown", ["report", "nope", "--entity", "acme", "--year", "2025"], OK],
  ["report needs --year", ["report", "income", "--entity", "acme"], OK],
  ["report yearless", ["report", "balances-per-source", "--entity", "acme"], OK],
  ["report --json on a PDF", ["report", "ato-mytax", "--entity", "acme", "--year", "2025", "--json"], OK],
  ["report --json on the bundle", ["report", "bundle", "--entity", "acme", "--year", "2025", "--json"], OK],
  ["report --json with --out", ["report", "income", "--entity", "acme", "--year", "2025", "--json", "--out", "x"], OK],
  ["report --all-entities needs a target", ["report", "income", "--all-entities", "--year", "2025"], OK],
  ["report to stdout", ["report", "capital-gains", "--entity", "acme", "--year", "2025"], OK],
  ["report binary to stdout", ["report", "bundle", "--entity", "acme", "--year", "2025"], OK],
  ["report --json", ["report", "capital-gains", "--entity", "acme", "--year", "2025", "--json"], OK],
  ["report --json, prose note", ["report", "gifts-donations-lost", "--entity", "acme", "--year", "2025", "--json"], OK],
  ["report --json, empty body", ["report", "other-gains", "--entity", "acme", "--year", "2025", "--json"], OK],
  ["report --json yearless", ["report", "balances-per-source", "--entity", "acme", "--year", "2025", "--json"], OK],
  ["report --json --all-entities, one fails", ["report", "income", "--all-entities", "--year", "2025", "--json"], OK],
  ["report --out file", ["report", "capital-gains", "--entity", "acme", "--year", "2025", "--out", "{out}/cg.csv"], OK, { out: true }],
  ["report --out missing dir", ["report", "capital-gains", "--entity", "acme", "--year", "2025", "--out", "{out}/nope/cg.csv"], OK, { out: true }],
  ["report --out is a directory", ["report", "capital-gains", "--entity", "acme", "--year", "2025", "--out", "{out}"], OK, { out: true }],
  ["report --out --all-entities", ["report", "capital-gains", "--all-entities", "--year", "2025", "--out", "{out}/clients"], OK, { out: true }],
  ["report bundle --out --all-entities", ["report", "bundle", "--all-entities", "--year", "2025", "--out", "{out}"], OK, { out: true }],
  ["report, percent-encoded filename", ["report", "fees", "--all-entities", "--year", "2025", "--out", "{out}"], OK, { out: true }],
  ["report, malformed filename", ["report", "expenses", "--all-entities", "--year", "2025", "--out", "{out}"], OK, { out: true }],
  // Path traversal: none of these may write outside --out.
  ["report, server filename climbs out", ["report", "highest-balance", "--all-entities", "--year", "2025", "--out", "{out}"], OK, { out: true }],
  ["report, server filename climbs out, encoded", ["report", "end-of-year-holdings", "--all-entities", "--year", "2025", "--out", "{out}"], OK, { out: true }],
  ["report, server filename climbs out, backslashes", ["report", "beginning-of-year-holdings", "--all-entities", "--year", "2025", "--out", "{out}"], OK, { out: true }],
  ["report, server filename is ..", ["report", "division-70-trading-stock", "--all-entities", "--year", "2025", "--out", "{out}"], OK, { out: true }],
  ["report, entity named ..", ["report", "capital-gains", "--all-entities", "--year", "2025", "--out", "{out}"], { GRUBLESS_TOKEN: "grb_ok_dots" }, { out: true }],
  ["report, one entity fails", ["report", "income", "--all-entities", "--year", "2025", "--out", "{out}"], OK, { out: true }],
  ["report, 402 stops the run", ["report", "income", "--all-entities", "--year", "2025", "--out", "{out}"], { GRUBLESS_TOKEN: "grb_402" }, { out: true }],
  ["report, 401 is per-entity", ["report", "income", "--entity", "acme", "--year", "2025"], { GRUBLESS_TOKEN: "grb_401" }],

  // interactive, without a terminal
  ["tui refuses a pipe", [], { GRUBLESS_TOKEN: "grb_ok", FORCE_STDIN_TTY: "" }],
];

const SEQUENCE = [
  ["login", ["auth", "login", "--token", "grb_ok"]],
  ["entities list from the file", ["entities", "list"]],
  ["whoami from the file", ["auth", "whoami", "--json"]],
  ["world-readable config warns", ["entities", "list"], "chmod644"],
  ["logout", ["auth", "logout"]],
  ["signed out after logout", ["entities", "list"]],
];

// ---------- running ----------

function run(bin, args, env, home) {
  const isNode = bin.endsWith(".js");
  const [file, argv] = isNode ? [process.execPath, [bin, ...args]] : [bin, args];
  const needsApi = !args.some((a) => a.startsWith("--api-url"));
  return new Promise((resolve) => {
    execFile(
      file,
      needsApi ? [...argv, "--api-url", API] : argv,
      { encoding: "buffer", maxBuffer: 64 << 20, env: { PATH: "", HOME: home, XDG_CONFIG_HOME: home, NO_COLOR: "1", TZ: "UTC", ...env } },
      (err, stdout, stderr) => resolve({ code: err ? (err.code ?? 1) : 0, stdout, stderr }),
    ).stdin.end();
  });
}

/** Every file under dir, as relative path → bytes. */
function tree(dir) {
  const out = {};
  if (!existsSync(dir)) return out;
  const walk = (d) => {
    for (const name of readdirSync(d)) {
      const p = join(d, name);
      if (statSync(p).isDirectory()) walk(p);
      else out[relative(dir, p)] = readFileSync(p).toString("base64");
    }
  };
  walk(dir);
  return out;
}

/**
 * What the Go build adds on purpose, removed from its output before
 * comparing — so the rest of that output is still held to the Node build's.
 * That's the help text's lines for the Go-only features: transactions, and
 * the TUI's themes.
 */
function withoutGoOnly(text) {
  return text
    .replace("  transactions                  An entity's transactions, filtered, newest first\n\n", "")
    .replace(/\nTRANSACTIONS OPTIONS\n(?:  .*\n)+/, "")
    .replace("  GRUBLESS_THEME                Interface theme: cypher (default) or terminal; t switches it\n", "");
}

/** The only other accepted difference: the transport error text, which names the host either way. */
function normalise(buf, dirs) {
  let text = buf.toString("latin1");
  for (const [dir, label] of dirs) text = text.replaceAll(dir, label);
  return text.replace(/(Could not reach \S+): .*/g, "$1: <transport error>").replaceAll(API, "<api>");
}

let failures = 0;
let ran = 0;
function compare(label, a, b, dirs) {
  const diffs = [];
  if (a.code !== b.code) diffs.push(`exit: node ${a.code}, go ${b.code}`);
  for (const stream of ["stdout", "stderr"]) {
    const [x, y] = [normalise(a[stream], dirs.node), withoutGoOnly(normalise(b[stream], dirs.go))];
    if (x !== y) diffs.push(`${stream}:\n    node: ${JSON.stringify(x.slice(0, 2000))}\n    go:   ${JSON.stringify(y.slice(0, 2000))}`);
  }
  if (a.files !== undefined && JSON.stringify(a.files) !== JSON.stringify(b.files)) {
    diffs.push(`files:\n    node: ${Object.keys(a.files).join(", ")}\n    go:   ${Object.keys(b.files).join(", ")}${Object.keys(a.files).join() === Object.keys(b.files).join() ? " (same names, different bytes)" : ""}`);
  }
  ran++;
  if (diffs.length) {
    failures++;
    console.log(`✗ ${label}\n  ${diffs.join("\n  ")}`);
  } else {
    console.log(`✓ ${label}  (exit ${a.code})`);
  }
}

for (const [label, args, env, opts = {}] of CASES) {
  if (FILTER && !label.includes(FILTER)) continue;
  const results = {};
  const dirs = {};
  for (const [name, bin] of [["node", NODE_CLI], ["go", GO_CLI]]) {
    resetStub();
    const home = mkdtempSync(join(tmpdir(), "parity-home-"));
    // --out sits three levels inside a sandbox, and it's the whole sandbox
    // that's collected: a file that climbs out of --out lands where the
    // comparison, and the escape check below, can see it.
    const sandbox = mkdtempSync(join(tmpdir(), "parity-out-"));
    const out = join(sandbox, "a", "b", "out");
    mkdirSync(out, { recursive: true });
    const argv = args.map((a) => a.replace("{out}", out));
    results[name] = await run(bin, argv, env, home);
    if (opts.out) {
      results[name].files = tree(sandbox);
      results[name].escaped = Object.keys(results[name].files).filter((f) => !f.startsWith(join("a", "b", "out") + "/"));
    }
    results[name].posts = JSON.stringify(posts);
    // The stub stamps activity with the moment the sync was queued, which
    // differs between the two runs by however long the first one took.
    dirs[name] = [[out, "<out>"], [home, "<home>"], ...(syncStartedAt ? [[syncStartedAt, "<queued-at>"]] : [])];
    rmSync(home, { recursive: true, force: true });
    rmSync(sandbox, { recursive: true, force: true });
  }
  compare(label, results.node, results.go, dirs);
  for (const name of ["node", "go"]) {
    if (results[name].escaped?.length) {
      failures++;
      console.log(`✗ ${label}: ${name} wrote outside --out: ${results[name].escaped.join(", ")}`);
    }
  }
  if (process.env.PARITY_SHOW) {
    // What the scenario actually produced, so a match can be checked for
    // being a meaningful one rather than two builds agreeing on nothing.
    const show = (b) => b.toString("utf8").split("\n").slice(0, 30).map((l) => "      " + l).join("\n");
    console.log(`    stdout:\n${show(results.go.stdout)}\n    stderr:\n${show(results.go.stderr)}`);
    if (results.go.files) console.log(`    files: ${Object.entries(results.go.files).map(([k, v]) => `${k} (${Buffer.from(v, "base64").length}B)`).join(", ")}`);
  }
  if (results.node.posts !== results.go.posts) {
    failures++;
    console.log(`✗ ${label}: requests sent differ\n    node: ${results.node.posts.slice(0, 500)}\n    go:   ${results.go.posts.slice(0, 500)}`);
  }
}

if (!FILTER || "sequence".includes(FILTER)) {
  const homes = { node: mkdtempSync(join(tmpdir(), "parity-")), go: mkdtempSync(join(tmpdir(), "parity-")) };
  for (const [label, args, action] of SEQUENCE) {
    for (const dir of Object.values(homes)) {
      const file = join(dir, "grubless", "config.json");
      if (action === "chmod644" && existsSync(file)) chmodSync(file, 0o644);
    }
    const a = await run(NODE_CLI, args, {}, homes.node);
    const b = await run(GO_CLI, args, {}, homes.go);
    compare(`[sequence] ${label}`, a, b, { node: [[homes.node, "<home>"]], go: [[homes.go, "<home>"]] });
    const read = (d) => (existsSync(join(d, "grubless", "config.json")) ? readFileSync(join(d, "grubless", "config.json"), "utf8").replaceAll(API, "<api>") : "<none>");
    if (read(homes.node) !== read(homes.go)) {
      failures++;
      console.log(`✗ [sequence] ${label}: config file differs\n    node: ${JSON.stringify(read(homes.node))}\n    go:   ${JSON.stringify(read(homes.go))}`);
    }
  }
  Object.values(homes).forEach((d) => rmSync(d, { recursive: true, force: true }));
}

server.close();
rmSync(dirname(csvFile), { recursive: true, force: true });
console.log(`\n${failures === 0 ? "parity: identical" : `parity: ${failures} difference(s)`} across ${ran} scenarios`);
process.exitCode = failures === 0 ? 0 : 1;
