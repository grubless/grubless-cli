import { createInterface } from "node:readline/promises";
import type { ApiToken, Entity } from "@grubless/api-types";
import { ApiClient } from "../client.js";
import { clearToken, loadConfig, saveToken, configFilePath } from "../config.js";
import { CliError, ExitCode, note, out, style, table } from "../output.js";

/**
 * `grubless auth …`
 *
 * There is deliberately no password login here. The CLI authenticates with an
 * API token created in the web UI, for the reasons in docs/plan-cli.md §2: a
 * CLI that handles passwords ends up storing a full-privilege browser session
 * on disk with no scope and no way to revoke it on its own. Pasting a token
 * once is a small amount of friction that buys named, scoped, individually
 * revocable credentials.
 */

const TOKEN_PAGE = "/app/settings/api-tokens";

export async function authLogin(args: { token?: string; apiUrl?: string }): Promise<number> {
  const config = loadConfig({ apiUrl: args.apiUrl });

  let token = args.token?.trim();
  if (!token) {
    if (!process.stdin.isTTY) {
      throw new CliError(
        "No token given and stdin isn't a terminal.\n" +
          "In a script, pass --token, or set GRUBLESS_TOKEN instead of logging in.",
        ExitCode.UsageError,
      );
    }
    note(`Create a token at ${config.apiUrl.replace(/^https:\/\/api\./, "https://")}${TOKEN_PAGE}`);
    note("");
    const rl = createInterface({ input: process.stdin, output: process.stderr });
    token = (await rl.question("Paste your API token: ")).trim();
    rl.close();
  }

  if (!token) throw new CliError("No token entered.", ExitCode.UsageError);

  // Verify before storing. Writing an invalid token to the keychain and only
  // discovering it on the next command is a needlessly confusing failure —
  // the user would reasonably assume login had worked.
  const client = new ApiClient({ apiUrl: config.apiUrl, token });
  const entities = await client.get<Entity[]>("/entities");

  const where = saveToken(token, config.apiUrl);
  note(
    style.green("✓") +
      ` Signed in — ${entities.length} ${entities.length === 1 ? "entity" : "entities"} available.\n` +
      `  Token stored in the ${where === "keychain" ? "system keychain" : `config file (${configFilePath()})`}.`,
  );
  return ExitCode.Ok;
}

export async function authLogout(): Promise<number> {
  clearToken();
  note(
    style.green("✓") +
      " Signed out locally.\n" +
      "  The token still exists server-side — revoke it at " +
      TOKEN_PAGE +
      " if it may have been exposed.",
  );
  return ExitCode.Ok;
}

export async function authWhoami(client: ApiClient, asJson: boolean): Promise<number> {
  const config = loadConfig();
  const [entities, tokens] = await Promise.all([
    client.get<Entity[]>("/entities"),
    // A read-scoped token can list tokens (it's a GET), so this is safe to
    // always fetch; it's the fastest way to tell someone WHICH credential
    // they're currently using.
    client.get<ApiToken[]>("/api-tokens").catch(() => [] as ApiToken[]),
  ]);

  if (asJson) {
    out(JSON.stringify({ apiUrl: config.apiUrl, tokenSource: config.tokenSource, entities, tokens }, null, 2));
    return ExitCode.Ok;
  }

  note(`API      ${config.apiUrl}`);
  note(`Token    from ${config.tokenSource ?? "nowhere — not signed in"}`);
  note("");
  if (entities.length === 0) {
    note("No entities available to this account.");
    return ExitCode.Ok;
  }
  table(entities, [
    { header: "ENTITY", value: (e) => e.name },
    { header: "TYPE", value: (e) => e.entityType },
    { header: "ROLE", value: (e) => e.role },
    { header: "ID", value: (e) => e.id },
  ]);
  return ExitCode.Ok;
}
