// Package encoder turns a patch (binary, optionally grayscale) into a bit code.
package encoder

import (
	"fmt"
	"image"
	"math"

	"treasury/internal/bitcode"
	"treasury/internal/bitmap"
)

// Patch is what gets encoded: the binary crop and, when a grayscale channel
// is wanted, the matching grayscale crop of the same size.
type Patch struct {
	Bin  *bitmap.Bitmap
	Gray *image.Gray
}

// Encoder maps patches to codes of a fixed length.
type Encoder interface {
	Encode(p Patch) bitcode.Code
	Bits() int
	Name() string
}

// Hash is the deterministic baseline: the binary patch area-averaged onto a
// W×H grid and thresholded at half coverage, optionally followed by a
// difference hash of the grayscale patch on a (W+1)×H grid.
type Hash struct {
	W, H  int
	DHash bool
}

// Glyph is the configuration for single glyphs inside a line frame.
func Glyph() Hash { return Hash{W: 16, H: 16} }

// Line is the configuration for line-level regions and spelled codewords.
func Line() Hash { return Hash{W: 64, H: 16, DHash: true} }

// Bits is the code length.
func (h Hash) Bits() int {
	n := h.W * h.H
	if h.DHash {
		n *= 2
	}
	return n
}

// Name identifies the configuration in evidence.
func (h Hash) Name() string {
	if h.DHash {
		return fmt.Sprintf("hash-%dx%d+dhash", h.W, h.H)
	}
	return fmt.Sprintf("hash-%dx%d", h.W, h.H)
}

// Encode implements Encoder.
func (h Hash) Encode(p Patch) bitcode.Code {
	code := bitcode.New(h.Bits())
	if p.Bin != nil {
		cov := Coverage(p.Bin, h.W, h.H)
		for i, v := range cov {
			if v >= 0.5 {
				code.Set(i)
			}
		}
	}
	if h.DHash && p.Gray != nil {
		g := GrayGrid(p.Gray, h.W+1, h.H)
		base := h.W * h.H
		// Cells of a flat region average to the same value up to rounding;
		// a strict comparison would set random bits there.
		const eps = 1e-6
		for y := range h.H {
			for x := range h.W {
				if g[y*(h.W+1)+x]-g[y*(h.W+1)+x+1] > eps {
					code.Set(base + y*h.W + x)
				}
			}
		}
	}
	return code
}

// Coverage area-averages the bitmap onto a w×h grid: each cell is the
// fraction of its (fractional) source rectangle that is ink.
func Coverage(b *bitmap.Bitmap, w, h int) []float64 {
	return grid(b.W, b.H, func(x, y int) float64 { return float64(b.Pix[y*b.W+x]) }, w, h)
}

// GrayGrid area-averages the grayscale image onto a w×h grid.
func GrayGrid(g *image.Gray, w, h int) []float64 {
	gw, gh := g.Rect.Dx(), g.Rect.Dy()
	return grid(gw, gh, func(x, y int) float64 {
		return float64(g.Pix[g.PixOffset(g.Rect.Min.X+x, g.Rect.Min.Y+y)])
	}, w, h)
}

// grid resamples a sw×sh field onto w×h cells by exact box integration using
// a summed-area table with fractional edges.
func grid(sw, sh int, at func(x, y int) float64, w, h int) []float64 {
	out := make([]float64, w*h)
	if sw == 0 || sh == 0 {
		return out
	}
	stride := sw + 1
	s := make([]float64, (sw+1)*(sh+1))
	for y := 1; y <= sh; y++ {
		var row float64
		for x := 1; x <= sw; x++ {
			row += at(x-1, y-1)
			s[y*stride+x] = s[(y-1)*stride+x] + row
		}
	}
	// integral at real coordinates: whole cells plus fractional edge strips
	// and the fractional corner pixel.
	S := func(fx, fy float64) float64 {
		ix, iy := int(fx), int(fy)
		if ix >= sw {
			ix, fx = sw, float64(sw)
		}
		if iy >= sh {
			iy, fy = sh, float64(sh)
		}
		ax, ay := fx-float64(ix), fy-float64(iy)
		v := s[iy*stride+ix]
		if ax > 0 {
			v += ax * (s[iy*stride+ix+1] - s[iy*stride+ix])
		}
		if ay > 0 {
			v += ay * (s[(iy+1)*stride+ix] - s[iy*stride+ix])
		}
		if ax > 0 && ay > 0 {
			v += ax * ay * at(ix, iy)
		}
		return v
	}
	cw, ch := float64(sw)/float64(w), float64(sh)/float64(h)
	for j := range h {
		y0, y1 := float64(j)*ch, float64(j+1)*ch
		for i := range w {
			x0, x1 := float64(i)*cw, float64(i+1)*cw
			sum := S(x1, y1) - S(x0, y1) - S(x1, y0) + S(x0, y0)
			out[j*w+i] = sum / (cw * ch)
		}
	}
	return out
}

// NormalizedDistance is Hamming distance as a fraction of the code length.
func NormalizedDistance(a, b bitcode.Code, bits int) float64 {
	if bits == 0 {
		return 0
	}
	return math.Min(1, float64(bitcode.Distance(a, b))/float64(bits))
}
