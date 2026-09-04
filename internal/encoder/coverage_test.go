package encoder

import (
	"math/rand"
	"testing"

	"treasury/internal/bitmap"
)

// TestCoverageMatchesGrid checks the inlined coverage against the generic
// grid bit for bit, on random bitmaps of odd sizes.
func TestCoverageMatchesGrid(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	for _, size := range [][2]int{{7, 9}, {23, 23}, {31, 44}, {16, 16}} {
		b := bitmap.New(size[0], size[1])
		for i := range b.Pix {
			if rng.Intn(3) == 0 {
				b.Pix[i] = 1
			}
		}
		for _, g := range [][2]int{{16, 16}, {12, 16}, {64, 16}} {
			a := Coverage(b, g[0], g[1])
			ref := grid(b.W, b.H, func(x, y int) float64 { return float64(b.Pix[y*b.W+x]) }, g[0], g[1])
			for i := range a {
				if a[i] != ref[i] {
					t.Fatalf("size %v grid %v cell %d: %v vs %v", size, g, i, a[i], ref[i])
				}
			}
		}
	}
}
