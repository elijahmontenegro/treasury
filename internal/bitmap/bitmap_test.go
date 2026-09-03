package bitmap

import (
	"image"
	"image/color"
	"testing"
)

func white(w, h int) *image.Gray {
	g := image.NewGray(image.Rect(0, 0, w, h))
	for i := range g.Pix {
		g.Pix[i] = 255
	}
	return g
}

func TestFromGrayToGray(t *testing.T) {
	g := white(4, 3)
	g.SetGray(1, 1, color.Gray{Y: 0})
	g.SetGray(3, 2, color.Gray{Y: 100})
	b := FromGray(g, 128)
	if b.Count() != 2 || b.At(1, 1) != 1 || b.At(3, 2) != 1 {
		t.Fatalf("ink = %d at (1,1)=%d (3,2)=%d", b.Count(), b.At(1, 1), b.At(3, 2))
	}
	if b.At(-1, 0) != 0 || b.At(4, 0) != 0 || b.At(0, 3) != 0 {
		t.Error("out-of-range reads must be 0")
	}
	back := b.ToGray()
	if back.GrayAt(1, 1).Y != 0 || back.GrayAt(0, 0).Y != 255 {
		t.Errorf("ToGray: ink=%d paper=%d", back.GrayAt(1, 1).Y, back.GrayAt(0, 0).Y)
	}
}

func TestFromGrayWithOffsetRect(t *testing.T) {
	g := white(6, 6)
	g.SetGray(3, 3, color.Gray{})
	sub := g.SubImage(image.Rect(2, 2, 6, 6)).(*image.Gray)
	b := FromGray(sub, 128)
	if b.W != 4 || b.H != 4 || b.Count() != 1 || b.At(1, 1) != 1 {
		t.Fatalf("sub-image not handled: %dx%d count=%d at(1,1)=%d", b.W, b.H, b.Count(), b.At(1, 1))
	}
}

func TestCropAndInkBounds(t *testing.T) {
	b := New(10, 10)
	b.Set(2, 3, 1)
	b.Set(7, 8, 1)
	b.Set(-1, 0, 1) // ignored
	r, ok := b.InkBounds()
	if !ok || r != image.Rect(2, 3, 8, 9) {
		t.Fatalf("InkBounds = %v %v", r, ok)
	}
	c := b.Crop(image.Rect(-5, 0, 5, 5))
	if c.W != 5 || c.H != 5 || c.Count() != 1 || c.At(2, 3) != 1 {
		t.Fatalf("Crop clamped wrong: %dx%d count=%d", c.W, c.H, c.Count())
	}
	if n := b.CountIn(image.Rect(0, 0, 5, 5)); n != 1 {
		t.Errorf("CountIn = %d, want 1", n)
	}
	if _, ok := New(3, 3).InkBounds(); ok {
		t.Error("empty bitmap must report no ink")
	}
}
