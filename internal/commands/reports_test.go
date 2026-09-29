package commands

import (
	"path/filepath"
	"testing"
)

// `report --out --all-entities` builds each path from two things this
// machine doesn't control: the entity's name and the server's
// Content-Disposition filename. Neither may put a file outside --out. The
// cases match src/test/reports.test.ts.

func TestSafeFilename(t *testing.T) {
	keep := map[string]string{
		"capital-gains-FY2025-26.csv": "capital-gains-FY2025-26.csv",
		"fees€-FY.csv":                "fees€-FY.csv",
		"..report.csv":                "..report.csv", // two dots, then a name
		"../../.bashrc":               ".bashrc",
		"/etc/passwd":                 "passwd",
		`..\..\evil.csv`:              "evil.csv",
		"a/b/c.csv":                   "c.csv",
	}
	for in, want := range keep {
		if got, ok := safeFilename(in); !ok || got != want {
			t.Errorf("safeFilename(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"", " ", ".", "..", "...", "../", "a/..", "x\x00y.csv", "line\nbreak.csv", "del\x7f.csv"} {
		if got, ok := safeFilename(in); ok {
			t.Errorf("safeFilename(%q) = %q, should be refused", in, got)
		}
	}
}

func TestSafeDirName(t *testing.T) {
	for in, want := range map[string]string{
		"Acme Trading Pty Ltd":      "Acme-Trading-Pty-Ltd",
		"Smith & Co (Trust) / 2025": "Smith-Co-Trust-2025",
		"..Holdings":                "..Holdings",
		"../..":                     "..-..", // one ordinary name: the separator is replaced
		"..":                        "entity",
		".":                         "entity",
		"...":                       "entity",
		" .. ":                      "entity",
		"/..":                       "entity",
		"!!!":                       "entity",
	} {
		if got := safeDirName(in); got != want {
			t.Errorf("safeDirName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsInside(t *testing.T) {
	inside := [][2]string{{"out", "out/Acme/x.csv"}, {"out", "out/..Holdings/x.csv"}, {"./out/", "out/a/../b/x.csv"}}
	outside := [][2]string{{"out", "out"}, {"out", "out/.."}, {"out", "out/../x.csv"}, {"out", "out/a/../../x.csv"}, {"out", "/etc/passwd"}, {"out", "outside/x.csv"}}
	for _, c := range inside {
		if !isInside(c[0], filepath.FromSlash(c[1])) {
			t.Errorf("isInside(%q, %q) = false", c[0], c[1])
		}
	}
	for _, c := range outside {
		if isInside(c[0], filepath.FromSlash(c[1])) {
			t.Errorf("isInside(%q, %q) = true", c[0], c[1])
		}
	}
}
