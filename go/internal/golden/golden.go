// Package golden loads the expected outputs that go/scripts/goldens.ts
// records by running the TS implementation. See that script for why.
package golden

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"testing"
)

// Load decodes a gzipped golden file into v, failing the test if it can't.
func Load(t *testing.T, path string, v any) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("%v — regenerate with: pnpm exec tsx go/scripts/goldens.ts", err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewDecoder(zr).Decode(v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

// Num is a JSON number that may be null — how JSON.stringify writes NaN.
type Num struct {
	V     float64
	IsNaN bool
}

func (n *Num) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		n.IsNaN = true
		return nil
	}
	return json.Unmarshal(b, &n.V)
}
