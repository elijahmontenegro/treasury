package imgops

import (
	"image"
	"math"
	"testing"

	"treasury/internal/bitmap"
)

func canvas(w, h int, rect image.Rectangle) *image.Gray {
	g := image.NewGray(image.Rect(0, 0, w, h))
	for i := range g.Pix {
		g.Pix[i] = 255
	}
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			g.Pix[y*w+x] = 0
		}
	}
	return g
}

func within(a, b image.Rectangle, tol int) bool {
	return abs(a.Min.X-b.Min.X) <= tol && abs(a.Min.Y-b.Min.Y) <= tol &&
		abs(a.Max.X-b.Max.X) <= tol && abs(a.Max.Y-b.Max.Y) <= tol
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func TestRotateMatchesRotateRect(t *testing.T) {
	rect := image.Rect(60, 40, 140, 80)
	g := canvas(200, 120, rect)
	r := Rotate(g, 7, 255)
	got, ok := bitmap.FromGray(r, 128).InkBounds()
	if !ok {
		t.Fatal("rotated image has no ink")
	}
	want := RotateRect(rect, 100, 60, 7)
	if !within(got, want, 2) {
		t.Errorf("rotated ink bounds %v, RotateRect says %v", got, want)
	}
	back, ok := bitmap.FromGray(Rotate(r, -7, 255), 128).InkBounds()
	if !ok || !within(back, rect, 2) {
		t.Errorf("round trip bounds %v, want %v", back, rect)
	}
}

func TestRotateDirection(t *testing.T) {
	// A point on the right of centre moves down (larger y) under a positive
	// angle: image coordinates, clockwise on screen.
	_, y := RotatePoint(100, 50, 50, 50, 10)
	if y <= 50 {
		t.Errorf("positive rotation moved a right-hand point up: y=%.1f", y)
	}
}

func TestGaussianBlurPreservesMean(t *testing.T) {
	g := canvas(64, 64, image.Rect(20, 20, 44, 44))
	b := GaussianBlur(g, 1.5)
	if b.Rect != g.Rect {
		t.Fatalf("size changed: %v", b.Rect)
	}
	if math.Abs(mean(g)-mean(b)) > 1 {
		t.Errorf("mean drifted from %.2f to %.2f", mean(g), mean(b))
	}
	if b.GrayAt(32, 32).Y != 0 || b.GrayAt(20, 32).Y == 0 {
		t.Errorf("blur shape wrong: centre=%d edge=%d", b.GrayAt(32, 32).Y, b.GrayAt(20, 32).Y)
	}
}

func TestJPEGRoundTrip(t *testing.T) {
	g := canvas(64, 48, image.Rect(10, 10, 30, 30))
	j, err := JPEGRoundTrip(g, 50)
	if err != nil {
		t.Fatal(err)
	}
	if j.Rect != g.Rect {
		t.Fatalf("size changed: %v", j.Rect)
	}
	if d := math.Abs(mean(g) - mean(j)); d > 4 {
		t.Errorf("mean moved by %.2f", d)
	}
}

func mean(g *image.Gray) float64 {
	sum := 0.0
	for _, v := range g.Pix {
		sum += float64(v)
	}
	return sum / float64(len(g.Pix))
}
