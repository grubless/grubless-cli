// Package golden loads golden files: expected outputs, gzipped JSON.
//
// Most were recorded by running the TS build this CLI was first written in
// over thousands of seeded inputs, which is how the Go port was held to it
// byte for byte. That build is gone, so those files are now a fixed record of
// its behaviour: extend them by hand if a case is missing, and treat a
// failure against one as a real change in behaviour. The TUI's render and
// reduce files are the exception, regenerated from this build with -update.
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
		t.Fatalf("%v — golden files live in each package's testdata/", err)
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

// Save writes v back to a gzipped golden file.
func Save(t *testing.T, path string, v any) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw, _ := gzip.NewWriterLevel(f, gzip.BestCompression)
	if err := json.NewEncoder(zw).Encode(v); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}
