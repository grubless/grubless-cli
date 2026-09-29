import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

/**
 * Which built CLI the subprocess tests drive.
 *
 * The bundle by default. `GRUBLESS_CLI_BIN` points them at any other build of
 * the same CLI instead — the Go port in `go/`, say — because these tests only
 * talk to it through argv, stdout, stderr and the exit code, and that is
 * exactly the contract a port has to keep.
 */
const here = dirname(fileURLToPath(import.meta.url));

export const CLI = process.env.GRUBLESS_CLI_BIN || join(here, "..", "..", "dist", "index.js");

/** `[file, argv]` for execFile: a native binary runs directly, the bundle through Node. */
export function cliCommand(args: string[]): [string, string[]] {
  return process.env.GRUBLESS_CLI_BIN ? [CLI, args] : [process.execPath, [CLI, ...args]];
}
