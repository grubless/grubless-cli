// Package guard holds the two promises this client's safety rests on, as
// tests rather than conventions:
//
//  1. It is a thin HTTP client. The tax engine (@grubless/core, in the main
//     repo) is not open source, and none of it may reach this public one — a
//     vendored snippet in a hurry is all it would take.
//  2. Its dependencies are the Go standard library plus the Go team's
//     golang.org/x/term and golang.org/x/sys. The binary holds a token that
//     can read a firm's entire client list; every module it links is another
//     party that could reach that token.
//
// These replaced scripts/verify-bundle.mjs, which checked the same two
// things in the Node build's bundle.
package guard

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// forbidden are distinctive exports of the engine that no thin client would
// contain by coincidence.
var forbidden = []string{
	"processDisposal",
	"consumeLots",
	"orderLotsForConsumption",
	"toDisposals",
	"auAtoRuleset",
	"usIrsRuleset",
	"classifyCategory",
	"@grubless/core",
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestNoEngineCode(t *testing.T) {
	root := repoRoot(t)
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "testdata") {
			return filepath.SkipDir
		}
		// The binary is built from non-test Go files; this file names the
		// symbols it forbids, so it's the one exception.
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, symbol := range forbidden {
			if strings.Contains(string(src), symbol) {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("%s contains %q: tax engine code must not reach this client. Wire shapes belong in internal/api.", rel, symbol)
			}
		}
		return nil
	})
}

func TestOnlyGoTeamDependencies(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"golang.org/x/term": true, "golang.org/x/sys": true}
	// go.mod read by hand: parsing it with golang.org/x/mod would be the
	// very kind of dependency this test is here to question.
	inBlock := false
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "require (":
			inBlock = true
			continue
		case inBlock && line == ")":
			inBlock = false
			continue
		case strings.HasPrefix(line, "require "):
			line = strings.TrimPrefix(line, "require ")
		case !inBlock:
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "//") {
			continue
		}
		if !allowed[fields[0]] {
			t.Errorf("go.mod requires %s. This binary holds a token that reaches a firm's whole client list; a new dependency needs arguing for, and adding here.", fields[0])
		}
	}
}
