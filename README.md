# grubless

Command-line client for [Grubless](https://grubless.io) — crypto tax for
Australian and US entities.

A single static binary for Linux, macOS (Intel and Apple silicon) and Windows,
with nothing else to install. Download the one for your platform from the
[releases page](https://github.com/grubless/grubless-cli/releases), check it
against the release's `SHA256SUMS`, and put it on your `PATH`:

```bash
curl -LO https://github.com/grubless/grubless-cli/releases/download/v0.2.0/grubless-0.2.0-darwin-arm64
curl -LO https://github.com/grubless/grubless-cli/releases/download/v0.2.0/SHA256SUMS
sha256sum --check --ignore-missing SHA256SUMS      # macOS: shasum -a 256 -c --ignore-missing
install -m 755 grubless-0.2.0-darwin-arm64 /usr/local/bin/grubless
```

The binaries aren't code-signed yet. On macOS, Gatekeeper blocks a
downloaded one until you allow it (System Settings → Privacy & Security, or
`xattr -d com.apple.quarantine /usr/local/bin/grubless`); on Windows,
SmartScreen warns before the first run.

Or build it with Go 1.26+:

```bash
go install github.com/grubless/grubless-cli/cmd/grubless@latest
```

The CLI used to be the npm package `@grubless/cli`. That package is no longer
updated; this binary replaces it, with the same commands, flags, output and
exit codes.

Two ways to use it, and they coexist deliberately:

- **`grubless`** with no arguments opens an interactive terminal interface —
  browse entities, check holdings, warnings and transactions, watch a sync
  run live.
- **`grubless <command>`** is scriptable — pipeable output, real exit codes,
  built for CI and bulk work. A TUI can't be piped, so it doesn't replace this.

---

## Interactive mode

```bash
grubless
```

Pick an entity, then move between **Overview**, **Holdings**, **Warnings**,
**Tax**, **Sources** and **Transactions**. It's laid out like the web app, in
cards: the Overview has the portfolio chart, the activity breakdown by
category, and headline figures for sources, transactions, last sync and price
coverage.

| | |
|---|---|
| `↑ ↓` / `k j` | move |
| `PgUp` `PgDn`, `Home` `End` | page / jump |
| `⏎` | open the selected entity, or the selected transaction |
| `↹` / `← →` / `h l`, or `1`–`6` | switch tab |
| `[` `]` | narrow / widen the chart's time range |
| `f` or `/` | filter transactions (Transactions tab) |
| `r` | reload |
| `s` | sync every enabled source, and watch it |
| `t` | switch theme |
| `?` | help |
| `q` / `Esc` | back to entities, or quit from there |
| `Ctrl-C` | quit immediately |

**Transactions** loads a page at a time as you scroll. **⏎** opens one with
every leg's exact value, proceeds, cost basis and gain/loss. **f** filters by
category (with suggestions as you type), direction, dates, source, asset and
text.

**Themes.** The default is **Cypherpunk**, the web app's theme, with its
colours exactly. It falls back to **Terminal**, your terminal's own colours,
when `NO_COLOR` is set or the terminal has fewer than 256 colours. `t`
switches and remembers the choice; `GRUBLESS_THEME=cypher|terminal`
overrides it.

---

## Every client's reports, in one command

If you prepare returns for several entities, this is the command that matters:

```bash
grubless report capital-gains --all-entities --year 2025 --out ./clients/
```

One directory per entity, one login, every client. Your access is whatever
your `entity_members` roles already grant — the CLI adds no permissions of its
own.

## Gate a filing in CI

```bash
grubless warnings --entity "Node Integration" --fail-on-blocking
```

Exits **4** when there are unresolved issues that would make the figures
wrong if you filed as-is — disposals with no substantiated cost basis, and
uncategorised transfers that may be internal movements being taxed as
disposals. Advisory issues (unpriced assets, unbalanced transfers) are
reported but don't fail the run.

---

## Authentication

Create a token at **Settings → API tokens** in the web app, then:

```bash
grubless auth login
```

The token is stored in your system keychain where one is available (macOS
Keychain, or libsecret on Linux), and in `~/.config/grubless/config.json`
(mode `0600`) otherwise.

In CI, skip the login and set the environment variable — it takes precedence
over anything stored locally:

```bash
export GRUBLESS_TOKEN=grb_…
```

**Use a read-scoped token** for anything scheduled. A nightly report pull has
no reason to be able to delete a source, and read-only is the default when you
create one.

There is no password login. Tokens are named, scoped, and individually
revocable; a stored password-derived session is none of those things.

## Commands

```
grubless auth login|logout|whoami

grubless entities list

grubless sources list       --entity <id|name>
grubless sources sync       --entity <id|name> (--source <id> | --all) [--full] [--wait]
grubless import <file.csv>  --entity <id|name> --source <id> [--wait]

grubless holdings           --entity <id|name>
grubless portfolio          --entity <id|name> [--range 24h|1w|1m|3m|6m|1y|fy|all]
grubless tax-summary        --entity <id|name> [--year 2025]
grubless warnings           --entity <id|name> [--fail-on-blocking]
grubless transactions       --entity <id|name> [filters] [--limit n|all]

grubless report <name>      --entity <id|name> --year 2025 [--out <path>]
grubless report <name>      --all-entities --year 2025 --out <dir>
```

`--entity` takes a uuid, an exact name, or an unambiguous name prefix. An
ambiguous prefix is an error rather than a guess — quietly picking the wrong
client's entity would produce a confident, correct-looking, completely wrong
report.

Add `--all-entities` to most commands to run across everything you can reach.
With `--json`, `--all-entities` is always an array and `--entity` always one
object, however many entities the account holds.

### Reports

`capital-gains`, `income`, `fees`, `expenses`, `buy-sell`,
`gifts-donations-lost`, `other-gains`, `transaction-history`,
`balances-per-source`, `beginning-of-year-holdings`, `end-of-year-holdings`,
`highest-balance`, `division-70-trading-stock`, `ato-mytax`, and `bundle`
(a ZIP of all of them).

`--year` is the calendar year the financial year *starts* in — `--year 2025`
is FY2025–26 for a July-start entity.

Without `--out`, the CSV goes to stdout:

```bash
grubless report capital-gains --entity acme --year 2025 > cg.csv
grubless holdings --entity acme --json | jq '.[] | select(.hasMismatch)'
```

With `--all-entities --out <dir>`, each file is named as the server suggests,
inside a folder per entity — and never outside `<dir>`: a path in a
server-sent filename is dropped, with a warning.

### Transactions

```bash
grubless transactions --entity acme --category transfer,send --direction out
grubless transactions --entity acme --asset SOL --from 2025-07-01 --to 2026-06-30
grubless transactions --entity acme --limit all --json > txs.json
```

Filters: `--category` (comma-separated event types; a bad one gets the API's
list of valid ones back), `--direction in|out`, `--from`/`--to`, `--source`
(id or label), `--asset` (id, or a symbol the entity holds — a symbol held on
several chains is refused rather than guessed), `--search`, `--sort asc|desc`.
`--limit` defaults to 50; `all` pages through everything. `--json` gives the
events and assets as the server sent them, the filters that produced them,
and a `nextCursor` when more remain.

### Portfolio

```bash
grubless portfolio --entity acme --range 1y
```

Draws value, unrealised gain and cumulative income over time, in braille, in
your terminal. `--json` gives you the series instead, with the range echoed
back — a filtered array with no record of the filter can't tell a quiet year
apart from a narrow window.

Unlike the interactive interface and the web dashboard, which open on the
current financial year, the command defaults to `all`: something reading into a
pipe gets everything unless told otherwise.

### Waiting for syncs

`sources sync` and `import` queue work and return immediately. `--wait` blocks
until it finishes, printing the server's own progress messages.

A full resync of a large exchange account can legitimately take **~30
minutes** under API throttling, so `--wait` allows 35 by default. Adjust with
`--timeout <minutes>`. Timing out stops watching; it does not stop the job.

## Output

stdout is data, stderr is commentary — progress, warnings and errors never
pollute a pipe. Colour is disabled automatically when stdout isn't a terminal,
and by `NO_COLOR`.

## Exit codes

| | |
|---|---|
| `0` | Success |
| `1` | The operation ran and failed |
| `2` | Usage error — bad flag or missing argument |
| `3` | Not authenticated, or the token was rejected |
| `4` | `--fail-on-blocking` found blocking issues |
| `5` | A paid plan is required |

`4` and `5` are separated from `1` on purpose: in CI, "this filing isn't clean
yet" and "your subscription lapsed" need different handling from "the command
broke".

## Environment

| | |
|---|---|
| `GRUBLESS_TOKEN` | API token; wins over stored config |
| `GRUBLESS_API_URL` | API endpoint |
| `GRUBLESS_THEME` | Interface theme: `cypher` (default) or `terminal` |
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

The CLI was first written in TypeScript and published to npm as
`@grubless/cli`. It was ported to Go and held to the TypeScript build byte
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
