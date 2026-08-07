import { parseArgs } from "node:util";
import { ApiClient, CLI_VERSION } from "./client.js";
import { loadConfig } from "./config.js";
import { CliError, ExitCode, note, out } from "./output.js";
import { authLogin, authLogout, authWhoami } from "./commands/auth.js";
import { entitiesList } from "./commands/entities.js";
import { portfolio } from "./commands/portfolio.js";
import { holdings, sourcesImport, sourcesList, sourcesSync } from "./commands/sources.js";
import { taxSummary } from "./commands/tax.js";
import { warnings } from "./commands/warnings.js";
import { REPORTS, report } from "./commands/reports.js";

/**
 * Entry point and command dispatch.
 *
 * Argument parsing is Node's own `util.parseArgs` — no dependency. That is a
 * deliberate posture, not minimalism for its own sake: this binary holds a
 * token that can reach an accounting firm's entire client list, so every
 * transitive package in it is supply-chain surface pointed at exactly the
 * wrong thing. Node 20 gives us `fetch` and `parseArgs`, which is all this
 * needs, so the published package declares **zero** runtime dependencies.
 */

const USAGE = `grubless ${CLI_VERSION} — crypto tax for entities

USAGE
  grubless <command> [options]

COMMANDS
  auth login [--token <t>]      Authenticate with an API token
  auth logout                   Forget the stored token
  auth whoami                   Show the current account and its entities

  entities list                 List entities this account can reach

  sources list                  List an entity's sources
  sources sync                  Queue a sync (--source <id> | --all)
  import <file.csv>             Upload a CSV into a csv_import source

  holdings                      Current positions, with reconciliation flags
  portfolio                     Value over time (a chart, or --json for the series)
  tax-summary                   Per-financial-year tax position
  warnings                      Data-quality issues blocking a clean filing

  report <name>                 Download a report (see REPORTS below)

COMMON OPTIONS
  --entity <id|name>            Target entity; name may be an unambiguous prefix
  --all-entities                Every entity this account can reach
  --json                        Machine-readable output on stdout
  --api-url <url>               Override the API endpoint
  -h, --help                    Show this help
  -v, --version                 Show the version

REPORT OPTIONS
  --year <startYear>            Financial year, e.g. 2025
  --out <path|dir>              Write to a file, or a directory with --all-entities
  --json                        Rows as JSON on stdout, including --all-entities
                                (not for bundle / ato-mytax / division-70: PDF and ZIP)

SYNC / IMPORT OPTIONS
  --wait                        Block until the queued work finishes
  --full                        Re-fetch entire history, ignoring last sync
  --timeout <minutes>           How long --wait waits (default 35)

WARNINGS OPTIONS
  --fail-on-blocking            Exit ${ExitCode.BlockingWarnings} if blocking issues exist (CI gate)

REPORTS
  ${REPORTS.join(", ")}, bundle

ENVIRONMENT
  GRUBLESS_TOKEN                API token; takes precedence over stored config
  GRUBLESS_API_URL              Default API endpoint
  NO_COLOR                      Disable colour

EXIT CODES
  ${ExitCode.Ok} ok   ${ExitCode.Failure} failed   ${ExitCode.UsageError} usage   ${ExitCode.AuthFailure} auth   ${ExitCode.BlockingWarnings} blocking warnings   ${ExitCode.UpgradeRequired} upgrade required

EXAMPLES
  # Every client's capital gains for FY2025, one directory per entity
  grubless report capital-gains --all-entities --year 2025 --out ./clients/

  # The same data as one JSON document on stdout — for jq, or an LLM tool
  grubless report capital-gains --all-entities --year 2025 --json | jq '.[].entity.name'

  # Pre-filing gate for CI
  grubless warnings --entity "Node Integration" --fail-on-blocking

  # Sync everything and wait for it
  grubless sources sync --entity acme --all --wait
`;

const OPTIONS = {
  entity: { type: "string" },
  "all-entities": { type: "boolean" },
  source: { type: "string" },
  all: { type: "boolean" },
  full: { type: "boolean" },
  wait: { type: "boolean" },
  timeout: { type: "string" },
  year: { type: "string" },
  out: { type: "string" },
  token: { type: "string" },
  "api-url": { type: "string" },
  json: { type: "boolean" },
  "fail-on-blocking": { type: "boolean" },
  help: { type: "boolean", short: "h" },
  version: { type: "boolean", short: "v" },
} as const;

