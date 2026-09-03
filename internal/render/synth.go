package render

import (
	"fmt"
	"golang.org/x/image/font"
	"image"
	"image/draw"
	"math"

	"golang.org/x/image/math/fixed"

	"treasury/internal/bitmap"
)

// Synth is a synthesized glyph on its own baseline: Box is relative to the
// pen origin, so Box.Min.Y is negative above the baseline.
type Synth struct {
	Bin  *bitmap.Bitmap
	Gray *image.Gray
	Box  image.Rectangle
}

// ratios returns the face's x-height and cap height as fractions of its size.
// Metrics reports the face's x-height and cap height as fractions of the
// pixel size.
func (f *Face) Metrics() (xh, cap float64, err error) { return f.ratios() }

func (f *Face) ratios() (xh, cap float64, err error) {
	f.mu.Lock()
	if f.ratioDone {
		xh, cap = f.xhRatio, f.capRatio
		f.mu.Unlock()
		return xh, cap, nil
	}
	f.mu.Unlock()
	face, err := f.At(100)
	if err != nil {
		return 0, 0, err
	}
	f.draw.Lock()
	m := face.Metrics()
	f.draw.Unlock()
	xh, cap = float64(m.XHeight)/64/100, float64(m.CapHeight)/64/100
	// Many faces leave these out of their OS/2 table; measure the glyphs.
	if xh <= 0 {
		xh = f.inkHeight(face, 'x') / 100
	}
	if cap <= 0 {
		cap = f.inkHeight(face, 'H') / 100
	}
	f.mu.Lock()
	f.xhRatio, f.capRatio, f.ratioDone = xh, cap, true
	f.mu.Unlock()
	return xh, cap, nil
}

// inkHeight is the height of r's ink at the face's size, 0 when r is
// missing or blank.
func (f *Face) inkHeight(face font.Face, r rune) float64 {
	f.draw.Lock()
	defer f.draw.Unlock()
	bounds, _, ok := face.GlyphBounds(r)
	if !ok {
		return 0
	}
	return float64(bounds.Max.Y-bounds.Min.Y) / 64
}

// Glyph renders r at the size where the face's x-height equals target px, or
// its cap height when byCap is set.
func (f *Face) Glyph(r rune, target float64, byCap bool) (Synth, error) {
	xh, cap, err := f.ratios()
	if err != nil {
		return Synth{}, err
	}
	ratio := xh
	if byCap {
		ratio = cap
	}
	if ratio <= 0 {
		ratio = 0.5
	}
	size := math.Round(target/ratio*4) / 4
	face, err := f.At(size)
	if err != nil {
		return Synth{}, err
	}
	pad := int(math.Ceil(size)) + 2
	canvas := image.NewGray(image.Rect(0, 0, 3*pad, 3*pad))
	for i := range canvas.Pix {
		canvas.Pix[i] = 255
	}
	dot := fixed.P(pad, 2*pad)
	f.draw.Lock()
	dr, mask, maskp, _, ok := face.Glyph(dot, r)
	if ok {
		draw.DrawMask(canvas, dr, image.Black, image.Point{}, mask, maskp, draw.Over)
	}
	f.draw.Unlock()
	if !ok {
		return Synth{}, fmt.Errorf("render: %s has no glyph for %q", f.Name, r)
	}
	bin := bitmap.FromGray(canvas, 128)
	ib, has := bin.InkBounds()
	if !has {
		return Synth{}, fmt.Errorf("render: %q renders blank in %s", r, f.Name)
	}
	gray := image.NewGray(image.Rect(0, 0, ib.Dx(), ib.Dy()))
	draw.Draw(gray, gray.Rect, canvas, ib.Min, draw.Src)
	return Synth{Bin: bin.Crop(ib), Gray: gray, Box: ib.Sub(image.Pt(pad, 2*pad))}, nil
}
