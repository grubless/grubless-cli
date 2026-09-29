# grubless

The command-line client for [Grubless](https://grubless.io) — crypto tax for
Australian and US entities.

A single static binary for Linux, macOS and Windows. It gives you two ways to
work with the entities your account can reach:

- **`grubless`** on its own opens an **interactive terminal interface**: the
  web dashboard's overview, holdings, warnings, tax, sources and transactions,
  in your terminal.
- **`grubless <command>`** is **scriptable**: pipeable output, JSON on
  request, and exit codes that mean something, for CI and bulk work. A TUI
  can't be piped, so the two coexist.

## Features

- **Every client's reports in one command.** Pull a report for every entity
  you can reach into one folder per client, or as one JSON document.
- **A pre-filing gate for CI.** `warnings --fail-on-blocking` exits non-zero
  while there are issues that would make filed figures wrong.
- **Transactions, filtered.** By category, direction, date range, source,
  asset or text, from the command line or a filter form in the TUI, with
  every leg's exact value, proceeds, cost basis and gain or loss.
- **The web dashboard in a terminal.** A portfolio chart in braille, the
  activity breakdown, headline figures, and live sync progress, laid out in
  cards like the web app, in its Cypherpunk theme.
- **Syncs and imports you can wait on.** Queue a sync or a CSV import and
  watch the server's own progress messages until it finishes.
- **Exact figures.** Every amount is the server's decimal string, formatted
  without ever passing through floating point; `--json` passes it through
  untouched.
- **Safe to hold a powerful token.** API tokens only, stored in your system
  keychain; no dependencies beyond Go and two Go-team modules; no tax logic
  in the client at all.

---

## Install

Download the binary for your platform from the
[latest release](https://github.com/grubless/grubless-cli/releases/latest):

| Platform | File |
|---|---|
| macOS, Apple silicon | `grubless-<version>-darwin-arm64` |
| macOS, Intel | `grubless-<version>-darwin-amd64` |
| Linux, x86-64 | `grubless-<version>-linux-amd64` |
| Linux, ARM64 | `grubless-<version>-linux-arm64` |
| Windows, x86-64 | `grubless-<version>-windows-amd64.exe` |

Check it against the release's `SHA256SUMS`, then put it on your `PATH`:

```bash
curl -LO https://github.com/grubless/grubless-cli/releases/download/v0.2.0/grubless-0.2.0-darwin-arm64
curl -LO https://github.com/grubless/grubless-cli/releases/download/v0.2.0/SHA256SUMS
shasum -a 256 -c --ignore-missing SHA256SUMS     # Linux: sha256sum --check --ignore-missing SHA256SUMS
install -m 755 grubless-0.2.0-darwin-arm64 /usr/local/bin/grubless
grubless --version
```

The binaries aren't code-signed yet. On macOS, Gatekeeper blocks a downloaded
one until you allow it (System Settings → Privacy & Security, or
`xattr -d com.apple.quarantine /usr/local/bin/grubless`); on Windows,
SmartScreen warns before the first run.

Or install it with Go 1.26+:

```bash
go install github.com/grubless/grubless-cli/cmd/grubless@latest
```

To build from a checkout, see [Development](#development).

## Quick start

1. Create an API token in the web app under **Settings → API tokens**.
   Read-only is the default, and it's all anything but syncing and importing
   needs.
2. Sign in, and see what you can reach:

   ```bash
   grubless auth login          # paste the token when asked
   grubless entities list
   ```

3. Open the interactive interface, or run a command:

   ```bash
   grubless
   grubless holdings --entity acme
   grubless report capital-gains --entity acme --year 2025 --out cg.csv
   ```

`--entity` takes an entity's id, its exact name, or any unambiguous prefix of
its name, so `acme` is enough for "Acme Trading Pty Ltd".

---

## Interactive mode

```bash
grubless
```

Pick an entity, then move between six tabs:

| Tab | What it shows |
|---|---|
| **1 Overview** | The portfolio chart (value, income and unrealised gain over time), the activity breakdown by category, and cards for connected sources, transaction count, last sync and price coverage. `[` `]` change the time range. |
| **2 Holdings** | Current positions, where each is held, quantity and value, with reconciliation mismatches flagged. Spam tokens are hidden. |
| **3 Warnings** | The blocking issues: disposals with no cost basis, and uncategorised transfers. |
| **4 Tax** | The tax position for each financial year. |
| **5 Sources** | Each connected source, its transaction count, when it last synced, and its status. |
| **6 Transactions** | Every transaction, newest first, loading more as you scroll. **⏎** opens one with all of its legs. **f** filters. |

**Keys**

| | |
|---|---|
| `↑ ↓` / `k j` | move |
| `PgUp` `PgDn`, `Home` `End` | page / jump |
| `⏎` | open the selected entity, or the selected transaction |
| `↹` / `← →` / `h l`, or `1`–`6` | switch tab |
| `[` `]` | narrow / widen the chart's time range |
| `f` or `/` | filter transactions |
| `r` | reload the entity |
| `s` | sync every enabled source, and watch its progress |
| `t` | switch theme |
| `?` | help |
| `q` / `Esc` | back to the entity list, or quit from there |
| `Ctrl-C` | quit immediately |

**Filtering transactions.** `f` opens a form with a field for every filter:
category (with suggestions as you type — `→` takes the one shown), direction,
from and to dates, source, asset, free text and sort order. `Enter` applies,
`Esc` cancels, `Ctrl-U` clears a field, and "Clear all filters" resets them.
The status bar counts the filters in force.

**Themes.** The default is **Cypherpunk**, the web app's theme, in its exact
colours. **Terminal** uses your terminal's own colours instead. `t` switches
and remembers the choice; `GRUBLESS_THEME=cypher` or `terminal` overrides it.
With `NO_COLOR` set, or on a terminal with fewer than 256 colours, the default
is Terminal.

The TUI needs a terminal. Run it in a pipe or a script and it says so and
exits, rather than writing escape codes into your output.

---

## Commands

```
grubless auth login|logout|whoami
grubless entities list

grubless holdings      --entity <id|name>
grubless portfolio     --entity <id|name> [--range 24h|1w|1m|3m|6m|1y|fy|all]
grubless tax-summary   --entity <id|name> [--year 2025]
grubless warnings      --entity <id|name> [--fail-on-blocking]
grubless transactions  --entity <id|name> [filters] [--limit n|all]

grubless sources list  --entity <id|name>
grubless sources sync  --entity <id|name> (--source <id|label> | --all) [--full] [--wait]
grubless import <file.csv> --entity <id|name> --source <id> [--wait]

grubless report <name> --entity <id|name> --year 2025 [--out <path>]
grubless report <name> --all-entities --year 2025 (--out <dir> | --json)
```

Options that apply to most commands:

| | |
|---|---|
| `--entity <id\|name>` | The entity: an id, an exact name, or an unambiguous name prefix. An ambiguous prefix is an error, never a guess. |
| `--all-entities` | Every entity your account can reach, instead of one. |
| `--json` | Machine-readable output on stdout (see [JSON output](#json-output)). |
| `--api-url <url>` | Talk to a different API endpoint. |
| `-h`, `--help` / `-v`, `--version` | Help, or the version. |

### auth

```bash
grubless auth login                   # prompts for the token
grubless auth login --token grb_…     # or give it directly, e.g. in a script
grubless auth whoami                  # which API, where the token came from, and your entities
grubless auth logout                  # forget the stored token
```

`logout` forgets the token on this machine only. Revoke it in the web app if
it may have been exposed.

### entities

```bash
grubless entities list
grubless entities list --json
```

### holdings

Current positions: asset, where it's held (its chain, or for an exchange
balance, the source), quantity and value. A position whose quantity
disagrees with what the source itself reports is flagged `mismatch`. Spam
tokens are left out of the table; `--json` includes everything.

```bash
grubless holdings --entity acme
grubless holdings --all-entities
```

### portfolio

Value, cumulative income and unrealised gain over time, drawn in braille:

```bash
grubless portfolio --entity acme --range 1y
grubless portfolio --entity acme --json | jq '.points[-1]'
```

`--range` is `24h`, `1w`, `1m`, `3m`, `6m`, `1y`, `fy` (this financial year)
or `all`. The command defaults to `all` — a pipe gets everything unless told
otherwise — where the TUI and the web dashboard open on the financial year.
`--json` gives every daily point, with the range echoed back.

### tax-summary

Income, expenses, net capital gain, taxable amount and tax for each financial
year. A pass-through entity (a trust or partnership) shows "n/a" rather than
a tax figure it doesn't pay.

```bash
grubless tax-summary --entity acme
grubless tax-summary --entity acme --year 2025
```

`--year` is the calendar year the financial year *starts* in: `--year 2025`
is FY2025–26 for a July-start entity.

### warnings

The data-quality issues that stand between an entity and a clean filing:

- **Blocking:** disposals with no substantiated cost basis (the gain is
  computed against zero, over-reporting tax), and uncategorised transfers
  (which may be internal movements being taxed as disposals).
- **Advisory:** unpriced assets, and unbalanced transfers. Reported, but they
  don't make the figures wrong.

```bash
grubless warnings --entity acme
grubless warnings --entity acme --fail-on-blocking     # exit 4 if anything is blocking
```

### transactions

An entity's transactions, newest first: date and time (UTC), type, what went
out and came in, fees, realised gain or loss, source and tags. A `*` after
the type marks one categorised by hand.

```bash
grubless transactions --entity acme
grubless transactions --entity acme --category transfer,send --direction out
grubless transactions --entity acme --asset SOL --from 2025-07-01 --to 2026-06-30
grubless transactions --entity acme --source Kraken --search jupiter --sort asc
grubless transactions --entity acme --limit all --json > transactions.json
```

| Filter | |
|---|---|
| `--category <a,b,…>` | Event types, comma-separated, e.g. `transfer,send`. A type that doesn't exist gets the API's list of the ones that do. |
| `--direction in\|out` | Only transactions with a leg moving that way. |
| `--from <date>`, `--to <date>` | ISO dates or timestamps. |
| `--source <id\|label>` | One source, by id or by its label. |
| `--asset <id\|symbol>` | One asset. A symbol must be one the entity holds; one held on several chains (USDC, say) is refused with the list, so you can pass the id. |
| `--search <text>` | Free-text search. |
| `--sort asc\|desc` | Oldest or newest first. Newest is the default. |
| `--limit <n\|all>` | How many. 50 by default; `all` pages through everything. |

When more match than were shown, stderr says so.

### sources and import

```bash
grubless sources list --entity acme
grubless sources sync --entity acme --all --wait         # sync every enabled source, and wait
grubless sources sync --entity acme --source Kraken --full
grubless import trades.csv --entity acme --source <source-id> --wait
```

`sources sync` and `import` queue the work and return straight away. With
`--wait` they block until it finishes, printing the server's own progress as
it goes, and exit 1 if a job failed. `--full` re-fetches a source's whole
history rather than what's new since the last sync.

A full resync of a large exchange account can legitimately take **~30
minutes** under API throttling, so `--wait` allows 35 by default. Change it
with `--timeout <minutes>`. Timing out stops watching; it doesn't stop the
job.

Syncing and importing need a write-scoped token.

### report

```bash
grubless report capital-gains --entity acme --year 2025 > cg.csv
grubless report capital-gains --entity acme --year 2025 --out cg.csv
grubless report capital-gains --all-entities --year 2025 --out ./clients/
grubless report capital-gains --entity acme --year 2025 --json | jq '.rows[0]'
grubless report bundle --entity acme --year 2025 --out acme-fy2025.zip
```

| Report | |
|---|---|
| `capital-gains` | Every CGT disposal — proceeds, cost base, days held, and discounted gain or loss |
| `income` | Income events (staking, interest, airdrops and so on) at market value on receipt |
| `expenses` | Deductible business spending (crypto spent on expenses) |
| `fees` | Every fee paid, per transaction |
| `buy-sell` | Plain buy and sell transactions |
| `other-gains` | Realised gains outside ordinary buys and sells (derivatives, reward disposals) |
| `gifts-donations-lost` | Gifts given, donations, and lost or stolen assets |
| `transaction-history` | Every transaction — sent, received, and value |
| `beginning-of-year-holdings`, `end-of-year-holdings` | Holdings at the start or end of the financial year |
| `highest-balance` | Each asset's peak balance during the year |
| `balances-per-source` | Each asset's current balance per source (not year-specific; no `--year` needed) |
| `ato-mytax` | The myTax label figures for the year, as a PDF (Australian entities) |
| `division-70-trading-stock` | The trading-stock schedule across all years, as a PDF (Australian business entities) |
| `bundle` | Every report, as one ZIP |

- Without `--out`, the report goes to stdout.
- `--all-entities` needs `--out <dir>` — one folder per entity, each file
  named as the server suggests — or `--json`. Files are never written outside
  `<dir>`.
- `--json` turns a CSV report into rows (not for the PDFs or the bundle).
- If one entity's report fails, the others still run; the failure is
  reported and the exit code is 1. A "paid plan required" answer stops the
  run (exit 5), since it would be the same for every entity.

---

## Scripting and CI

**Every client's reports, in one command.** If you prepare returns for
several entities:

```bash
grubless report capital-gains --all-entities --year 2025 --out ./clients/
```

One folder per entity, one login, every client. Your access is whatever your
roles on those entities already grant — the CLI adds no permissions of its
own.

**Gate a filing in CI.**

```bash
grubless warnings --entity acme --fail-on-blocking
```

Exits 4 while anything is blocking, and 0 once the entity is clean.

**A nightly sync and report pull.**

```bash
export GRUBLESS_TOKEN=grb_…          # a write-scoped token, since it syncs
grubless sources sync --all-entities --all --wait
grubless report bundle --all-entities --year 2025 --out "./reports/$(date +%F)/"
```

**Pick things out with jq.**

```bash
grubless holdings --entity acme --json | jq '.holdings[] | select(.hasMismatch)'
grubless transactions --entity acme --category staking_reward --limit all --json | jq '.events | length'
grubless report capital-gains --all-entities --year 2025 --json | jq '.[] | {entity: .entity.name, rows: (.rows | length)}'
```

### JSON output

stdout carries only the JSON; progress and warnings stay on stderr. Figures
are the server's exact decimal strings, and every field the server sends is
passed through, including ones this client doesn't use.

| Command | With `--entity` | With `--all-entities` |
|---|---|---|
| `entities list`, `auth whoami` | — | one document |
| `portfolio`, `transactions`, `report`, `sources sync` | one object | an array of them |
| `holdings`, `tax-summary` | one object | one object per entity, one after another (`jq -s` collects them) |
| `warnings` | one object | an array — but a single object if the account has only one entity |
| `sources list` | an array of sources | one array of every entity's sources, each with its `entityName` |

`portfolio` and `transactions` echo the range or filters they used, so a
result can't be mistaken for a narrower question than it answered.
`transactions` also includes a `nextCursor` when more remain.

### Exit codes

| | |
|---|---|
| `0` | Success |
| `1` | The operation ran and failed |
| `2` | Usage error — a bad flag, a missing argument, an unknown entity |
| `3` | Not signed in, or the token was rejected (or is read-only for this) |
| `4` | `--fail-on-blocking` found blocking issues |
| `5` | A paid plan is required |

`4` and `5` are kept apart from `1` on purpose: in CI, "this filing isn't
clean yet" and "your subscription lapsed" need different handling from "the
command broke".

---

## Authentication and configuration

There's no password login. Tokens are named, scoped and individually
revocable; a stored password-derived session is none of those things.

The token is looked for in this order:

1. **`GRUBLESS_TOKEN`**, which always wins — use it in CI, where there's no
   keychain and no one to log in.
2. **The system keychain** — macOS Keychain, or libsecret (`secret-tool`) on
   Linux — where `auth login` stores it when it can.
3. **`~/.config/grubless/config.json`** (or under `$XDG_CONFIG_HOME`), mode
   `0600`, when there's no keychain — in containers, on headless Linux
   without libsecret, and on Windows. The CLI warns if other users can read
   it.

`auth whoami` says which one it's using.

**Use a read-scoped token for anything scheduled.** A nightly report pull has
no reason to be able to delete a source. A read-scoped token trying to sync
gets a message saying it's a token-scope problem, not a permissions one.

The config file also holds the API endpoint, if you've set one, and the TUI's
theme.

## Output

stdout is data and stderr is commentary: progress, warnings and errors never
end up in a pipe. Colour is used only when stdout is a terminal, and never
with `NO_COLOR` set.

If the server needs a newer CLI than the one you're running, you'll see a
warning on stderr, but the command still runs — a raised minimum mustn't
break a nightly job.

## Environment

| | |
|---|---|
| `GRUBLESS_TOKEN` | API token; wins over anything stored |
| `GRUBLESS_API_URL` | API endpoint |
| `GRUBLESS_THEME` | TUI theme: `cypher` (the default) or `terminal` |
| `NO_COLOR` | Disable colour |

---

## Development

```bash
go test ./...              # everything, including the scenario and TUI tests
go test -short ./...       # without the --wait scenarios and the TUI session
go build ./cmd/grubless
```

### What this repo is

A thin HTTP client, and nothing more. Every figure it prints arrives from the
Grubless API as a finished value — there is no tax logic here, and there is
not meant to be. The server that computes those figures is not open source;
this is the client you talk to it with. `internal/guard` enforces that on
every test run: it fails if any of the tax engine's symbols appear in the
source.

### Dependencies

The Go standard library, plus `golang.org/x/term` and `golang.org/x/sys` from
the Go team (raw mode and the window size, for the TUI). This binary holds a
token that can read a firm's entire client list, so every module it links is
another party that could reach that token. `internal/guard` fails if `go.mod`
requires anything else. A pull request that adds a dependency needs to argue
that case, and add it there.

### Tests

- **`internal/scenarios`** runs the built binary against a stub API across
  ~125 scenarios and compares everything observable — stdout, stderr, exit
  code, the requests sent, the config file, every file `--out` writes — with
  recordings in `testdata/`. Several scenarios try to write outside `--out`
  and fail the test outright if one succeeds. On Linux it also records colour
  output on a pseudo-terminal, and drives the TUI key by key. After an
  intended change, `go test ./internal/scenarios -update`, then read the
  diff before committing it.
- **Golden files** in several packages hold exact expected outputs for the
  formatters, the CSV reader, the chart, key decoding and TUI rendering over
  thousands of inputs. See `internal/golden`.
- **`internal/e2e`** drives the built binary against a real Grubless API,
  set up through public routes only. It's skipped unless pointed at one:

  ```bash
  GRUBLESS_E2E_API_URL=http://localhost:3000 go test ./internal/e2e -v
  ```

  It leaves a throwaway account and one entity behind — there's no
  account-deletion route to clean up with. Point it at a stack you don't
  mind that happening to.

### Releasing

Push a version tag:

```bash
git tag v0.2.0 && git push origin v0.2.0
```

`.github/workflows/release.yml` runs the tests, builds a static binary for
each target with the tag's version stamped in, and creates the GitHub release
with the binaries and a `SHA256SUMS` file attached. It marks where signing
goes once there are certificates for it: an Apple Developer ID with
notarisation, and a Windows Authenticode certificate.

### Wire types

`internal/api` is this client's *declared* view of the API's shapes, kept
deliberately as a copy rather than imported from the server. A CLI is
separately versioned from the deployment it talks to, so compiling against the
server's own declarations would only prove agreement with a server you aren't
talking to. What catches real drift is the e2e run above; what tells a user
about it is the `x-grubless-min-cli-version` handshake, which warns on stderr
without failing the command — a raised floor must not break someone's nightly
job at 2am.

### History

The CLI was first written in TypeScript, as a Node package. It was ported
to Go and held to the TypeScript build byte
for byte — output, exit codes, files written — before that build was removed;
comments that say "the TS" refer to it, and its source is in the git history.
The scenario recordings and most golden files began as its behaviour. Where
the port had to work around a difference between Go and Node (JSON key order
and escaping, UTF-16 string widths, `toFixed` rounding, BOM handling, date
parsing, Node's file-error wording), the code says so where it happens.

Known issues carried over from the TypeScript build:

- `auth whoami --api-url X` queries X but reports the configured URL.
- **End** on a TUI tab sets the entity picker's position from that tab's
  last row, so back on the picker **Enter** can open nothing.
- The TUI's sync watcher treats "queued" as finished, and watches every
  activity row rather than the ones it started, so a stale "running" job
  keeps it polling.
- `--out` folder names drop every non-ASCII letter ("Société" becomes
  `Soci-t`).

## Licence

MIT. See [LICENSE](./LICENSE).
