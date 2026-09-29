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
   build. A failure afterwards is a real behavioural difference.

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
5. `report --out --all-entities` joins the server's filename unchecked, so a
   filename containing `../` would write outside `--out`.
6. A non-numeric value in the portfolio series makes the TS chart loop forever
   in Bresenham. Go draws nothing for that point instead. This is the one
   intentional difference; nothing else could be compared anyway, since the
   TS never finishes.
7. `--out` folder names drop every non-ASCII letter ("Société" becomes
   `Soci-t`).
