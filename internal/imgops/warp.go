package imgops

import (
	"image"
	"math"
)

// Homography is a 3×3 projective map in row-major order.
type Homography [9]float64

// Apply maps a point.
func (h Homography) Apply(x, y float64) (float64, float64) {
	d := h[6]*x + h[7]*y + h[8]
	if d == 0 {
		d = 1e-12
	}
	return (h[0]*x + h[1]*y + h[2]) / d, (h[3]*x + h[4]*y + h[5]) / d
}

// Inverse returns the inverse map.
func (h Homography) Inverse() Homography {
	a, b, c := h[0], h[1], h[2]
	d, e, f := h[3], h[4], h[5]
	g, hh, i := h[6], h[7], h[8]
	det := a*(e*i-f*hh) - b*(d*i-f*g) + c*(d*hh-e*g)
	if det == 0 {
		det = 1e-12
	}
	return Homography{
		(e*i - f*hh) / det, (c*hh - b*i) / det, (b*f - c*e) / det,
		(f*g - d*i) / det, (a*i - c*g) / det, (c*d - a*f) / det,
		(d*hh - e*g) / det, (b*g - a*hh) / det, (a*e - b*d) / det,
	}
}

// HomographyFrom solves the map that sends four source points to four
// destination points (direct linear transform, eight unknowns).
func HomographyFrom(src, dst [4][2]float64) Homography {
	var m [8][9]float64
	for i := range 4 {
		x, y := src[i][0], src[i][1]
		u, v := dst[i][0], dst[i][1]
		m[2*i] = [9]float64{x, y, 1, 0, 0, 0, -u * x, -u * y, u}
		m[2*i+1] = [9]float64{0, 0, 0, x, y, 1, -v * x, -v * y, v}
	}
	// Gaussian elimination with partial pivoting.
	for col := range 8 {
		pivot := col
		for r := col + 1; r < 8; r++ {
			if math.Abs(m[r][col]) > math.Abs(m[pivot][col]) {
				pivot = r
			}
		}
		m[col], m[pivot] = m[pivot], m[col]
		p := m[col][col]
		if p == 0 {
			continue
		}
		for k := col; k < 9; k++ {
			m[col][k] /= p
		}
		for r := range 8 {
			if r == col {
				continue
			}
			f := m[r][col]
			for k := col; k < 9; k++ {
				m[r][k] -= f * m[col][k]
			}
		}
	}
	var h Homography
	for i := range 8 {
		h[i] = m[i][8]
	}
	h[8] = 1
	return h
}

// Warp resamples g through H (source to destination) with bilinear
// interpolation, keeping the canvas size and filling uncovered pixels.
func Warp(g *image.Gray, H Homography, fill uint8) *image.Gray {
	w, h := g.Rect.Dx(), g.Rect.Dy()
	out := image.NewGray(image.Rect(0, 0, w, h))
	inv := H.Inverse()
	at := func(x, y int) float64 {
		if x < 0 || y < 0 || x >= w || y >= h {
			return float64(fill)
		}
		return float64(g.Pix[g.PixOffset(g.Rect.Min.X+x, g.Rect.Min.Y+y)])
	}
	for y := range h {
		for x := range w {
			sx, sy := inv.Apply(float64(x)+0.5, float64(y)+0.5)
			sx -= 0.5
			sy -= 0.5
			if sx < -1 || sy < -1 || sx > float64(w) || sy > float64(h) {
				out.Pix[y*w+x] = fill
				continue
			}
			x0, y0 := int(math.Floor(sx)), int(math.Floor(sy))
			fx, fy := sx-float64(x0), sy-float64(y0)
			v := (1-fy)*((1-fx)*at(x0, y0)+fx*at(x0+1, y0)) + fy*((1-fx)*at(x0, y0+1)+fx*at(x0+1, y0+1))
			out.Pix[y*w+x] = uint8(math.Round(math.Max(0, math.Min(255, v))))
		}
	}
	return out
}

// WarpRect maps a rectangle's corners through H and returns their box.
func WarpRect(r image.Rectangle, H Homography) image.Rectangle {
	xs := [4]float64{float64(r.Min.X), float64(r.Max.X), float64(r.Max.X), float64(r.Min.X)}
	ys := [4]float64{float64(r.Min.Y), float64(r.Min.Y), float64(r.Max.Y), float64(r.Max.Y)}
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for i := range xs {
		x, y := H.Apply(xs[i], ys[i])
		minX, maxX = math.Min(minX, x), math.Max(maxX, x)
		minY, maxY = math.Min(minY, y), math.Max(maxY, y)
	}
	return image.Rect(int(math.Floor(minX)), int(math.Floor(minY)), int(math.Ceil(maxX)), int(math.Ceil(maxY)))
}

// Levels applies v' = contrast·(v − 128) + 128 + brightness.
func Levels(g *image.Gray, contrast, brightness float64) *image.Gray {
	w, h := g.Rect.Dx(), g.Rect.Dy()
	out := image.NewGray(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			v := float64(g.Pix[g.PixOffset(g.Rect.Min.X+x, g.Rect.Min.Y+y)])
			v = contrast*(v-128) + 128 + brightness
			out.Pix[y*w+x] = uint8(math.Round(math.Max(0, math.Min(255, v))))
		}
	}
	return out
}

// Glare brightens a radial blob: pixels within r of (cx, cy) move toward
// white by strength at the centre, falling off to zero at the edge.
func Glare(g *image.Gray, cx, cy, r, strength float64) *image.Gray {
	w, h := g.Rect.Dx(), g.Rect.Dy()
	out := image.NewGray(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			v := float64(g.Pix[g.PixOffset(g.Rect.Min.X+x, g.Rect.Min.Y+y)])
			d := math.Hypot(float64(x)-cx, float64(y)-cy)
			if d < r {
				k := strength * (1 - d/r) * (1 - d/r)
				v = v + (255-v)*k
			}
			out.Pix[y*w+x] = uint8(math.Round(math.Max(0, math.Min(255, v))))
		}
	}
	return out
}
