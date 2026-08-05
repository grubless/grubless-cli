/**
 * Terminal primitives for the TUI — raw mode, the alternate screen, key
 * decoding, and painting.
 *
 * Hand-rolled against ANSI rather than pulling in Ink or blessed. Ink would
 * be quicker to write and brings React plus ~50 transitive packages with it;
 * this binary stores an API token that can reach an accounting firm's entire
 * client list, so its dependency surface is the last place to spend
 * convenience. The published package declares zero runtime dependencies and
 * this file is what keeps that true.
 *
 * The hard requirement here is **restoring the terminal no matter how we
 * exit**. A TUI that leaves raw mode on, the cursor hidden, or the alternate
 * screen active hands the user a shell that looks broken — and they will
 * rightly blame the tool. Every exit path goes through `close()`.
 */

const ESC = "\u001b";

export const ansi = {
  altScreenOn: `${ESC}[?1049h`,
  altScreenOff: `${ESC}[?1049l`,
  hideCursor: `${ESC}[?25l`,
  showCursor: `${ESC}[?25h`,
  clear: `${ESC}[2J`,
  home: `${ESC}[H`,
  reset: `${ESC}[0m`,
  bold: `${ESC}[1m`,
  dim: `${ESC}[2m`,
  reverse: `${ESC}[7m`,
  red: `${ESC}[31m`,
  green: `${ESC}[32m`,
  yellow: `${ESC}[33m`,
  cyan: `${ESC}[36m`,
  moveTo: (row: number, col: number) => `${ESC}[${row};${col}H`,
  clearLine: `${ESC}[2K`,
};

export interface Key {
  name: string;
  ctrl: boolean;
}

/**
 * Decodes a chunk of raw stdin into key events.
 *
 * Arrow keys and friends arrive as multi-byte escape sequences, and a paste
 * or a fast keypress can deliver several in one chunk — so this returns an
 * array rather than a single key. Anything unrecognised is dropped instead of
 * being surfaced as garbage: an unknown sequence should do nothing, not
 * trigger whatever action its first byte happens to map to.
 */
