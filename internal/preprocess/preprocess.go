// Package preprocess turns an input image into a deskewed grayscale and binary
// pair, plus an optional glare mask.
package preprocess

import (
	"image"
	"math"

	"treasury/internal/bitmap"
	"treasury/internal/imgops"
)

// Params are the preprocessing knobs; Default matches the spec.
type Params struct {
	LongSide      int     // target longer side in px
	Window        int     // Sauvola window (odd)
	K             float64 // Sauvola k
	R             float64 // Sauvola dynamic range of the standard deviation
	MaxSkewDeg    float64 // deskew sweep is ±MaxSkewDeg
	GlareQuantile float64 // pixels at or above this intensity quantile are glare
	GlareMinGap   int     // the glare level must exceed the median by this much, else no mask
}

// Default returns the spec's parameters.
func Default() Params {
	return Params{LongSide: 1600, Window: 31, K: 0.2, R: 128, MaxSkewDeg: 15, GlareQuantile: 0.99, GlareMinGap: 16}
}

// Result is the preprocessed image. Gray and Bin share coordinates.
type Result struct {
	Gray     *image.Gray    // resized and deskewed grayscale
	Bin      *bitmap.Bitmap // Sauvola threshold of Gray, ink = 1
	Glare    *bitmap.Bitmap // 1 where the image is unreliable; nil when no mask applies
	AngleDeg float64        // skew estimated on the binary; Rotate(gray, -AngleDeg) was applied
	Scale    float64        // Gray size / input size
}

// Run preprocesses img.
func Run(img image.Image, p Params) (*Result, error) {
	g := imgops.ToGray(img)
	w, h := g.Rect.Dx(), g.Rect.Dy()
	long := max(w, h)
	scale := 1.0
	if long > 0 && p.LongSide > 0 {
		scale = float64(p.LongSide) / float64(long)
		g = imgops.Resize(g, int(math.Round(float64(w)*scale)), int(math.Round(float64(h)*scale)))
	}
	glare := GlareMask(g, p.GlareQuantile, p.GlareMinGap)
	bin := Sauvola(g, p.Window, p.K, p.R)
	angle := EstimateSkew(bin, p.MaxSkewDeg)
	if math.Abs(angle) >= 0.05 {
		g = imgops.Rotate(g, -angle, 255)
		bin = Sauvola(g, p.Window, p.K, p.R)
		if glare != nil {
			glare = bitmap.FromGray(imgops.Rotate(glare.ToGray(), -angle, 255), 128)
		}
	}
	return &Result{Gray: g, Bin: bin, Glare: glare, AngleDeg: angle, Scale: scale}, nil
}

// Sauvola thresholds g with T = m·(1 + k·(s/R − 1)) over a window×window
// neighbourhood; pixels darker than T are ink.
func Sauvola(g *image.Gray, window int, k, R float64) *bitmap.Bitmap {
	w, h := g.Rect.Dx(), g.Rect.Dy()
	out := bitmap.New(w, h)
	if w == 0 || h == 0 {
		return out
	}
	stride := w + 1
	s1 := make([]float64, (w+1)*(h+1))
	s2 := make([]float64, (w+1)*(h+1))
	for y := 1; y <= h; y++ {
		off := g.PixOffset(g.Rect.Min.X, g.Rect.Min.Y+y-1)
		row := g.Pix[off : off+w]
		var rs, rs2 float64
		for x := 1; x <= w; x++ {
			v := float64(row[x-1])
			rs += v
			rs2 += v * v
			s1[y*stride+x] = s1[(y-1)*stride+x] + rs
			s2[y*stride+x] = s2[(y-1)*stride+x] + rs2
		}
	}
	half := window / 2
	for y := 0; y < h; y++ {
		y0, y1 := max(0, y-half), min(h, y+half+1)
		off := g.PixOffset(g.Rect.Min.X, g.Rect.Min.Y+y)
		row := g.Pix[off : off+w]
		for x := 0; x < w; x++ {
			x0, x1 := max(0, x-half), min(w, x+half+1)
			n := float64((x1 - x0) * (y1 - y0))
			sum := s1[y1*stride+x1] - s1[y0*stride+x1] - s1[y1*stride+x0] + s1[y0*stride+x0]
			sum2 := s2[y1*stride+x1] - s2[y0*stride+x1] - s2[y1*stride+x0] + s2[y0*stride+x0]
			m := sum / n
			v := sum2/n - m*m
			if v < 0 {
				v = 0
			}
			t := m * (1 + k*(math.Sqrt(v)/R-1))
			if float64(row[x]) < t {
				out.Pix[y*w+x] = 1
			}
		}
	}
	return out
}

