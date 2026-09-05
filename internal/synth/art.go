package synth

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"math/rand"
)

// Art is the label under the text.
//
// A label is not text on paper. It is a printed thing with a ground, panels
// and bands, borders and rules, a pattern or a photograph, a barcode, and
// ornament, and the text is composited over all of it. What is here is what
// the fifty real labels are made of, measured rather than assumed: the
// shares, greys and contrasts are recorded in docs/approach.md against the
// measurement each came from.
type Art struct {
	Ground    uint8 // the label's own grey
	Gradient  float64
	Angle     float64 // the gradient's direction in radians
	Panels    []Panel
	Rules     []Panel
	Pattern   *Pattern
	Texture   float64 // amplitude of a printed texture, as a fraction of 255
	Barcode   *Barcode
	Ornaments []Ornament
}

// Panel is a band or a block of its own grey: what light text sits on.
type Panel struct {
	Rect image.Rectangle
	Grey uint8
}

// Pattern is a repeated decorative mark over the ground.
type Pattern struct {
	Cell   int
	Radius int
	Grey   uint8
	Rect   image.Rectangle
}

// Barcode is a field of bars. Every second real label carries one.
type Barcode struct {
	Rect image.Rectangle
	Grey uint8
}

// Ornament is a decorative shape, which text may overlap.
type Ornament struct {
	Rect  image.Rectangle
	Grey  uint8
	Round bool
}

// Draw lays the artwork into img before any text is set on it.
func (a *Art) Draw(img *image.Gray, rng *rand.Rand) {
	if a == nil {
		for i := range img.Pix {
			img.Pix[i] = 255
		}
		return
	}
	w, h := img.Rect.Dx(), img.Rect.Dy()
	dx, dy := math.Cos(a.Angle), math.Sin(a.Angle)
	span := math.Abs(dx)*float64(w) + math.Abs(dy)*float64(h)
	for y := range h {
		for x := range w {
			v := float64(a.Ground)
			if a.Gradient != 0 && span > 0 {
				t := (float64(x)*dx + float64(y)*dy) / span
				v += a.Gradient * (t - 0.5) * 255
			}
			img.Pix[y*w+x] = clamp(v)
		}
	}
	if a.Pattern != nil {
		p := a.Pattern
		r := p.Rect.Intersect(img.Rect)
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				cx, cy := x%max(1, p.Cell), y%max(1, p.Cell)
				d := math.Hypot(float64(cx-p.Cell/2), float64(cy-p.Cell/2))
				if d <= float64(p.Radius) {
					img.Pix[y*w+x] = p.Grey
				}
			}
		}
	}
	for _, pn := range a.Panels {
		fill(img, pn.Rect, pn.Grey)
	}
	for _, o := range a.Ornaments {
		if o.Round {
			ellipse(img, o.Rect, o.Grey)
			continue
		}
		fill(img, o.Rect, o.Grey)
	}
	for _, rl := range a.Rules {
		fill(img, rl.Rect, rl.Grey)
	}
	if a.Barcode != nil {
		bars(img, a.Barcode.Rect, a.Barcode.Grey, rng)
	}
	if a.Texture > 0 {
		amp := a.Texture * 255
		for i := range img.Pix {
			img.Pix[i] = clamp(float64(img.Pix[i]) + rng.NormFloat64()*amp)
		}
	}
}

// GreyAt is the artwork's grey at a point, for choosing an ink that stands
// against what it is printed on rather than against the page.
func (a *Art) GreyAt(img *image.Gray, r image.Rectangle) float64 {
	r = r.Intersect(img.Rect)
	if r.Empty() {
		return 255
	}
	w := img.Rect.Dx()
	sum := 0.0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			sum += float64(img.Pix[y*w+x])
		}
	}
	return sum / float64(r.Dx()*r.Dy())
}

func fill(img *image.Gray, r image.Rectangle, g uint8) {
	draw.Draw(img, r.Intersect(img.Rect), image.NewUniform(color.Gray{Y: g}), image.Point{}, draw.Src)
}

func ellipse(img *image.Gray, r image.Rectangle, g uint8) {
	r = r.Intersect(img.Rect)
	cx, cy := float64(r.Min.X+r.Max.X)/2, float64(r.Min.Y+r.Max.Y)/2
	rx, ry := float64(r.Dx())/2, float64(r.Dy())/2
	if rx <= 0 || ry <= 0 {
		return
	}
	w := img.Rect.Dx()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			u, v := (float64(x)-cx)/rx, (float64(y)-cy)/ry
			if u*u+v*v <= 1 {
				img.Pix[y*w+x] = g
			}
		}
	}
}

// bars draws a barcode: a field of bars of varying width sharing a top and
// a bottom, which is what tells one from a line of type.
func bars(img *image.Gray, r image.Rectangle, g uint8, rng *rand.Rand) {
	r = r.Intersect(img.Rect)
	unit := max(1, r.Dx()/60)
	x := r.Min.X
	for x < r.Max.X-unit {
		wdt := unit * (1 + rng.Intn(3))
		fill(img, image.Rect(x, r.Min.Y, min(x+wdt, r.Max.X), r.Max.Y), g)
		x += wdt + unit*(1+rng.Intn(2))
	}
}

func clamp(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(math.Round(v))
}
