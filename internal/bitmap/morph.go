package bitmap

import "math"

// DistanceInside returns, for every pixel, the chamfer (3-4) distance from
// an ink pixel to the nearest background pixel, in pixels; background is 0.
// The image border counts as background.
func (b *Bitmap) DistanceInside() []float32 {
	return chamfer(b, true)
}

// DistanceOutside returns, for every pixel, the distance from a background
// pixel to the nearest ink pixel; ink is 0.
func (b *Bitmap) DistanceOutside() []float32 {
	return chamfer(b, false)
}

func chamfer(b *Bitmap, inside bool) []float32 {
	w, h := b.W, b.H
	d := make([]float32, w*h)
	const inf = float32(1e9)
	for i, v := range b.Pix {
		if (v != 0) == inside {
			d[i] = inf
		}
	}
	at := func(x, y int) float32 {
		if x < 0 || y < 0 || x >= w || y >= h {
			if inside {
				return 0 // the border is background
			}
			return inf
		}
		return d[y*w+x]
	}
	for y := range h {
		for x := range w {
			i := y*w + x
			if d[i] == 0 {
				continue
			}
			d[i] = min(d[i], at(x-1, y)+3, at(x, y-1)+3, at(x-1, y-1)+4, at(x+1, y-1)+4)
		}
	}
	for y := h - 1; y >= 0; y-- {
		for x := w - 1; x >= 0; x-- {
			i := y*w + x
			if d[i] == 0 {
				continue
			}
			d[i] = min(d[i], at(x+1, y)+3, at(x, y+1)+3, at(x+1, y+1)+4, at(x-1, y+1)+4)
		}
	}
	for i := range d {
		if d[i] >= inf {
			d[i] = 0
		} else {
			d[i] /= 3
		}
	}
	return d
}

// StrokeWidth estimates the typical stroke width in pixels from the mean
// inside distance over ink: across a stroke of width w the distances run
// 1, 2, …, w/2 and back, whose mean is w/4 + 1/2.
func (b *Bitmap) StrokeWidth() float64 {
	d := b.DistanceInside()
	sum, n := 0.0, 0
	for i, v := range b.Pix {
		if v != 0 {
			sum += float64(d[i])
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return math.Max(1, 4*sum/float64(n)-2)
}

// Erode removes ink whose centre lies within r pixels of the ink edge. The
// chamfer distance is measured between pixel centres, and the edge sits
// half a pixel from the outermost ink centre, so a pixel at distance d is
// d − ½ from the edge.
func (b *Bitmap) Erode(r float64) *Bitmap {
	out := New(b.W, b.H)
	if r <= 0 {
		copy(out.Pix, b.Pix)
		return out
	}
	d := b.DistanceInside()
	for i, v := range b.Pix {
		if v != 0 && float64(d[i])-0.5 >= r {
			out.Pix[i] = 1
		}
	}
	return out
}

// Dilate adds background whose centre lies within r pixels of the ink edge.
func (b *Bitmap) Dilate(r float64) *Bitmap {
	out := New(b.W, b.H)
	copy(out.Pix, b.Pix)
	if r <= 0 {
		return out
	}
	d := b.DistanceOutside()
	for i, v := range b.Pix {
		if v == 0 && float64(d[i])-0.5 <= r {
			out.Pix[i] = 1
		}
	}
	return out
}

// Upsample repeats every pixel k×k times.
func (b *Bitmap) Upsample(k int) *Bitmap {
	out := New(b.W*k, b.H*k)
	for y := range out.H {
		row := b.Pix[(y/k)*b.W : (y/k+1)*b.W]
		for x := range out.W {
			out.Pix[y*out.W+x] = row[x/k]
		}
	}
	return out
}

// Downsample averages k×k blocks and keeps cells at least half inked.
func (b *Bitmap) Downsample(k int) *Bitmap {
	out := New(b.W/k, b.H/k)
	for y := range out.H {
		for x := range out.W {
			n := 0
			for dy := range k {
				for dx := range k {
					n += int(b.Pix[(y*k+dy)*b.W+x*k+dx])
				}
			}
			if 2*n >= k*k {
				out.Pix[y*out.W+x] = 1
			}
		}
	}
	return out
}

// WithStroke returns a copy whose stroke width is brought to target px by
// eroding or dilating by half the difference. Strokes are a few pixels
// wide, so the morphology runs at four times the resolution.
func (b *Bitmap) WithStroke(target float64) *Bitmap {
	cur := b.StrokeWidth()
	if cur == 0 || target <= 0 {
		return b.Erode(0)
	}
	const k = 4
	delta := (cur - target) / 2 * k
	if math.Abs(delta) < 0.5 {
		return b.Erode(0)
	}
	up := b.Upsample(k)
	if delta > 0 {
		return up.Erode(delta).Downsample(k)
	}
	return up.Dilate(-delta).Downsample(k)
}
