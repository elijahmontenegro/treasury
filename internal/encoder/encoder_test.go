package encoder

import (
	"image"
	"math"
	"testing"

	"treasury/internal/bitcode"
	"treasury/internal/bitmap"
)

func TestCoverageExact(t *testing.T) {
	// 3×3 bitmap with the left column and the centre pixel set; resample to 2×2.
	b := bitmap.New(3, 3)
	for y := range 3 {
		b.Set(0, y, 1)
	}
	b.Set(1, 1, 1)
	cov := Coverage(b, 2, 2)
	// Each cell covers 1.5×1.5 source px. Top-left: column 0 over 1.5 rows
	// = 1.5, plus a quarter of the centre pixel = 0.25.
	want := []float64{1.75 / 2.25, 0.25 / 2.25, 1.75 / 2.25, 0.25 / 2.25}
	for i := range want {
		if math.Abs(cov[i]-want[i]) > 1e-9 {
			t.Errorf("cell %d = %.4f, want %.4f", i, cov[i], want[i])
		}
	}
}

func TestCoverageIdentity(t *testing.T) {
	b := bitmap.New(5, 4)
	b.Set(2, 1, 1)
	b.Set(4, 3, 1)
	cov := Coverage(b, 5, 4)
	for i, v := range cov {
		if math.Abs(v-float64(b.Pix[i])) > 1e-9 {
			t.Fatalf("cell %d = %.3f want %d", i, v, b.Pix[i])
		}
	}
}

func bar(x0, x1 int) (*bitmap.Bitmap, *image.Gray) {
	b := bitmap.New(40, 10)
	g := image.NewGray(image.Rect(0, 0, 40, 10))
	for i := range g.Pix {
		g.Pix[i] = 255
	}
	for x := x0; x < x1; x++ {
		for y := 2; y < 8; y++ {
			b.Set(x, y, 1)
			g.Pix[y*40+x] = 0
		}
	}
	return b, g
}

func TestHashDeterministicAndDistinct(t *testing.T) {
	h := Line()
	if h.Bits() != 2048 || Glyph().Bits() != 1792 {
		t.Fatalf("bits: line %d glyph %d", h.Bits(), Glyph().Bits())
	}
	lb, lg := bar(0, 20)
	c1 := h.Encode(Patch{Bin: lb, Gray: lg})
	c2 := h.Encode(Patch{Bin: lb, Gray: lg})
	if d := bitcode.Distance(c1, c2); d != 0 {
		t.Errorf("same patch encodes differently: %d", d)
	}
	rb, rg := bar(20, 40)
	c3 := h.Encode(Patch{Bin: rb, Gray: rg})
	if d := bitcode.Distance(c1, c3); d < 500 {
		t.Errorf("mirrored patch too close: %d", d)
	}
	// The grid channel of the left bar: the bar spans y 2..8 of 10, so grid
	// row 8 (y 5.0..5.6) is fully inside it and has its left half set; grid
	// row 0 (y 0..0.6) is empty.
	for x := range 64 {
		if want := x < 32; c1.Get(8*64+x) != want {
			t.Fatalf("grid bit (%d,8) = %v", x, !want)
		}
		if c1.Get(x) {
			t.Fatalf("grid bit (%d,0) set above the bar", x)
		}
	}
	// The dHash channel: a bit is set where the gray falls left to right. The
	// left bar only rises (dark then bright), so no bits; the right bar
	// falls once per row inside the bar, over one or two grid columns.
	if n := popRange(c1, 1024, 2048); n != 0 {
		t.Errorf("left bar dHash bits = %d, want 0", n)
	}
	if n := popRange(c3, 1024, 2048); n < 8 || n > 32 {
		t.Errorf("right bar dHash bits = %d, want one edge per bar row", n)
	}
}

func TestThermometerLevels(t *testing.T) {
	h := Hash{W: 2, H: 1, Levels: 2}
	b := bitmap.New(4, 4)
	// Left cell fully inked, right cell one third inked (coverage 0.33).
	for y := range 4 {
		b.Set(0, y, 1)
		b.Set(1, y, 1)
		if y < 3 {
			b.Set(2, y, 1)
		}
	}
	// coverage: left = 1.0 → levels 0.25 and 0.75 set; right = 3/8 → only 0.25 set
	c := h.Encode(Patch{Bin: b})
	if !c.Get(0) || !c.Get(1) || !c.Get(2) || c.Get(3) {
		t.Errorf("thermometer bits = %v %v %v %v", c.Get(0), c.Get(1), c.Get(2), c.Get(3))
	}
	if h.Name() != "hash-2x1x2" || h.Bits() != 4 {
		t.Errorf("name %q bits %d", h.Name(), h.Bits())
	}
}

func popRange(c bitcode.Code, from, to int) int {
	n := 0
	for i := from; i < to; i++ {
		if c.Get(i) {
			n++
		}
	}
	return n
}
