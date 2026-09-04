// Package imgops holds the grayscale image operations shared by the engine and
// the generator. All grays produced here are canonical: origin (0,0) and
// Stride == width.
package imgops

import (
	"bytes"
	"image"
	"image/draw"
	"image/jpeg"
	"math"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/math/f64"
)

// ToGray converts any image to a canonical 8-bit grayscale.
func ToGray(img image.Image) *image.Gray {
	b := img.Bounds()
	g := image.NewGray(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(g, g.Rect, img, b.Min, draw.Src)
	return g
}

// Resize scales g to w×h with a Catmull-Rom kernel.
func Resize(g *image.Gray, w, h int) *image.Gray {
	dst := image.NewGray(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Rect, g, g.Rect, xdraw.Src, nil)
	return dst
}

// Rotate rotates g by deg about its centre, keeping the canvas size and
// filling uncovered pixels with fill. Positive angles follow image
// coordinates (y down), so they appear clockwise on screen. RotatePoint and
// RotateRect use the same convention, and Rotate(Rotate(g, a), -a) restores g
// up to resampling.
func Rotate(g *image.Gray, deg float64, fill uint8) *image.Gray {
	dst := image.NewGray(image.Rect(0, 0, g.Rect.Dx(), g.Rect.Dy()))
	for i := range dst.Pix {
		dst.Pix[i] = fill
	}
	cx, cy := float64(g.Rect.Dx())/2, float64(g.Rect.Dy())/2
	t := deg * math.Pi / 180
	c, s := math.Cos(t), math.Sin(t)
	// m maps src points to dst points.
	m := f64.Aff3{c, -s, cx - c*cx + s*cy, s, c, cy - s*cx - c*cy}
	xdraw.BiLinear.Transform(dst, m, g, g.Rect, xdraw.Src, nil)
	return dst
}

// RotatePoint rotates (x, y) about (cx, cy) by deg, Rotate's convention.
func RotatePoint(x, y, cx, cy, deg float64) (float64, float64) {
	t := deg * math.Pi / 180
	c, s := math.Cos(t), math.Sin(t)
	dx, dy := x-cx, y-cy
	return cx + c*dx - s*dy, cy + s*dx + c*dy
}

// RotateRect returns the bounding box of r's corners after RotatePoint.
func RotateRect(r image.Rectangle, cx, cy, deg float64) image.Rectangle {
	xs := [4]float64{float64(r.Min.X), float64(r.Max.X), float64(r.Max.X), float64(r.Min.X)}
	ys := [4]float64{float64(r.Min.Y), float64(r.Min.Y), float64(r.Max.Y), float64(r.Max.Y)}
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for i := range xs {
		x, y := RotatePoint(xs[i], ys[i], cx, cy, deg)
		minX = math.Min(minX, x)
		maxX = math.Max(maxX, x)
		minY = math.Min(minY, y)
		maxY = math.Max(maxY, y)
	}
	return image.Rect(int(math.Floor(minX)), int(math.Floor(minY)), int(math.Ceil(maxX)), int(math.Ceil(maxY)))
}

// GaussianBlur blurs with a separable kernel of radius ceil(3σ). σ ≤ 0 copies.
func GaussianBlur(g *image.Gray, sigma float64) *image.Gray {
	w, h := g.Rect.Dx(), g.Rect.Dy()
	out := image.NewGray(image.Rect(0, 0, w, h))
	if sigma <= 0 {
		draw.Draw(out, out.Rect, g, g.Rect.Min, draw.Src)
		return out
	}
	r := int(math.Ceil(3 * sigma))
	k := make([]float64, 2*r+1)
	sum := 0.0
	for i := -r; i <= r; i++ {
		k[i+r] = math.Exp(-float64(i*i) / (2 * sigma * sigma))
		sum += k[i+r]
	}
	for i := range k {
		k[i] /= sum
	}
	tmp := make([]float64, w*h)
	for y := 0; y < h; y++ {
		off := g.PixOffset(g.Rect.Min.X, g.Rect.Min.Y+y)
		row := g.Pix[off : off+w]
		for x := 0; x < w; x++ {
			acc := 0.0
			for i := -r; i <= r; i++ {
				xx := x + i
				if xx < 0 {
					xx = 0
				} else if xx >= w {
					xx = w - 1
				}
				acc += k[i+r] * float64(row[xx])
			}
			tmp[y*w+x] = acc
		}
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			acc := 0.0
			for i := -r; i <= r; i++ {
				yy := y + i
				if yy < 0 {
					yy = 0
				} else if yy >= h {
					yy = h - 1
				}
				acc += k[i+r] * tmp[yy*w+x]
			}
			out.Pix[y*w+x] = uint8(math.Round(math.Max(0, math.Min(255, acc))))
		}
	}
	return out
}

// JPEGRoundTrip encodes at the given quality and decodes back.
func JPEGRoundTrip(g *image.Gray, quality int) (*image.Gray, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, g, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	img, err := jpeg.Decode(&buf)
	if err != nil {
		return nil, err
	}
	return ToGray(img), nil
}

// Invert returns the negative of g: light type on a dark ground becomes
// dark type on light, which is what thresholding expects.
func Invert(g *image.Gray) *image.Gray {
	out := image.NewGray(g.Rect)
	for i, v := range g.Pix {
		out.Pix[i] = 255 - v
	}
	return out
}

// Rotate90 rotates g by quarter turns clockwise, exactly, with no
// resampling; the canvas changes shape with the image.
func Rotate90(g *image.Gray, quarters int) *image.Gray {
	quarters = ((quarters % 4) + 4) % 4
	if quarters == 0 {
		return g
	}
	w, h := g.Rect.Dx(), g.Rect.Dy()
	var out *image.Gray
	switch quarters {
	case 2:
		out = image.NewGray(image.Rect(0, 0, w, h))
	default:
		out = image.NewGray(image.Rect(0, 0, h, w))
	}
	for y := range h {
		for x := range w {
			v := g.Pix[(y+g.Rect.Min.Y-g.Rect.Min.Y)*g.Stride+x]
			switch quarters {
			case 1:
				out.Pix[x*out.Stride+(h-1-y)] = v
			case 2:
				out.Pix[(h-1-y)*out.Stride+(w-1-x)] = v
			case 3:
				out.Pix[(w-1-x)*out.Stride+y] = v
			}
		}
	}
	return out
}
