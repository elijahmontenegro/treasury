package region

import (
	"image"
	"testing"

	"treasury/internal/bitmap"
)

// Two 10×14 blobs joined by a one-pixel bridge must split at the bridge; a
// single blob of ordinary width must not.
func TestSplitFused(t *testing.T) {
	b := bitmap.New(60, 30)
	fill(b, image.Rect(5, 8, 15, 22))
	fill(b, image.Rect(17, 8, 27, 22))
	fill(b, image.Rect(15, 14, 17, 15)) // bridge
	fill(b, image.Rect(40, 8, 50, 22))  // a lone glyph
	cs := Components(b)
	if len(cs) != 2 {
		t.Fatalf("components = %d, want 2 (one fused pair, one glyph)", len(cs))
	}
	pieces := SplitFused(b, cs, 10, 14, 1.6)
	if len(pieces) != 3 {
		t.Fatalf("pieces = %d, want 3: %v", len(pieces), pieces)
	}
	sortByX := func(p []Component) {
		for i := 1; i < len(p); i++ {
			for j := i; j > 0 && p[j].Box.Min.X < p[j-1].Box.Min.X; j-- {
				p[j], p[j-1] = p[j-1], p[j]
			}
		}
	}
	sortByX(pieces)
	if pieces[0].Box.Max.X > 17 || pieces[1].Box.Min.X < 15 || pieces[2].Box != image.Rect(40, 8, 50, 22) {
		t.Errorf("pieces %v", pieces)
	}
	// A wide single glyph with solid ink across its width (no neck) stays whole.
	solid := bitmap.New(60, 30)
	fill(solid, image.Rect(5, 8, 25, 22))
	if got := SplitFused(solid, Components(solid), 10, 14, 1.6); len(got) != 1 {
		t.Errorf("solid block split into %d pieces", len(got))
	}
}
