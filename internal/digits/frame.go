// Package digits classifies a glyph frame as one of the ten digits, the
// percent sign, the period, the comma, or something else. The reference
// text never teaches digits, so they are the one place the engine carries
// prior knowledge of shapes: a small convolutional net trained on the
// classes rendered in many faces through the same channel as the labels.
package digits

import (
	"image"
	"math"
	"sort"
)

// Side is the frame's side in pixels, as the classifier sees it.
const Side = 32

// Classes are the labels in output order; Other is the last.
const Classes = "0123456789%.,?"

// Other is the class index of a glyph that is none of the digit classes.
const Other = len(Classes) - 1

// FrameW and FrameH are the frame's extent in x-heights: the alphabet
// frame's height (1.6 above the baseline to 0.6 below) but narrower, so
// the glyph fills the patch and its neighbours stay at the edges.
var (
	FrameW = 1.6
	FrameH = 2.2
)

// Frame samples the gray image around a glyph into a Side×Side patch:
// FrameW x-heights wide and FrameH tall, top 1.6 x-heights above the
// baseline, centred on the glyph's centre column. Ink is bright and
// background dark, scaled by the patch's own contrast so lighting and print
// density cancel; a patch with no contrast is all zero. Values are in
// [0, 1].
func Frame(gray *image.Gray, box image.Rectangle, baseline int, xh float64) []float32 {
	return FrameAt(gray, box, baseline, xh, 1, 0)
}

// FrameAt is Frame with the source window widened by 1/hscale, so the
// glyph appears condensed (hscale < 1) or extended, and slanted by shear
// (the horizontal offset per unit of height above the baseline). Training
// draws these to stand in for condensed, extended, and italic faces; the
// engine always frames at 1 and 0.
func FrameAt(gray *image.Gray, box image.Rectangle, baseline int, xh, hscale, shear float64) []float32 {
	width, height := FrameW*xh/hscale, FrameH*xh
	top := float64(baseline) - 1.6*xh
	left := float64(box.Min.X+box.Max.X)/2 - width/2
	out := make([]float32, Side*Side)
	cw, ch := width/Side, height/Side
	for oy := range Side {
		y0, y1 := top+float64(oy)*ch, top+float64(oy+1)*ch
		// Slant: shift the row by its height above the baseline.
		dx := shear * (float64(baseline) - (y0+y1)/2)
		for ox := range Side {
			// Area-average the source cell; outside the image is background.
			x0, x1 := left+dx+float64(ox)*cw, left+dx+float64(ox+1)*cw
			out[oy*Side+ox] = float32(meanGray(gray, x0, y0, x1, y1))
		}
	}
	// Normalize: background is the bright tail, ink the darkest cells. The
	// ink level comes from the third darkest cell, not a percentile: a
	// period covers under two percent of the patch and a percentile would
	// land in the background.
	sorted := append([]float32(nil), out...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	ink := sorted[2]
	bg := sorted[len(sorted)*19/20]
	if bg-ink < 12 {
		for i := range out {
			out[i] = 0
		}
		return out
	}
	for i, v := range out {
		out[i] = float32(math.Max(0, math.Min(1, float64((bg-v)/(bg-ink)))))
	}
	return out
}

// meanGray is the mean gray over a real-valued rectangle, sampling at pixel
// centres; pixels outside the image count as background.
func meanGray(g *image.Gray, x0, y0, x1, y1 float64) float64 {
	sum, n := 0.0, 0
	ix0, ix1 := int(math.Floor(x0)), int(math.Ceil(x1))
	iy0, iy1 := int(math.Floor(y0)), int(math.Ceil(y1))
	if ix1 <= ix0 {
		ix1 = ix0 + 1
	}
	if iy1 <= iy0 {
		iy1 = iy0 + 1
	}
	b := g.Rect
	for y := iy0; y < iy1; y++ {
		for x := ix0; x < ix1; x++ {
			// Weight by the overlap of the pixel with the cell.
			w := (math.Min(x1, float64(x+1)) - math.Max(x0, float64(x))) * (math.Min(y1, float64(y+1)) - math.Max(y0, float64(y)))
			if w <= 0 {
				continue
			}
			v := 255.0
			if x >= b.Min.X && x < b.Max.X && y >= b.Min.Y && y < b.Max.Y {
				v = float64(g.Pix[(y-b.Min.Y)*g.Stride+(x-b.Min.X)])
			}
			sum += v * w
			n++
		}
	}
	if n == 0 {
		return 255
	}
	area := (x1 - x0) * (y1 - y0)
	if area <= 0 {
		return 255
	}
	return sum / area
}

// ClassOf is the class index of a character, Other when it is not a digit
// class.
func ClassOf(r rune) int {
	for i, c := range Classes {
		if c == r && i != Other {
			return i
		}
	}
	return Other
}
