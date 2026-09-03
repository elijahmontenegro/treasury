package alphabet_test

import (
	"testing"

	"treasury/internal/alphabet"
	"treasury/internal/encoder"
	"treasury/internal/preprocess"
	"treasury/internal/region"
	"treasury/ttb"
)

// TestDumpSamples prints, for a few characters, every sample's box and its
// distance to the character's centroid. It only reports.
func TestDumpSamples(t *testing.T) {
	img, _ := sample(t)
	pre, err := preprocess.Run(img, preprocess.Default())
	if err != nil {
		t.Fatal(err)
	}
	lines, _ := region.Propose(pre.Bin, pre.Glare, region.Default())
	a, err := alphabet.Find(lines, pre.Bin, pre.Gray, ttb.Statute, headerSpan, alphabet.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range "il" {
		cen := a.Centroid[alphabet.Key{R: r, Span: -1}]
		for _, g := range a.Samples[r] {
			t.Logf("%q box=%v (%dx%d) derived=%v distance=%.3f", r, g.Box, g.Box.Dx(), g.Box.Dy(), g.Derived, encoder.NormalizedDistance(g.Code, cen, a.Bits))
		}
	}
}
