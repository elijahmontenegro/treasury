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
// W×H grid and quantized to Levels bits per cell, optionally followed by a
// difference hash of the grayscale patch on a (W+1)×H grid.
//
// With one level a cell is set at half coverage. With more, the cell holds a
// thermometer code (level k set when coverage exceeds (2k+1)/(2·Levels)), so
// an edge cell that straddles a threshold differs by one bit between two
// renderings of the same shape while a cell that is ink in one and paper in
// the other differs by all of them.
type Hash struct {
	W, H   int
	Levels int // bits per cell; 0 means 1
	Smooth int // radius, in cells, of a box blur applied to coverage before quantizing; 0 means none
	DHash  bool
}

// Glyph is the encoder for single glyphs inside a line frame: a positional
// view, 16 cells across the 2.2 x-height frame, plus a tight view of the
// ink's own box at 12×16, both with coverage smoothed over neighbouring
// cells and quantized to four levels. Measured on Go Regular across sizes
// (see spell's frame tests), the same character drifts by under a third
// of the distance between two different characters, the closest genuine
// pair stays 1.4 times the worst drift apart, and a four percent x-height
// or one pixel baseline error costs a fifth of a pair distance. Sharper
// grids and single views lose those properties.
func Glyph() Encoder {
	return Dual{
		Pos:   Hash{W: 16, H: 16, Levels: 4, Smooth: 1},
		Tight: Hash{W: 12, H: 16, Levels: 4, Smooth: 1},
	}
}

// Line is the spec's configuration for line-level regions and spelled
// codewords.
func Line() Hash { return Hash{W: 64, H: 16, DHash: true} }

func (h Hash) levels() int { return max(1, h.Levels) }

// Bits is the code length.
func (h Hash) Bits() int {
	n := h.W * h.H * h.levels()
	if h.DHash {
		n += h.W * h.H
	}
	return n
}

// Name identifies the configuration in evidence.
func (h Hash) Name() string {
	s := fmt.Sprintf("hash-%dx%d", h.W, h.H)
	if h.levels() > 1 {
		s += fmt.Sprintf("x%d", h.levels())
	}
	if h.Smooth > 0 {
		s += fmt.Sprintf("s%d", h.Smooth)
	}
	if h.DHash {
		s += "+dhash"
	}
	return s
}

// smooth box-blurs a w×h field in place with the given radius, clamping at
// the edges.
func smooth(f []float64, w, h, r int) {
	if r <= 0 {
		return
	}
	tmp := make([]float64, len(f))
	for y := range h {
		for x := range w {
			sum, n := 0.0, 0
			for dx := -r; dx <= r; dx++ {
				xx := min(max(x+dx, 0), w-1)
				sum += f[y*w+xx]
				n++
			}
			tmp[y*w+x] = sum / float64(n)
		}
	}
	for y := range h {
		for x := range w {
			sum, n := 0.0, 0
			for dy := -r; dy <= r; dy++ {
				yy := min(max(y+dy, 0), h-1)
				sum += tmp[yy*w+x]
				n++
			}
			f[y*w+x] = sum / float64(n)
		}
	}
}

// Encode implements Encoder.
func (h Hash) Encode(p Patch) bitcode.Code {
	code := bitcode.New(h.Bits())
	levels := h.levels()
	if p.Bin != nil {
		cov := Coverage(p.Bin, h.W, h.H)
		smooth(cov, h.W, h.H, h.Smooth)
		for i, v := range cov {
			for k := range levels {
				if v > float64(2*k+1)/float64(2*levels) {
					code.Set(i*levels + k)
				}
			}
		}
	}
	if h.DHash && p.Gray != nil {
		g := GrayGrid(p.Gray, h.W+1, h.H)
		base := h.W * h.H * levels
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

// Dual concatenates two views of a glyph patch: Pos encodes the patch as
// given (a frame that carries size and position), Tight encodes the ink's
// own bounding box resampled to the grid (shape alone, whatever the size).
// The tight box is widened and heightened to at least MinTight of the
// patch's larger side, centred on the ink, so that a stem three pixels wide
// is not stretched across the whole grid where one pixel of rasterization
// phase would rewrite its code.
type Dual struct {
	Pos, Tight Hash
	MinTight   float64 // 0 means 0.25
}

// Bits is the code length.
func (d Dual) Bits() int { return d.Pos.Bits() + d.Tight.Bits() }

// Name identifies the configuration in evidence.
func (d Dual) Name() string { return d.Pos.Name() + "+" + d.Tight.Name() }

// Encode implements Encoder.
func (d Dual) Encode(p Patch) bitcode.Code {
	code := bitcode.New(d.Bits())
	pos := d.Pos.Encode(p)
	copy(code, pos)
	if p.Bin == nil {
		return code
	}
	ib, ok := p.Bin.InkBounds()
	if !ok {
		return code
	}
	frac := d.MinTight
	if frac == 0 {
		frac = 0.25
	}
	minSide := int(math.Ceil(frac * float64(max(p.Bin.W, p.Bin.H))))
	if w := ib.Dx(); w < minSide {
		ib.Min.X -= (minSide - w) / 2
		ib.Max.X = ib.Min.X + minSide
	}
	if h := ib.Dy(); h < minSide {
		ib.Min.Y -= (minSide - h) / 2
		ib.Max.Y = ib.Min.Y + minSide
	}
	tight := d.Tight.Encode(Patch{Bin: cropPadded(p.Bin, ib)})
	off := d.Pos.Bits()
	for i := range d.Tight.Bits() {
		if tight.Get(i) {
			code.Set(off + i)
		}
	}
	return code
}

// cropPadded copies r out of b, with background where r leaves the bitmap.
func cropPadded(b *bitmap.Bitmap, r image.Rectangle) *bitmap.Bitmap {
	out := bitmap.New(r.Dx(), r.Dy())
	for y := range out.H {
		for x := range out.W {
			out.Pix[y*out.W+x] = b.At(r.Min.X+x, r.Min.Y+y)
		}
	}
	return out
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
