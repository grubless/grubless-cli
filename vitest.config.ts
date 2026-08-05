import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    // These boot a real API against a scratch database and shell out to the
    // built bundle — well past vitest's 5s default.
    testTimeout: 60_000,
    hookTimeout: 120_000,
    // One suite, one scratch database, one port. Running files in parallel
    // would have them fight over both.
    fileParallelism: false,
  },
});
