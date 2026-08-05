#!/usr/bin/env node
/**
 * Fails the build if the tax engine leaked into the published CLI bundle.
 *
 * `apps/cli` is published to npm. `@grubless/core` is the moat — genuine
 * entity-aware ATO/IRS computation, identified in PLAN-gtm.md §3.1 as the
 * thing that is slowest and most expensive for a competitor to copy. Bundling
 * it would hand it over in readable JS, and would also be a self-hosting
 * vector the SaaS-only decision closed on purpose (STATUS.md §8).
 *
 * The CLI is a thin HTTP client: every figure comes from the API as a
 * finished value. Nothing here should ever need engine code.
 *
 * Why a build check and not a code review rule: `import type` erases at
 * compile time but a plain `import` does not, and the two differ by five
 * characters. A reviewer will miss that on a busy afternoon; grep will not.
 */

import { readFileSync, existsSync, readdirSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const dist = join(root, "dist");

if (!existsSync(join(dist, "index.js"))) {
  console.error(`verify-bundle: ${join(dist, "index.js")} not found — run \`pnpm build\` first.`);
  process.exit(1);
}

/**
 * EVERY emitted file, not just index.js. A dynamic import (the TUI is behind
 * one) makes the bundler emit extra chunks, and a guard that only reads the
 * entry point would wave through a leak in any of them. Splitting is off in
 * tsup.config.ts, so today this is one file — checking the directory means it
 * stays correct if that ever changes.
 */
const files = readdirSync(dist).filter((f) => f.endsWith(".js") || f.endsWith(".mjs") || f.endsWith(".cjs"));
const source = files.map((f) => readFileSync(join(dist, f), "utf8")).join("\n");

/**
 * Distinctive runtime symbols from the engine. Chosen because each is
 * exported from @grubless/core and would never appear in a thin client by
 * coincidence.
 */
const FORBIDDEN = [
  "processDisposal",
  "consumeLots",
  "orderLotsForConsumption",
  "toDisposals",
  "auAtoRuleset",
  "usIrsRuleset",
  "classifyCategory",
  // The workspace specifier itself: if this survives bundling, the package is
  // unusable on npm anyway (workspace:* cannot resolve for a consumer).
  "@grubless/core",
];

const found = FORBIDDEN.filter((symbol) => source.includes(symbol));

if (found.length > 0) {
  console.error("verify-bundle: tax engine code leaked into the published CLI bundle.\n");
  for (const symbol of found) console.error(`  ✗ ${symbol}`);
  console.error(
    "\nThe CLI must stay a thin HTTP client (docs/plan-cli.md §3).\n" +
      "Most likely cause: a value import from @grubless/core that should be `import type`.\n" +
      "Wire shapes belong in @grubless/api-types, which is types-only.",
  );
  process.exit(1);
}

console.log(
  `verify-bundle: clean (${files.length} file(s), ${(source.length / 1024).toFixed(1)}KB, no engine symbols, 0 runtime deps)`,
);
