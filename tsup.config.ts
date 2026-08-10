import { defineConfig } from "tsup";

/**
 * Bundles everything into a single file with an empty dependency graph.
 *
 * `dependencies` in package.json is deliberately empty — Node 20 gives us
 * fetch and parseArgs, which is all this needs — so the published package's
 * dependency surface is zero. That is a security property, not a minimalism
 * exercise: this binary holds an API token that can read a firm's entire
 * client list, so every transitive package it installs is another machine that
 * could reach that token.
 *
 * `noExternal` remains as a backstop. Nothing resolves to `@grubless/*` any
 * more (the wire types were vendored into `src/api-types.ts` when this repo
 * split out of the monorepo), but if a dependency on one is ever added back it
 * must be inlined rather than declared — a `workspace:*` version published to
 * npm is unresolvable for everyone who installs it.
 *
 * The real guard against the tax engine leaking into this bundle is
 * `scripts/verify-bundle.mjs`, which runs on every build.
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
