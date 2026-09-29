#!/usr/bin/env python3
"""
Parity on a real terminal: what parity.mjs can't see through a pipe.

Two checks, each run against the Node bundle and the Go binary in turn, in a
pseudo-terminal of a fixed size, against parity.mjs's stub API:

1. Commands with colour on. On a TTY the tables carry escape codes, and the
   TS counts those codes in its column widths — so this is where a port that
   measured "correctly" would drift. Raw bytes are compared.

2. A scripted TUI session. The same keystrokes go to both, and after each
   one the screen is reconstructed (a minimal emulator: the TUI only ever
   moves the cursor, clears a line and writes text) once output settles.
   Screens are compared, then the exit code and the terminal restore.

    pnpm build && (cd go && go build -o bin/grubless ./cmd/grubless)
    python3 go/scripts/tty-parity.py
"""

import fcntl
import os
import pty
import re
import select
import signal
import struct
import subprocess
import sys
import tempfile
import termios
import time

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
BUILDS = {
    "node": [subprocess.check_output(["which", "node"]).decode().strip(), os.path.join(ROOT, "dist", "index.js")],
    "go": [os.path.join(ROOT, "go", "bin", "grubless")],
}
COLS, ROWS = 100, 30


def spawn(argv, env):
    pid, fd = pty.fork()
    if pid == 0:
        os.execve(argv[0], argv, env)
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))
    return pid, fd


def read_until_idle(fd, idle, limit=30.0):
    """Everything the child writes until it's been quiet for `idle` seconds."""
    out = b""
    deadline = time.time() + limit
    last = time.time()
    while time.time() < deadline:
        r, _, _ = select.select([fd], [], [], 0.05)
        if r:
            try:
                chunk = os.read(fd, 65536)
            except OSError:
                break
            if not chunk:
                break
            out += chunk
            last = time.time()
        elif time.time() - last >= idle:
            break
    return out


def wait_exit(pid, timeout=10.0):
    deadline = time.time() + timeout
    while time.time() < deadline:
        done, status = os.waitpid(pid, os.WNOHANG)
        if done:
            return os.waitstatus_to_exitcode(status)
        time.sleep(0.05)
    os.kill(pid, signal.SIGKILL)
    return "killed"


class Screen:
    """Just enough VT100 for this TUI: cursor moves, line clears, text."""

    def __init__(self):
        self.clear()

    def clear(self):
        self.grid = [[" "] * COLS for _ in range(ROWS)]
        self.row = self.col = 0

    def feed(self, data):
        text = data.decode("utf-8", "replace")
        i = 0
        while i < len(text):
            m = re.match(r"\x1b\[(\??)([0-9;]*)([A-Za-z])", text[i:])
            if m:
                private, params, cmd = m.groups()
                if cmd == "H":
                    parts = [int(p) if p else 1 for p in (params.split(";") + ["1", "1"])[:2]]
                    self.row, self.col = parts[0] - 1, parts[1] - 1
                elif cmd == "K" and params == "2":
                    if 0 <= self.row < ROWS:
                        self.grid[self.row] = [" "] * COLS
                elif cmd == "J" and params == "2":
                    self.clear()
                elif cmd == "h" and private and params == "1049":
                    self.clear()
                i += len(m.group(0))
                continue
            ch = text[i]
            if ch == "\r":
                self.col = 0
            elif ch == "\n":
                self.row += 1
            elif ch >= " " and 0 <= self.row < ROWS and 0 <= self.col < COLS:
                self.grid[self.row][self.col] = ch
                self.col += 1
            i += 1

    def text(self):
        return "\n".join("".join(r).rstrip() for r in self.grid).rstrip()


