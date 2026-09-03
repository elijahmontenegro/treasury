package bitmap

import (
	"image"
	"math"
	"testing"
)

func bar(w, h, stroke int) *Bitmap {
	b := New(w, h)
	x0 := (w - stroke) / 2
	for y := 4; y < h-4; y++ {
		for x := x0; x < x0+stroke; x++ {
			b.Set(x, y, 1)
		}
	}
	return b
}

func TestStrokeWidth(t *testing.T) {
	for _, s := range []int{3, 5, 8} {
		got := bar(40, 60, s).StrokeWidth()
		if math.Abs(got-float64(s)) > 1.5 {
			t.Errorf("stroke %d measured %.1f", s, got)
		}
	}
}

func TestErodeDilate(t *testing.T) {
	b := bar(40, 60, 6)
	thin, _ := b.Erode(1).InkBounds()
	if thin.Dx() != 4 {
		t.Errorf("erode 1: width %d, want 4", thin.Dx())
	}
	thick, _ := b.Dilate(1).InkBounds()
	if thick.Dx() != 8 {
		t.Errorf("dilate 1: width %d, want 8", thick.Dx())
	}
	if same, _ := b.WithStroke(6).InkBounds(); same != (image.Rectangle{Min: image.Pt(17, 4), Max: image.Pt(23, 56)}) {
		t.Errorf("WithStroke at the current width changed the bar: %v", same)
	}
	if norm := b.WithStroke(3).StrokeWidth(); math.Abs(norm-3) > 1.5 {
		t.Errorf("WithStroke(3) measured %.1f", norm)
	}
	if norm := b.WithStroke(9).StrokeWidth(); math.Abs(norm-9) > 1.5 {
		t.Errorf("WithStroke(9) measured %.1f", norm)
	}
}
