# Go port — spike

A spike to test whether this CLI should be a native binary. It ports `auth`
(login, logout, whoami) and `entities list` end to end: argument parsing,
config and keychain, the HTTP client and its error mapping, the version
handshake, and exit codes. Every other command reports that it isn't ported
and exits 1.

```bash
cd go
go test ./...
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/grubless ./cmd/grubless
```

## Checking it against the Node build

```bash
pnpm build                                  # from the repo root
node go/scripts/parity.mjs                  # both builds vs a stub API, diffed
GRUBLESS_CLI_BIN=$PWD/go/bin/grubless pnpm test   # the existing subprocess tests
```

`parity.mjs` runs both builds across 43 scenarios, including every failure
status, flag edge cases and a login → read → logout sequence. It compares
stdout, stderr, exit code and the config file left on disk. The only
difference it normalises away is the transport error text for an unreachable
host: Node says `fetch failed`, Go names the syscall, and both name the host.

## Dependencies

None: `go list -m all` lists only this module. TLS, HTTP/2 and IDNA come from
Go's own standard library, which ships Go-team copies of `golang.org/x/*`
inside itself. The TUI would be the first thing to need a module:
`golang.org/x/term` for raw mode, which is maintained by the Go team.

## Things the port had to get right that Go gets wrong by default

- **`flag` can't parse this CLI.** It stops at the first positional, so
  `entities list --json` never sees `--json`. `internal/args` is a strict
  parser shaped like Node's `parseArgs`.
- **A panic exits 2**, which is this CLI's usage-error code. `main` recovers
  and exits 1 instead.
- **`--json` must not decode into a struct.** Doing that drops fields this
  client doesn't declare and reorders the rest. The server's bytes are
  re-indented instead.
- **`json.Marshal` escapes `<>&`** as `<` and so on. It's turned off for
  output this client builds itself.
- **String length is in bytes.** Table widths and truncation count runes
  instead.
- **`ParseFloat` accepts `NaN` and `Inf`**, which `Number.isFinite` refused.
  Both are rejected explicitly.
- **TTY detection** uses `ModeCharDevice`, which is fine on Unix but wrong on
  Windows, where `NUL` is also a character device. The full port would use
  `x/term`.