def main():
    stub = subprocess.Popen(["node", os.path.join(ROOT, "go", "scripts", "parity.mjs"), "--serve"], stdout=subprocess.PIPE, text=True)
    api = stub.stdout.readline().strip()
    failures = 0
    home = tempfile.mkdtemp(prefix="tty-parity-")
    base_env = {"PATH": "", "HOME": home, "XDG_CONFIG_HOME": home, "TZ": "UTC", "TERM": "xterm-256color", "GRUBLESS_TOKEN": "grb_ok", "GRUBLESS_API_URL": api}

    # 1. Commands, colour on.
    commands = [
        ["holdings", "--entity", "acme"],
        ["sources", "list", "--all-entities"],
        ["warnings", "--entity", "acme"],
        ["tax-summary", "--all-entities"],
        ["portfolio", "--entity", "acme", "--range", "1y"],
        ["entities", "list"],
        ["auth", "whoami"],
    ]
    for args in commands:
        outputs = {}
        for name, argv in BUILDS.items():
            stub.send_signal(signal.SIGUSR2)
            pid, fd = spawn(argv + args, base_env)
            out = read_until_idle(fd, idle=1.0)
            outputs[name] = (out, wait_exit(pid))
            os.close(fd)
        label = " ".join(args)
        if outputs["node"] == outputs["go"]:
            print(f"✓ tty: {label}  (exit {outputs['go'][1]}, {len(outputs['go'][0])} bytes)")
        else:
            failures += 1
            print(f"✗ tty: {label}\n    node: {outputs['node']!r:.1500}\n    go:   {outputs['go']!r:.1500}")

    # 2. A TUI session.
    steps = [
        ("load", None, 1.5),
        ("move down", b"j", 0.5),
        ("back up", b"k", 0.5),
        ("open Acme", b"\r", 1.5),
        ("widen range", b"]", 0.5),
        ("narrow range", b"[[[", 0.5),
        ("holdings", b"2", 0.5),
        ("cursor down", b"j", 0.5),
        ("page down", b"\x1b[6~", 0.5),
        ("warnings", b"3", 0.5),
        ("tax", b"4", 0.5),
        ("sources", b"5", 0.5),
        ("tab wraps", b"\t", 0.5),
        ("help", b"?", 0.5),
        ("dismiss help", b"x", 0.5),
        ("reload", b"r", 1.5),
        ("sync", b"s", 3.0),
        ("sync settles", b"", 6.0),
        ("back to picker", b"q", 0.5),
        ("open the empty entity", b"j\r", 1.5),
        ("its warnings", b"3", 0.5),
        ("back", b"\x1b", 0.8),
        ("quit", b"q", 0.5),
    ]
    screens = {}
    exits = {}
    tails = {}
    for name, argv in BUILDS.items():
        stub.send_signal(signal.SIGUSR2)
        pid, fd = spawn(argv, base_env)
        screen = Screen()
        screens[name] = []
        raw = b""
        for label, keys, settle in steps:
            if keys:
                os.write(fd, keys)
            out = read_until_idle(fd, idle=settle)
            raw += out
            screen.feed(out)
            screens[name].append((label, screen.text()))
        exits[name] = wait_exit(pid)
        raw += read_until_idle(fd, idle=0.3, limit=2)
        tails[name] = raw[-40:]
        os.close(fd)

    for (label, a), (_, b) in zip(screens["node"], screens["go"]):
        if a == b:
            print(f"✓ tui: {label}")
        else:
            failures += 1
            print(f"✗ tui: {label}")
            for i, (x, y) in enumerate(zip(a.split("\n") + [""] * ROWS, b.split("\n") + [""] * ROWS)):
                if x != y:
                    print(f"    row {i}\n      node: {x!r}\n      go:   {y!r}")
    for name in BUILDS:
        restored = b"\x1b[?25h" in tails[name] and b"\x1b[?1049l" in tails[name]
        ok = exits[name] == 0 and restored
        failures += 0 if ok else 1
        print(f"{'✓' if ok else '✗'} tui ({name}): exit {exits[name]}, terminal restored: {restored}")

    stub.terminate()
    print(f"\n{'tty parity: identical' if failures == 0 else f'tty parity: {failures} difference(s)'}")
    if os.environ.get("TTY_PARITY_SHOW"):
        for label, text in screens["go"]:
            print(f"\n=== {label} ===\n{text}")
    sys.exit(1 if failures else 0)


if __name__ == "__main__":
    main()
