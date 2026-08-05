import { defineConfig } from "tsup";

/**
 * Bundles everything into a single file with NO workspace dependencies left
 * in the output's import graph — `@grubless/*` packages use `workspace:*`
 * versions that npm consumers cannot resolve, so they must be inlined rather
 * than declared. `noExternal` is what forces that; `dependencies` in
 * package.json is deliberately empty (Node 20 gives us fetch and parseArgs,
 * which is all this needs) so the published package's dependency surface —
 * the thing that would carry a supply-chain risk into a machine holding an
 * API token for a firm's whole client list — is zero.
 *
 * `@grubless/api-types` is types-only and erases entirely; it is listed here
 * as a belt-and-braces measure in case a value is ever added to it by
 * mistake. The real guard against the tax engine leaking into this bundle is
 * the `verify-bundle` script (see package.json / docs/plan-cli.md §3).
 */
export default defineConfig({
  entry: ["src/index.ts"],
  format: ["esm"],
  target: "node20",
  platform: "node",
  bundle: true,
  noExternal: [/^@grubless\//],
  clean: true,
  minify: false,
  // One file, not a chunk graph. tsup splits ESM by default, and the TUI is
  // behind a dynamic import — but a `bin` entry that depends on a sibling
  // chunk breaks the moment anyone copies or vendors just the one file, and
  // the deferred parse it buys is a few milliseconds on 22KB.
  splitting: false,
  banner: { js: "#!/usr/bin/env node" },
});
