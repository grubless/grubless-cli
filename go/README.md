# Go port

A complete port of the CLI to Go: every command, the interactive TUI, config
and keychain handling, and the exit codes. Its contract is **byte-identical
behaviour** with the Node build: the same stdout, stderr, exit codes and files
for the same server responses. Three layers of checks, described below, hold
it to that.

```bash
cd go
go test ./...
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/grubless ./cmd/grubless
```

The build is a static binary of about 6–7 MB, cross-compiled from any machine:
`GOOS=darwin|linux|windows GOARCH=amd64|arm64`.

## Go-only: transactions

The Go build is now the one going forward, and its first feature the Node
build doesn't have is a transactions view, over
`GET /entities/:id/tx-events`:

```bash
grubless transactions --entity acme                          # newest 50
grubless transactions --entity acme --category transfer,send --direction out
grubless transactions --entity acme --asset SOL --from 2025-07-01 --to 2026-06-30
grubless transactions --entity acme --limit all --json > txs.json
```

- Filters are the API's own: `--category` (comma-separated event types; a bad
  one gets the API's list of valid ones back), `--direction`, `--from`/`--to`,
  `--source` (id or label), `--asset` (id, or a symbol the entity holds; a
  symbol held on several chains is refused rather than guessed), `--search`
  and `--sort`.
- `--limit` (default 50, or `all`) pages past the API's 2000-per-request cap by
  following its cursor.
- `--json` is one document per entity, with the events and assets as the
  server sent them, the `filters` that produced them, and the `nextCursor`
  when more remain.
- In the TUI it's tab 6. It loads 100 rows with the entity, then pages in the
  next 100 as the cursor gets within a screenful of the end. **Enter** opens a
  transaction: every leg with its exact value, proceeds, cost basis and
  gain/loss. **↑/↓** step through transactions, and **Esc** closes it.