// GlareMask marks pixels at or above the q quantile of intensity. It returns
// nil when that level is within minGap of the median: on a clean scan the
// brightest percentile is plain paper and masking it would flag everything.
func GlareMask(g *image.Gray, q float64, minGap int) *bitmap.Bitmap {
	w, h := g.Rect.Dx(), g.Rect.Dy()
	if w == 0 || h == 0 {
		return nil
	}
	var hist [256]int
	for y := 0; y < h; y++ {
		off := g.PixOffset(g.Rect.Min.X, g.Rect.Min.Y+y)
		for _, v := range g.Pix[off : off+w] {
			hist[v]++
		}
	}
	quantile := func(f float64) int {
		target := int(math.Ceil(f * float64(w*h)))
		acc := 0
		for i, c := range hist {
			acc += c
			if acc >= target {
				return i
			}
		}
		return 255
	}
	level, median := quantile(q), quantile(0.5)
	if level-median < minGap {
		return nil
	}
	mask := bitmap.New(w, h)
	for y := 0; y < h; y++ {
		off := g.PixOffset(g.Rect.Min.X, g.Rect.Min.Y+y)
		for x, v := range g.Pix[off : off+w] {
			if int(v) >= level {
				mask.Pix[y*w+x] = 1
			}
		}
	}
	return mask
}

// EstimateSkew returns the text angle in degrees, in Rotate's convention, by
// sweeping ±maxDeg for the rotation that concentrates ink into the fewest
// rows (maximum squared row-projection). Coarse step 0.5°, fine step 0.1°.
func EstimateSkew(b *bitmap.Bitmap, maxDeg float64) float64 {
	var xs, ys []float32
	total := b.Count()
	if total < 50 {
		return 0
	}
	stride := max(1, total/60000)
	i := 0
	for y := 0; y < b.H; y++ {
		row := b.Pix[y*b.W : (y+1)*b.W]
		for x, v := range row {
			if v == 0 {
				continue
			}
			if i%stride == 0 {
				xs = append(xs, float32(x))
				ys = append(ys, float32(y))
			}
			i++
		}
	}
	cx, cy := float32(b.W)/2, float32(b.H)/2
	diag := int(math.Hypot(float64(b.W), float64(b.H))) + 2
	hist := make([]int32, diag+1)
	score := func(deg float64) float64 {
		for j := range hist {
			hist[j] = 0
		}
		t := -deg * math.Pi / 180
		c, s := float32(math.Cos(t)), float32(math.Sin(t))
		off := float32(diag)/2 - cy
		for j := range xs {
			yy := cy + s*(xs[j]-cx) + c*(ys[j]-cy) + off
			k := int(yy)
			if k >= 0 && k <= diag {
				hist[k]++
			}
		}
		var sum float64
		for _, v := range hist {
			sum += float64(v) * float64(v)
		}
		return sum
	}
	best, bestScore := 0.0, -1.0
	for deg := -maxDeg; deg <= maxDeg+1e-9; deg += 0.5 {
		if sc := score(deg); sc > bestScore {
			best, bestScore = deg, sc
		}
	}
	coarse := best
	for deg := coarse - 0.5; deg <= coarse+0.5+1e-9; deg += 0.1 {
		if sc := score(deg); sc > bestScore {
			best, bestScore = deg, sc
		}
	}
	return math.Round(best*10) / 10
}
