package synth

import (
	"image"
	"math"
	"math/rand"

	"treasury/internal/imgops"
)

// Aug is a channel augmentation. Zero values are skipped. Steps apply in
// the order warp, rotate, brightness and contrast, glare, blur, JPEG, and
// the truth boxes follow the geometry.
type Aug struct {
	WarpFrac    float64 // each corner moves by up to this fraction of the image size
	RotateDeg   float64
	Brightness  float64 // added to every pixel, in gray levels
	Contrast    float64 // multiplied about mid-gray; 0 means 1
	Glare       int     // number of bright radial blobs
	BlurSigma   float64
	JPEGQuality int
	Seed        int64 // for warp corners and glare placement
}

// Random draws an augmentation from the ranges in the spec: rotation
// ±10°, a small perspective warp, blur σ ≤ 1.5, JPEG 40–95, brightness and
// contrast jitter, and up to two glare blobs.
func Random(rng *rand.Rand) Aug {
	return Aug{
		WarpFrac:    rng.Float64() * 0.02,
		RotateDeg:   (rng.Float64()*2 - 1) * 10,
		Brightness:  (rng.Float64()*2 - 1) * 30,
		Contrast:    0.75 + rng.Float64()*0.5,
		Glare:       rng.Intn(3),
		BlurSigma:   rng.Float64() * 1.5,
		JPEGQuality: 40 + rng.Intn(56),
		Seed:        rng.Int63(),
	}
}

// Augment applies a to the image and transforms the truth.
func Augment(g *image.Gray, t *Truth, a Aug) (*image.Gray, *Truth, error) {
	out := g
	nt := &Truth{W: t.W, H: t.H, AngleDeg: t.AngleDeg, Glyphs: append([]Glyph(nil), t.Glyphs...)}
	rng := rand.New(rand.NewSource(a.Seed))
	w, h := float64(g.Rect.Dx()), float64(g.Rect.Dy())
	if a.WarpFrac > 0 {
		src := [4][2]float64{{0, 0}, {w, 0}, {w, h}, {0, h}}
		var dst [4][2]float64
		for i := range src {
			dst[i] = [2]float64{
				src[i][0] + (rng.Float64()*2-1)*a.WarpFrac*w,
				src[i][1] + (rng.Float64()*2-1)*a.WarpFrac*h,
			}
		}
		H := imgops.HomographyFrom(src, dst)
		out = imgops.Warp(out, H, 255)
		for i := range nt.Glyphs {
			nt.Glyphs[i].Box = imgops.WarpRect(nt.Glyphs[i].Box, H)
		}
	}
	if a.RotateDeg != 0 {
		out = imgops.Rotate(out, a.RotateDeg, 255)
		cx, cy := w/2, h/2
		for i := range nt.Glyphs {
			nt.Glyphs[i].Box = imgops.RotateRect(nt.Glyphs[i].Box, cx, cy, a.RotateDeg)
		}
		nt.AngleDeg += a.RotateDeg
	}
	if a.Brightness != 0 || (a.Contrast != 0 && a.Contrast != 1) {
		contrast := a.Contrast
		if contrast == 0 {
			contrast = 1
		}
		out = imgops.Levels(out, contrast, a.Brightness)
	}
	for i := 0; i < a.Glare; i++ {
		cx, cy := rng.Float64()*w, rng.Float64()*h
		r := (0.1 + rng.Float64()*0.2) * math.Max(w, h)
		out = imgops.Glare(out, cx, cy, r, 0.6+rng.Float64()*0.4)
	}
	if a.BlurSigma > 0 {
		out = imgops.GaussianBlur(out, a.BlurSigma)
	}
	if a.JPEGQuality > 0 {
		var err error
		if out, err = imgops.JPEGRoundTrip(out, a.JPEGQuality); err != nil {
			return nil, nil, err
		}
	}
	return out, nt, nil
}