export function decodeKeys(chunk: string): Key[] {
  const keys: Key[] = [];
  let i = 0;

  while (i < chunk.length) {
    const rest = chunk.slice(i);

    if (rest.startsWith(`${ESC}[`) || rest.startsWith(`${ESC}O`)) {
      const code = rest[2];
      const named: Record<string, string> = {
        A: "up",
        B: "down",
        C: "right",
        D: "left",
        H: "home",
        F: "end",
      };
      if (code && named[code]) {
        keys.push({ name: named[code], ctrl: false });
        i += 3;
        continue;
      }
      // Sequences like ESC[5~ (page up). Consume to the terminator so the
      // remaining bytes aren't misread as individual keypresses.
      const terminator = /^\u001b\[[0-9;]*([~a-zA-Z])/.exec(rest);
      if (terminator) {
        const numeric = /^\u001b\[(\d+)~/.exec(rest);
        if (numeric) {
          const pageKeys: Record<string, string> = { "5": "pageup", "6": "pagedown" };
          const name = pageKeys[numeric[1]];
          if (name) keys.push({ name, ctrl: false });
        }
        i += terminator[0].length;
        continue;
      }
      i += 1;
      continue;
    }

    const ch = chunk[i];
    if (ch === ESC) {
      keys.push({ name: "escape", ctrl: false });
    } else if (ch === "\r" || ch === "\n") {
      keys.push({ name: "return", ctrl: false });
    } else if (ch === "\t") {
      keys.push({ name: "tab", ctrl: false });
    } else if (ch === "\u007f" || ch === "\b") {
      keys.push({ name: "backspace", ctrl: false });
    } else if (ch === "\u0003") {
      keys.push({ name: "c", ctrl: true });
    } else if (ch === "\u0004") {
      keys.push({ name: "d", ctrl: true });
    } else if (ch >= " ") {
      keys.push({ name: ch, ctrl: false });
    }
    i += 1;
  }

  return keys;
}

/**
 * Visible width of a string, ignoring ANSI escapes.
 *
 * Used for padding and truncation. Escape sequences occupy zero columns, so
 * counting raw `.length` would push every styled line off by the length of
 * its colour codes — which shows up as ragged borders rather than as an
 * obvious bug.
 */
export function visibleWidth(text: string): number {
  return stripAnsi(text).length;
}

export function stripAnsi(text: string): string {
  // eslint-disable-next-line no-control-regex
  return text.replace(/\u001b\[[0-9;?]*[a-zA-Z]/g, "");
}

/**
 * Truncates to `width` visible columns, preserving escape sequences.
 *
 * Naively slicing a styled string can cut an escape sequence in half, which
 * spills raw bytes onto the screen and leaves the terminal's colour state
 * stuck. Walking the string and only counting printable characters avoids
 * both.
 */
export function truncate(text: string, width: number): string {
  if (width <= 0) return "";
  if (visibleWidth(text) <= width) return text;

  let out = "";
  let visible = 0;
  let i = 0;
  while (i < text.length && visible < width - 1) {
    const escape = /^\u001b\[[0-9;?]*[a-zA-Z]/.exec(text.slice(i));
    if (escape) {
      out += escape[0];
      i += escape[0].length;
      continue;
    }
    out += text[i];
    visible += 1;
    i += 1;
  }
  return out + "…" + ansi.reset;
}

export function pad(text: string, width: number): string {
  const gap = width - visibleWidth(text);
  return gap > 0 ? text + " ".repeat(gap) : text;
}

export interface TerminalOptions {
  stdin?: NodeJS.ReadStream;
  stdout?: NodeJS.WriteStream;
}

export class Terminal {
  private readonly stdin: NodeJS.ReadStream;
  private readonly stdout: NodeJS.WriteStream;
  private closed = false;
  private previousFrame: string[] = [];
  private onKeyHandler: ((key: Key) => void) | null = null;
  private onResizeHandler: (() => void) | null = null;

  constructor(opts: TerminalOptions = {}) {
    this.stdin = opts.stdin ?? process.stdin;
    this.stdout = opts.stdout ?? process.stdout;
  }

  get width(): number {
    // A sane floor: a zero-width terminal (or a pipe reporting no size) would
    // otherwise make every truncate() call return an empty string.
    return Math.max(this.stdout.columns ?? 80, 20);
  }

  get height(): number {
    return Math.max(this.stdout.rows ?? 24, 6);
  }

  open(): void {
    this.stdout.write(ansi.altScreenOn + ansi.hideCursor + ansi.clear);
    if (this.stdin.isTTY) this.stdin.setRawMode(true);
    this.stdin.resume();
    this.stdin.setEncoding("utf8");

    this.stdin.on("data", this.handleData);
    this.stdout.on("resize", this.handleResize);

    // Belt and braces. If anything throws past our own handlers — or the
    // process is killed — the terminal still gets restored. Without these a
    // crash leaves the user in the alternate screen with no cursor.
    process.on("exit", this.restore);
    process.on("SIGINT", this.handleSignal);
    process.on("SIGTERM", this.handleSignal);
    process.on("uncaughtException", this.handleFatal);
  }

  private handleData = (chunk: string): void => {
    if (!this.onKeyHandler) return;
    for (const key of decodeKeys(chunk)) this.onKeyHandler(key);
  };

  private handleResize = (): void => {
    // Force a full repaint: the diff below compares against the previous
    // frame, and after a resize those line positions no longer mean anything.
    this.previousFrame = [];
    this.onResizeHandler?.();
  };

  private handleSignal = (): void => {
    this.close();
    process.exit(130);
  };

  private handleFatal = (err: unknown): void => {
    this.close();
    process.stderr.write(`${err instanceof Error ? (err.stack ?? err.message) : String(err)}\n`);
    process.exit(1);
  };

  private restore = (): void => {
    if (this.closed) return;
    this.closed = true;
    if (this.stdin.isTTY) this.stdin.setRawMode(false);
    this.stdout.write(ansi.showCursor + ansi.altScreenOff + ansi.reset);
  };

  onKey(handler: (key: Key) => void): void {
    this.onKeyHandler = handler;
  }

  onResize(handler: () => void): void {
    this.onResizeHandler = handler;
  }

  /**
   * Paints a frame, writing only the lines that actually changed.
   *
   * A full redraw every tick makes the screen flicker and, over a slow SSH
   * link, visibly lag. Diffing against the previous frame keeps a progress
   * update to a single line of output.
   */
  render(lines: string[]): void {
    if (this.closed) return;
    const height = this.height;
    const frame = lines.slice(0, height);
    while (frame.length < height) frame.push("");

    let output = "";
    for (let row = 0; row < frame.length; row++) {
      if (this.previousFrame[row] === frame[row]) continue;
      output += ansi.moveTo(row + 1, 1) + ansi.clearLine + frame[row] + ansi.reset;
    }
    if (output) this.stdout.write(output);
    this.previousFrame = frame;
  }

  close(): void {
    this.restore();
    this.stdin.removeListener("data", this.handleData);
    this.stdout.removeListener("resize", this.handleResize);
    process.removeListener("exit", this.restore);
    process.removeListener("SIGINT", this.handleSignal);
    process.removeListener("SIGTERM", this.handleSignal);
    process.removeListener("uncaughtException", this.handleFatal);
    if (this.stdin.isTTY) this.stdin.pause();
  }
}
