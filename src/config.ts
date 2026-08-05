import { chmodSync, mkdirSync, readFileSync, statSync, writeFileSync, rmSync } from "node:fs";
import { homedir, platform } from "node:os";
import { dirname, join } from "node:path";
import { execFileSync } from "node:child_process";

/**
 * Where the API token lives, and in what order we look for it.
 *
 * Precedence — env, then keychain, then file — is deliberate:
 *
 * 1. `GRUBLESS_TOKEN` wins, because CI has no keychain and no interactive
 *    login, and an env var that silently loses to a stale file on a shared
 *    build machine is a genuinely awful afternoon.
 * 2. The OS keychain, because a token that reaches an accounting firm's whole
 *    client list should not sit in plaintext by default.
 * 3. A `0600` file, because keychains are unavailable often enough (headless
 *    Linux without libsecret, containers) that a hard dependency on one would
 *    make the CLI unusable in exactly the places it is most useful.
 *
 * The keychain is driven by shelling out to the platform's own tool rather
 * than a native addon: a native module would mean prebuilds per platform and
 * per Node ABI, which is a disproportionate amount of machinery — and build
 * breakage — for storing one string.
 */

const SERVICE = "grubless-cli";
const ACCOUNT = "api-token";

export const DEFAULT_API_URL = "https://api.grubless.io";

export interface CliConfig {
  apiUrl: string;
  token: string | null;
  /** Where the token came from, for `whoami` and for diagnosing surprises. */
  tokenSource: "env" | "keychain" | "file" | null;
}

function configDir(): string {
  // XDG on Linux; the same path shape is fine on macOS/Windows for the
  // non-secret half of the config (the API URL), which is all this file
  // holds when a keychain is available.
  const base = process.env.XDG_CONFIG_HOME ?? join(homedir(), ".config");
  return join(base, "grubless");
}

function configPath(): string {
  return join(configDir(), "config.json");
}

interface StoredConfig {
  apiUrl?: string;
  token?: string;
}

function readStored(): StoredConfig {
  try {
    const path = configPath();
    const raw = readFileSync(path, "utf8");

    // A token file readable by other accounts on the machine is worth saying
    // out loud — silently trusting it teaches people it is fine. Warn rather
    // than refuse: locking someone out of their own CLI over a permission bit
    // is a worse failure than the risk it mitigates.
    const mode = statSync(path).mode & 0o777;
    if (mode & 0o077) {
      process.stderr.write(
        `warning: ${path} is readable by other users (mode ${mode.toString(8)}). Run: chmod 600 ${path}\n`,
      );
    }
    return JSON.parse(raw) as StoredConfig;
  } catch {
    return {};
  }
}

function writeStored(next: StoredConfig): void {
  const dir = configDir();
  mkdirSync(dir, { recursive: true, mode: 0o700 });
  const path = configPath();
  writeFileSync(path, JSON.stringify(next, null, 2) + "\n", { mode: 0o600 });
  // writeFileSync's `mode` only applies when the file is CREATED — an
  // existing file keeps whatever permissions it already had, so a file that
  // was once world-readable would stay that way. chmod unconditionally.
  chmodSync(path, 0o600);
}

// ---------- keychain ----------

function keychainGet(): string | null {
  try {
    if (platform() === "darwin") {
      const out = execFileSync(
        "security",
        ["find-generic-password", "-s", SERVICE, "-a", ACCOUNT, "-w"],
        { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] },
      );
      return out.trim() || null;
    }
    if (platform() === "linux") {
      const out = execFileSync("secret-tool", ["lookup", "service", SERVICE, "account", ACCOUNT], {
        encoding: "utf8",
        stdio: ["ignore", "pipe", "ignore"],
      });
      return out.trim() || null;
    }
  } catch {
    // Tool missing, or no entry stored. Both mean "no keychain token here",
    // which is a normal state, not an error worth surfacing.
  }
  return null;
}

function keychainSet(token: string): boolean {
  try {
    if (platform() === "darwin") {
      execFileSync("security", ["add-generic-password", "-U", "-s", SERVICE, "-a", ACCOUNT, "-w", token], {
        stdio: "ignore",
      });
      return true;
    }
    if (platform() === "linux") {
      execFileSync("secret-tool", ["store", "--label=Grubless CLI", "service", SERVICE, "account", ACCOUNT], {
        input: token,
        stdio: ["pipe", "ignore", "ignore"],
      });
      return true;
    }
  } catch {
    return false;
  }
  return false;
}

function keychainClear(): void {
  try {
    if (platform() === "darwin") {
      execFileSync("security", ["delete-generic-password", "-s", SERVICE, "-a", ACCOUNT], { stdio: "ignore" });
    } else if (platform() === "linux") {
      execFileSync("secret-tool", ["clear", "service", SERVICE, "account", ACCOUNT], { stdio: "ignore" });
    }
  } catch {
    // Nothing stored — already in the desired state.
  }
}

// ---------- public ----------

export function loadConfig(overrides: { apiUrl?: string } = {}): CliConfig {
  const stored = readStored();

  const envToken = process.env.GRUBLESS_TOKEN?.trim();
  if (envToken) {
    return {
      apiUrl: overrides.apiUrl ?? process.env.GRUBLESS_API_URL ?? stored.apiUrl ?? DEFAULT_API_URL,
      token: envToken,
      tokenSource: "env",
    };
  }

  const fromKeychain = keychainGet();
  const token = fromKeychain ?? stored.token ?? null;

  return {
    apiUrl: overrides.apiUrl ?? process.env.GRUBLESS_API_URL ?? stored.apiUrl ?? DEFAULT_API_URL,
    token,
    tokenSource: token ? (fromKeychain ? "keychain" : "file") : null,
  };
}

/** Returns where the token ended up, so `login` can tell the user the truth. */
export function saveToken(token: string, apiUrl: string): "keychain" | "file" {
  const stored = readStored();

  if (keychainSet(token)) {
    // Don't leave a copy behind in the file — two places to revoke is one
    // too many, and the stale copy is the one that gets forgotten.
    writeStored({ apiUrl, token: undefined });
    return "keychain";
  }

  writeStored({ ...stored, apiUrl, token });
  return "file";
}

export function clearToken(): void {
  keychainClear();
  const stored = readStored();
  if (stored.token) writeStored({ apiUrl: stored.apiUrl, token: undefined });
}

/** For `logout --purge` and for tests: removes the config file entirely. */
export function removeConfigFile(): void {
  try {
    rmSync(configPath());
    const dir = dirname(configPath());
    rmSync(dir, { recursive: false });
  } catch {
    // Already gone, or the directory has other contents. Neither is a failure.
  }
}

export function configFilePath(): string {
  return configPath();
}
