import { describe, it, expect } from "vitest";
import { isInside, safeDirName, safeFilename } from "../commands/reports.js";

/**
 * `report --out --all-entities` builds each path from two things this machine
 * doesn't control: the entity's name (whoever created the entity chose it)
 * and the server's Content-Disposition filename. Neither may put a file
 * outside --out.
 */

describe("safeFilename", () => {
  it("keeps an ordinary name", () => {
    expect(safeFilename("capital-gains-FY2025-26.csv")).toBe("capital-gains-FY2025-26.csv");
    expect(safeFilename("fees€-FY.csv")).toBe("fees€-FY.csv");
    // Two leading dots are a name, not a traversal.
    expect(safeFilename("..report.csv")).toBe("..report.csv");
  });

  it("drops any directory part, in either separator", () => {
    expect(safeFilename("../../.bashrc")).toBe(".bashrc");
    expect(safeFilename("/etc/passwd")).toBe("passwd");
    expect(safeFilename("..\\..\\evil.csv")).toBe("evil.csv");
    expect(safeFilename("a/b/c.csv")).toBe("c.csv");
  });

  it("refuses a name with nothing usable left", () => {
    for (const name of ["", " ", ".", "..", "...", "../", "a/..", "x\u0000y.csv", "line\nbreak.csv", "del\u007f.csv"]) {
      expect(safeFilename(name), JSON.stringify(name)).toBeNull();
    }
  });
});

describe("safeDirName", () => {
  it("keeps readable names readable", () => {
    expect(safeDirName("Acme Trading Pty Ltd")).toBe("Acme-Trading-Pty-Ltd");
    expect(safeDirName("Smith & Co (Trust) / 2025")).toBe("Smith-Co-Trust-2025");
    expect(safeDirName("..Holdings")).toBe("..Holdings");
  });

  it("never yields the current or parent directory", () => {
    for (const name of ["..", ".", "...", " .. ", "/..", "!!!"]) {
      expect(safeDirName(name), JSON.stringify(name)).toBe("entity");
    }
    // A separator is replaced, so what's left is one ordinary name.
    expect(safeDirName("../..")).toBe("..-..");
  });
});

describe("isInside", () => {
  it("accepts paths under the directory", () => {
    expect(isInside("out", "out/Acme/x.csv")).toBe(true);
    expect(isInside("out", "out/..Holdings/x.csv")).toBe(true);
    expect(isInside("./out/", "out/a/../b/x.csv")).toBe(true);
  });

  it("rejects the directory itself and anything outside it", () => {
    expect(isInside("out", "out")).toBe(false);
    expect(isInside("out", "out/..")).toBe(false);
    expect(isInside("out", "out/../x.csv")).toBe(false);
    expect(isInside("out", "out/a/../../x.csv")).toBe(false);
    expect(isInside("out", "/etc/passwd")).toBe(false);
    expect(isInside("out", "outside/x.csv")).toBe(false);
  });
});