async function main(argv: string[]): Promise<number> {
  let parsed;
  try {
    parsed = parseArgs({ args: argv, options: OPTIONS, allowPositionals: true, strict: true });
  } catch (err) {
    // parseArgs throws on an unknown flag. Its message is decent; the usage
    // hint is what makes it actionable.
    throw new CliError(`${err instanceof Error ? err.message : String(err)}\n\nRun \`grubless --help\`.`, ExitCode.UsageError);
  }

  const { values, positionals } = parsed;

  if (values.version) {
    out(CLI_VERSION);
    return ExitCode.Ok;
  }
  if (values.help) {
    out(USAGE);
    return ExitCode.Ok;
  }

  // No command: open the interactive interface on a terminal, print usage
  // otherwise. The TUI and the scriptable commands are deliberately both
  // present — a TUI can't be piped, so the bulk report export and the CI
  // warnings gate would be lost if it replaced them.
  if (positionals.length === 0 && !process.stdin.isTTY) {
    note(USAGE);
    return ExitCode.UsageError;
  }

  const timeoutMinutes = values.timeout ? Number(values.timeout) : undefined;
  if (values.timeout && (!Number.isFinite(timeoutMinutes) || timeoutMinutes! <= 0)) {
    throw new CliError(`--timeout must be a positive number of minutes, got "${values.timeout}".`, ExitCode.UsageError);
  }

  const common = {
    entity: values.entity,
    allEntities: values["all-entities"],
    json: values.json,
  };

  const [command, sub, ...rest] = positionals;

  // `auth login` and `auth logout` are the only commands that work without an
  // existing credential — everything else builds a client and will 401 with a
  // message pointing at login.
  if (command === "auth" && sub === "login") return authLogin({ token: values.token, apiUrl: values["api-url"] });
  if (command === "auth" && sub === "logout") return authLogout();

  const config = loadConfig({ apiUrl: values["api-url"] });
  if (!config.token) {
    throw new CliError(
      "Not signed in.\nRun `grubless auth login`, or set GRUBLESS_TOKEN.",
      ExitCode.AuthFailure,
    );
  }
  const client = new ApiClient({ apiUrl: config.apiUrl, token: config.token });

  if (positionals.length === 0) {
    const { runTui } = await import("./tui/app.js");
    return runTui(client);
  }

  switch (command) {
    case "auth":
      if (sub === "whoami") return authWhoami(client, Boolean(values.json));
      throw new CliError(`Unknown: auth ${sub ?? ""}. Try login, logout or whoami.`, ExitCode.UsageError);

    case "entities":
      if (!sub || sub === "list") return entitiesList(client, Boolean(values.json));
      throw new CliError(`Unknown: entities ${sub}. Only \`list\` exists today.`, ExitCode.UsageError);

    case "sources":
      if (!sub || sub === "list") return sourcesList(client, common);
      if (sub === "sync")
        return sourcesSync(client, {
          ...common,
          source: values.source,
          all: values.all,
          full: values.full,
          wait: values.wait,
          timeoutMinutes,
        });
      throw new CliError(`Unknown: sources ${sub}. Try list or sync.`, ExitCode.UsageError);

    case "import": {
      const file = sub;
      if (!file) throw new CliError("Specify the file to import: `grubless import <file.csv>`.", ExitCode.UsageError);
      return sourcesImport(client, file, {
        entity: values.entity,
        source: values.source,
        wait: values.wait,
        timeoutMinutes,
        json: values.json,
      });
    }

    case "holdings":
      return holdings(client, common);

    case "portfolio":
      return portfolio(client, common);

    case "tax-summary":
      return taxSummary(client, { ...common, year: values.year });

    case "warnings":
      return warnings(client, { ...common, failOnBlocking: values["fail-on-blocking"] });

    case "report": {
      if (!sub) {
        throw new CliError(
          `Specify a report: ${[...REPORTS, "bundle"].join(", ")}`,
          ExitCode.UsageError,
        );
      }
      if (rest.length > 0) {
        throw new CliError(`Unexpected argument "${rest[0]}". One report at a time.`, ExitCode.UsageError);
      }
      return report(client, sub, { ...common, year: values.year, out: values.out });
    }

    default:
      throw new CliError(`Unknown command "${command}".\n\nRun \`grubless --help\`.`, ExitCode.UsageError);
  }
}

main(process.argv.slice(2))
  .then((code) => {
    process.exitCode = code;
  })
  .catch((err) => {
    if (err instanceof CliError) {
      // Already phrased for a human — no stack trace, which is noise to
      // everyone who isn't debugging the CLI itself.
      note(err.message);
      process.exitCode = err.exitCode;
      return;
    }
    note(err instanceof Error ? (err.stack ?? err.message) : String(err));
    process.exitCode = ExitCode.Failure;
  });
