// Package bitmap is a binary raster where 1 means ink.
package bitmap

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
)

// Bitmap is a row-major binary image. Pix[y*W+x] is 1 for ink, 0 for background.
type Bitmap struct {
	W, H int
	Pix  []uint8
}

// New returns an all-background bitmap.
func New(w, h int) *Bitmap {
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	return &Bitmap{W: w, H: h, Pix: make([]uint8, w*h)}
}

// Bounds is the rectangle (0,0)-(W,H).
func (b *Bitmap) Bounds() image.Rectangle { return image.Rect(0, 0, b.W, b.H) }

// At returns the pixel, or 0 outside the bitmap.
func (b *Bitmap) At(x, y int) uint8 {
	if x < 0 || y < 0 || x >= b.W || y >= b.H {
		return 0
	}
	return b.Pix[y*b.W+x]
}

// Set writes the pixel; out-of-range writes are ignored.
func (b *Bitmap) Set(x, y int, v uint8) {
	if x < 0 || y < 0 || x >= b.W || y >= b.H {
		return
	}
	b.Pix[y*b.W+x] = v
}

// Count returns the number of ink pixels.
func (b *Bitmap) Count() int {
	n := 0
	for _, v := range b.Pix {
		n += int(v)
	}
	return n
}

// CountIn returns the number of ink pixels inside r.
func (b *Bitmap) CountIn(r image.Rectangle) int {
	r = r.Intersect(b.Bounds())
	n := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for _, v := range b.Pix[y*b.W+r.Min.X : y*b.W+r.Max.X] {
			n += int(v)
		}
	}
	return n
}

// Crop copies r ∩ Bounds into a new bitmap whose origin is the clamped r.Min.
func (b *Bitmap) Crop(r image.Rectangle) *Bitmap {
	r = r.Intersect(b.Bounds())
	out := New(r.Dx(), r.Dy())
	for y := 0; y < out.H; y++ {
		src := (r.Min.Y+y)*b.W + r.Min.X
		copy(out.Pix[y*out.W:(y+1)*out.W], b.Pix[src:src+out.W])
	}
	return out
}

// InkBounds is the tight bounding box of ink; ok is false when there is none.
func (b *Bitmap) InkBounds() (r image.Rectangle, ok bool) {
	minX, minY, maxX, maxY := b.W, b.H, -1, -1
	for y := 0; y < b.H; y++ {
		row := b.Pix[y*b.W : (y+1)*b.W]
		for x, v := range row {
			if v == 0 {
				continue
			}
			if x < minX {
				minX = x
			}
			if x > maxX {
				maxX = x
			}
			if y < minY {
				minY = y
			}
			if y > maxY {
				maxY = y
			}
		}
	}
	if maxX < 0 {
		return image.Rectangle{}, false
	}
	return image.Rect(minX, minY, maxX+1, maxY+1), true
}

// FromGray marks pixels darker than thresh as ink.
func FromGray(g *image.Gray, thresh uint8) *Bitmap {
	w, h := g.Rect.Dx(), g.Rect.Dy()
	b := New(w, h)
	for y := 0; y < h; y++ {
		off := g.PixOffset(g.Rect.Min.X, g.Rect.Min.Y+y)
		row := g.Pix[off : off+w]
		for x, v := range row {
			if v < thresh {
				b.Pix[y*w+x] = 1
			}
		}
	}
	return b
}

// ToGray renders ink as black on white.
func (b *Bitmap) ToGray() *image.Gray {
	g := image.NewGray(b.Bounds())
	for i, v := range b.Pix {
		if v != 0 {
			g.Pix[i] = 0
		} else {
			g.Pix[i] = 255
		}
	}
	return g
}

// WritePNG writes img to path, creating parent directories.
func WritePNG(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
