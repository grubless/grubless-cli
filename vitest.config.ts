import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    // The e2e tier shells out to the built bundle against a real API over the
    // network — well past vitest's 5s default. The pure suites don't need it
    // and aren't slowed by it: a timeout is a ceiling, not a wait.
    testTimeout: 60_000,
    hookTimeout: 120_000,
    // The e2e suite registers one account and drives one entity through a
    // sequence where later assertions depend on earlier state (no source yet,
    // then a source). Running files in parallel would let another file's
    // clock-time interleave with that; the pure files are fast enough that
    // serialising them costs nothing worth measuring.
    fileParallelism: false,
  },
});
