package preprocess_test

import (
	"image"
	"math"
	"testing"

	"treasury/internal/bitmap"
	"treasury/internal/imgops"
	"treasury/internal/preprocess"
	"treasury/internal/render"
	"treasury/internal/synth"
)

// paragraph renders five lines of text on an 800×400 canvas.
func paragraph(t *testing.T) *image.Gray {
	t.Helper()
	faces, err := render.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	doc := synth.Document{W: 800, H: 400}
	for i := range 5 {
		doc.Items = append(doc.Items, synth.Item{
			Text: "The quick brown fox jumps over the lazy dog 0123456789",
			Face: "Go Regular", Px: 26, X: 40, Y: 80 + i*50,
		})
	}
	img, _, err := synth.Render(doc, faces)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func TestSauvolaFindsStrokesOnGradient(t *testing.T) {
	w, h := 200, 100
	g := image.NewGray(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			g.Pix[y*w+x] = uint8(170 + x/4) // 170..219 gradient
		}
	}
	var bars []image.Rectangle
	for x := 50; x < 150; x += 20 {
		bars = append(bars, image.Rect(x, 30, x+6, 70))
	}
	for _, r := range bars {
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				g.Pix[y*w+x] = 20
			}
		}
	}
	b := preprocess.Sauvola(g, 31, 0.2, 128)
	inside := 0
	for _, r := range bars {
		inside += b.CountIn(r)
	}
	if inside < 5*6*40*95/100 {
		t.Errorf("ink inside bars = %d of %d", inside, 5*6*40)
	}
	if outside := b.Count() - inside; outside > w*h/100 {
		t.Errorf("ink outside bars = %d", outside)
	}
}

func TestEstimateSkew(t *testing.T) {
	img := paragraph(t)
	for _, deg := range []float64{0, 7, -4.5, 12} {
		rot := imgops.Rotate(img, deg, 255)
		bin := preprocess.Sauvola(rot, 31, 0.2, 128)
		got := preprocess.EstimateSkew(bin, 15)
		if math.Abs(got-deg) > 0.3 {
			t.Errorf("rotated %.1f°, estimated %.1f°", deg, got)
		}
	}
}

func TestRunDeskewsAndScales(t *testing.T) {
	img := imgops.Rotate(paragraph(t), 7, 255)
	res, err := preprocess.Run(img, preprocess.Default())
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(res.AngleDeg-7) > 0.3 {
		t.Errorf("AngleDeg = %.1f, want 7", res.AngleDeg)
	}
	if res.Scale != 2 || res.Gray.Rect.Dx() != 1600 || res.Bin.W != 1600 || res.Bin.H != 800 {
		t.Errorf("scale %.2f size %v bin %dx%d", res.Scale, res.Gray.Rect, res.Bin.W, res.Bin.H)
	}
	if res.Glare != nil {
		t.Error("clean render must not get a glare mask")
	}
	// After deskew the rows must be horizontal: re-estimating finds ~0.
	if again := preprocess.EstimateSkew(res.Bin, 15); math.Abs(again) > 0.3 {
		t.Errorf("residual skew %.1f°", again)
	}
}

func TestGlareMask(t *testing.T) {
	flat := image.NewGray(image.Rect(0, 0, 50, 50))
	for i := range flat.Pix {
		flat.Pix[i] = 255
	}
	flat.Pix[0] = 0
	if preprocess.GlareMask(flat, 0.99, 16) != nil {
		t.Error("flat white image must not produce a mask")
	}
	grad := image.NewGray(image.Rect(0, 0, 100, 10))
	for y := range 10 {
		for x := range 100 {
			grad.Pix[y*100+x] = uint8(100 + x*155/99)
		}
	}
	m := preprocess.GlareMask(grad, 0.99, 16)
	if m == nil {
		t.Fatal("gradient must produce a mask")
	}
	if n := m.Count(); n < 10 || n > 30 {
		t.Errorf("mask covers %d px, want about the brightest 1–2%%", n)
	}
	if m.At(0, 0) != 0 || m.At(99, 0) != 1 {
		t.Error("mask must mark the bright end only")
	}
	_ = bitmap.New // keep the import honest if the helper set changes
}
