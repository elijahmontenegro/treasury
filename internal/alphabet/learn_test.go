package alphabet

import "testing"

// TestCoverageInAcceptance pins step 12c: an alignment that explains its own
// glyphs but reads only a quarter of the reference is not an alphabet,
// however few violations it shows.
func TestCoverageInAcceptance(t *testing.T) {
	block := &Block{Glyphs: make([]Glyph, 240)}
	a := &Alphabet{Matched: 62, Chars: 241, Block: block}
	if got := a.Coverage(); got < 0.25 || got > 0.26 {
		t.Fatalf("coverage %.3f, want about 0.257", got)
	}
	if a.OK(0.10, 0.15, 0.30) {
		t.Error("a block explaining a quarter of the reference was accepted")
	}
	if !a.OK(0.10, 0.15, 0.20) {
		t.Error("the same block was refused at a bound it clears")
	}
	full := &Alphabet{Matched: 219, Chars: 241, Block: block}
	if !full.OK(0.10, 0.15, 0.30) {
		t.Error("a real alignment was refused")
	}
}
