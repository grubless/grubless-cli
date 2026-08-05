# `@grubless/cli`

Command-line client for [Grubless](https://grubless.io) — crypto tax for
Australian and US entities.

```bash
npm install -g @grubless/cli
```

Requires Node 20+. Zero runtime dependencies.

Two ways to use it, and they coexist deliberately:

- **`grubless`** with no arguments opens an interactive terminal interface —
  browse entities, check holdings and warnings, watch a sync run live.
- **`grubless <command>`** is scriptable — pipeable output, real exit codes,
  built for CI and bulk work. A TUI can't be piped, so it doesn't replace this.

---

## Interactive mode

```bash
grubless
```

Pick an entity, then move between **Holdings**, **Warnings**, **Tax** and
**Sources**. Press `s` to sync every enabled source and watch the worker's own
progress messages update in place — the one thing the terminal genuinely does
better than a script.

| | |
|---|---|
| `↑ ↓` / `k j` | move |
| `PgUp` `PgDn`, `Home` `End` | page / jump |
| `⏎` | open the selected entity |
| `↹` / `← →` / `h l`, or `1`–`4` | switch tab |
| `r` | reload |
| `s` | sync every enabled source |
| `?` | help |
| `q` / `Esc` | back to entities, or quit from there |
| `Ctrl-C` | quit immediately |

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
grubless warnings --entity "Acme Trading" --fail-on-blocking
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

The token is stored in your system keychain where one is available, and in
`~/.config/grubless/config.json` (mode `0600`) otherwise.

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
grubless tax-summary        --entity <id|name> [--year 2025]
grubless warnings           --entity <id|name> [--fail-on-blocking]

grubless report <name>      --entity <id|name> --year 2025 [--out <path>]
grubless report <name>      --all-entities --year 2025 --out <dir>
```

`--entity` takes a uuid, an exact name, or an unambiguous name prefix. An
ambiguous prefix is an error rather than a guess — quietly picking the wrong
client's entity would produce a confident, correct-looking, completely wrong
report.

Add `--all-entities` to most commands to run across everything you can reach.

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
| `NO_COLOR` | Disable colour |
