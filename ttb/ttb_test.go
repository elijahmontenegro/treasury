package ttb

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"treasury/internal/render"
	"treasury/internal/synth"
)

// The reference every label aligns to. One wrong character fails every line,
// so the constant is pinned to the text verified against the CFR.
func TestStatuteHash(t *testing.T) {
	sum := sha256.Sum256([]byte(Statute))
	const want = "35e1f5d39ee341ac7c114f8159956cb0cc1981b94e4ffeee194ff5060bf99fbc"
	if got := hex.EncodeToString(sum[:]); got != want {
		t.Fatalf("Statute hash %s, want %s", got, want)
	}
	if Statute[:HeaderLen] != "GOVERNMENT WARNING:" {
		t.Fatalf("header span = %q", Statute[:HeaderLen])
	}
}

func TestText(t *testing.T) {
	cases := map[string]string{
		ABVText(45):    "45% Alc./Vol.",
		ABVText(45.5):  "45.5% Alc./Vol.",
		ProofText(45):  "90 Proof",
		ProofText(40.5): "81 Proof",
		NetText(750):   "750 mL",
		NetText(1000):  "1 L",
		NetText(1750):  "1.75 L",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %q want %q", got, want)
		}
	}
}

func TestLabelDocumentRenders(t *testing.T) {
	faces, err := render.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	_, truth, err := synth.Render(LabelDocument(Sample()), faces)
	if err != nil {
		t.Fatal(err)
	}
	claims := map[string]int{}
	for _, g := range truth.Glyphs {
		claims[g.Claim]++
	}
	for _, c := range []string{"brand", "class", "abv", "net", "producer", "origin", "reference"} {
		if claims[c] == 0 {
			t.Errorf("no glyphs for claim %q", c)
		}
	}
	if claims["reference"] != synth.CountNonSpace(Statute) {
		t.Errorf("reference glyphs = %d, want %d", claims["reference"], synth.CountNonSpace(Statute))
	}
}
