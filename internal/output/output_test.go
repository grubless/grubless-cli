package output

import "testing"

func TestEllipsizeCountsRunesNotBytes(t *testing.T) {
	// 12 runes, 13 bytes. A byte-based cut would split "é" and emit invalid
	// UTF-8 onto the terminal.
	name := "Société Acme"
	if got := Ellipsize(name, 20); got != name {
		t.Errorf("Ellipsize under the cap changed the text: %q", got)
	}
	if got := Ellipsize(name, 6); got != "Socié…" {
		t.Errorf("Ellipsize(_, 6) = %q", got)
	}
	if width(name) != 12 {
		t.Errorf("width = %d, want 12", width(name))
	}
}