**Filtering in the TUI:** **f** (or **/**) on the Transactions tab opens a
filter form with every filter the API supports:

- **Category** is typed, comma-separated, with suggestions from the ~80
  categories as you type. **→** takes the one shown in grey, the way a shell
  autosuggestion does.
- **Direction**, **Source**, **Asset** and **Sort** are choices, changed
  with **← →**, or by typing a letter to jump. Assets come from holdings and
  from the transactions loaded so far, labelled by chain, so the several
  USDCs can be told apart.
- **From**, **To** and **Search** are typed.
- **↑ ↓** or **Tab** move between fields, **Ctrl-U** clears one, **Enter**
  applies, **Esc** cancels, and "Clear all filters" resets everything.

The form checks categories and dates before any request. An unknown
category that's a prefix of a real one gets "did you mean transfer?" rather
than being completed silently. The applied filter is carried into paging and
reloads, and the status bar counts active filters. It belongs to the entity:
leaving the entity drops it.

The category list is copied from the API's own validation error (2026-09-30).
If the server adds a category, the form refuses it until the list is
updated, but `grubless transactions --category` passes anything through.

It's read-only. The write routes (recategorise, bulk recategorise, notes and
tags) are next, once their request bodies are known.

The parity checks keep holding everything else to the Node build.
`parity.mjs` and `tty-parity.py` remove exactly what this feature adds (its
help lines, the sixth tab, the filter's help line) before comparing. The
stub API applies the tx-events filters the way the real API does, so the
Go-only session in `tty-parity.py` can check them end to end. The TUI's session step that pressed Tab from
Sources now presses `1` instead, since Tab reaches Transactions in Go by
design. `tty-parity.py` also drives the new tab in a Go-only session with
asserted screens. The TUI render and reduce golden files now hold Go's
output (see `internal/tui/golden_test.go` for how that switch was checked).

## Go-only: the TUI's card layout

The TUI is laid out like the web app: cards, after
`apps/web/components/ui/card.tsx` in the main repo. Each card is a bordered
box with its title in the top border (a terminal can't spare a row per
title), and room at the other end of the border for a total, a count, or
which filters are on. The tab bar is the web's segmented control, and the
first tab is **Overview**, as the web's is:

- A **Portfolio value** card: headline value, unrealised gain and income,
  the range control, and the chart.
- An **Activity breakdown** card beside it, on terminals 110 columns or
  wider. It shows transaction value by category over the chart's range,
  largest first, the top five in the web's category colours and the rest as
  "Other", as the web's donut groups them. It uses bars rather than a donut,
  since a terminal draws bars better. The category names are the web's,
  copied as strings from the main repo's `transaction-categories.ts`, and
  only the names: nothing of the engine reaches this public client.
- Four **stat cards**: Connected sources, Transactions, Last synced (the
  web's wording, "3 hours ago") and Price coverage ("412 / 415, 3 missing").
  The breakdown and coverage come from `/activity-breakdown` and
  `/price-coverage`. They load after the entity opens and fill in when they
  arrive, since the breakdown can take seconds on a large ledger.

The list tabs are cards titled as the web titles them ("Current holdings",
with its total, and so on), and the picker, help, transaction detail and
filter form are cards too. The Overview drops the old holdings tile, as the
web's does, and on a small terminal the breakdown gives way first, then the
stat cards, so the chart keeps its room.

This ended the screen-by-screen comparison of the TUI with the Node build:
the layouts no longer correspond. `tty-parity.py` now checks the TUI in a
Go-only session that asserts what each screen must show. The render golden
files hold Go's output, and a test holds every fixture screen, in both
themes, to exactly its height and width. Command parity and the key-handling
comparison are unaffected.

## Go-only: TUI themes

The TUI has two themes, after the web app's:

- **Cypherpunk**, the default: the web's `.cypher` theme, phosphor green on
  near-black, with its colours taken exactly from `apps/web/app/globals.css` in
  the main repo, where they were measured for contrast and colour-blind
  separation. Selections and the active tab use the web's selection purple.
  The chart draws value in the primary purple and income in aqua, as the web
  portfolio chart does. The title reads `GRUBLESS█`, like the web's page
  titles. Things a terminal can't draw (scanlines, pixel shadows, the pixel
  font) are left out.
- **Terminal**: the TUI as it first looked, in your terminal's own colours and
  background.

With no choice made, the TUI opens in Cypherpunk, unless `NO_COLOR` is set or
the terminal has fewer than 256 colours (the Linux console, a plain `xterm`
entry). Cypherpunk's codes would show the wrong colours there, so those cases
get Terminal instead. Press `t` to switch; the choice is saved as `"theme"`
in the config file. `GRUBLESS_THEME=cypher|terminal` overrides both the saved
choice and the default. Cypherpunk uses exact 24-bit colour where the terminal
advertises it (`COLORTERM=truecolor`, or Windows Terminal), and the nearest of
the 256 standard colours otherwise, for example in macOS Terminal.app.

The Terminal theme's output is unchanged byte for byte: the TUI golden files
render in it and still pass. The Node-vs-Go TUI comparison in `tty-parity.py`
runs the Go build with `GRUBLESS_THEME=terminal`, so it still compares like
with like, and the parity scripts drop the two help lines the theme adds.

## Dependencies

`golang.org/x/term`, plus the `golang.org/x/sys` it needs. Both are maintained
by the Go team. They provide raw mode and the window size for the TUI, which
Node had built in. TLS, HTTP and everything else come from the standard
library. The toolchain is Go 1.26, which current `x/term` requires.

## Checking parity with the Node build

```bash
pnpm build                                        # from the repo root
(cd go && go build -o bin/grubless ./cmd/grubless)

node go/scripts/parity.mjs                        # 1. every command vs a stub API
python3 go/scripts/tty-parity.py                  # 2. colour output and a TUI session, in a pty
GRUBLESS_CLI_BIN=$PWD/go/bin/grubless pnpm test   #    the TS subprocess tests, against Go
pnpm exec tsx go/scripts/goldens.ts               # 3. regenerate the golden files (see below)
```

1. **`parity.mjs`** runs both builds through 116 scenarios against a stub API.
   The scenarios cover every command and flag, every error status, partial
   report failures, `--wait` against an activity feed that advances,
   malformed server filenames, file errors, and a login → use → logout
   sequence. It diffs stdout, stderr, exit codes, request bodies, the config
   file and every file `--out` writes. The only difference it normalises away
   is the transport error text for an unreachable host.
2. **`tty-parity.py`** does the same on a real pseudo-terminal, where colour
   is on. It also drives both TUIs through the same keystrokes, compares
   the reconstructed screen after each one, and checks that each build
   restores the terminal on exit.
3. **Golden files** (`internal/*/testdata/*.json.gz`) are produced by running
   the TS implementation itself over the TS tests' inputs plus thousands of
   seeded random ones: decimals, CSVs, chart series, TUI screens and key
   sequences through the reducer. The Go tests must reproduce every output
   exactly, and need no Node to run. Regenerate them after changing either
   build. A failure afterwards is a real behavioural difference. The
   exception is the TUI's render and reduce files. Since the Transactions tab
   they hold Go's expected output, regenerated with
   `go test ./internal/tui -run 'Render|Reduce' -update`.

The TS unit tests are also ported as ordinary Go tests alongside, as the
readable specification.

## Where Go's defaults differ from Node's, and what's done about it

Each of these would have made output differ silently. Every one is handled in
code and pinned by a test.

- **`--json` isn't `encoding/json`.** Outputs are the server's data,
  reshaped, then printed by `JSON.stringify`. `internal/jsonv` keeps key
  order, puts integer-like keys first as JS does, formats numbers as JS does,
  and escapes only what `JSON.stringify` escapes (`encoding/json` also escapes
  `<>&` and U+2028).
- **Widths are UTF-16.** JS `.length`, `.slice` and `padStart` count UTF-16
  units. `internal/jsstr` does the same, so columns line up identically,
  including where an emoji gets cut in half.
- **`Math.round`, `toFixed` and `String(n)`** round and format differently from
  `math.Round` and `strconv`. `(2.5).toFixed(0)` is `"3"`, not `"2"`.
- **`flag` can't parse this CLI**, because it stops at the first positional.
  `internal/args` is shaped like `parseArgs`.
- **`fetch` strips a BOM** from report CSVs, and decodes invalid UTF-8 per
  WHATWG. `readFileSync` keeps the BOM.
- **Dates:** V8 lets "2025-02-30" overflow to 2 March, and reads a
  date-time with no offset as local time.
- **File errors** use Node's (libuv's) wording:
  `EISDIR: illegal operation on a directory, open 'x'`.
- **A Go panic exits 2**, which is this CLI's usage-error code. It's
  recovered and exits 1.

Known, accepted divergences: V8's legacy date formats ("July 1, 2025"), which
the API never sends, and lone UTF-16 surrogates in JSON strings, which a Go
string can't hold.

## Bugs in the TS that the port found, and keeps for now

The port reproduces these faithfully, so both builds agree until each is fixed
in both:

1. `auth whoami --api-url X` queries X but reports the configured URL.
2. **End** on a TUI tab sets the picker's index to that tab's last row. Back
   on the picker, **Enter** can then open nothing. Go would have panicked
   here; the golden tests caught it.
3. The TUI's sync watcher treats "queued" as finished, and watches every
   activity row rather than the ones it started. A stale "running" job keeps
   it polling forever.
4. The TUI's Holdings tab doesn't cap long asset symbols, so an unresolved
   token address runs into the next column.
5. ~~`report --out --all-entities` joins the server's filename unchecked, so a
   filename containing `../` would write outside `--out`.~~ **Fixed in both
   builds.** An entity named `..` did the same through the folder name, and
   is fixed too. The server's filename is reduced to its last component
   (with a warning if it named a path), a name of only dots is refused, and
   the final path is checked to be inside `--out` before anything is
   written. `parity.mjs` runs each attack (`../`, URL-encoded, backslashes, a
   bare `..`, an entity named `..`) against both builds, and fails if either
   writes anywhere outside `--out`.
6. A non-numeric value in the portfolio series makes the TS chart loop forever
   in Bresenham. Go draws nothing for that point instead. This is the one
   intentional difference; nothing else could be compared anyway, since the
   TS never finishes.
7. `--out` folder names drop every non-ASCII letter ("Société" becomes
   `Soci-t`).
