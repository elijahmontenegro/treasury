package region

import (
	"image"
	"math"
	"sort"

	"treasury/internal/bitmap"
)

// SplitFused replaces components that are too wide to be one glyph by the
// pieces between the necks of their ink. A component wider than FusedFrac
// times the line's typical glyph width is cut at every column where the
// thickest ink crossing it is well under a stroke: the bridge where two
// letters touch after blur or through a serif. Strokes themselves, straight,
// diagonal, or arched, fill their columns at full thickness and are never
// cut, so an m or a w stays whole. The pieces are ordinary components
// afterwards; an alignment that disagrees with a cut re-joins the pieces
// through its split path, and one that finds a fusion the cutter missed
// merges through its merge path.
func SplitFused(b *bitmap.Bitmap, comps []Component, typical, medH float64, fusedFrac float64) []Component {
	if typical <= 0 || len(comps) == 0 {
		return comps
	}
	out := make([]Component, 0, len(comps))
	for _, c := range comps {
		w := float64(c.Box.Dx())
		// A capital or a tall glyph is wider than a lowercase one; its
		// allowance grows with its height, up to half again.
		allow := typical
		if medH > 0 {
			allow *= math.Max(1, math.Min(1.5, float64(c.Box.Dy())/medH))
		}
		if w <= fusedFrac*allow {
			out = append(out, c)
			continue
		}
		out = append(out, cutAtNecks(b, c, allow)...)
	}
	return out
}

// cutAtNecks cuts c at its necks and returns the ink bounds of each piece.
// A neck is a run of columns whose thickest ink is under three quarters of
// the component's stroke and whose ink count is under half the fullest
// column; the cut falls on the run's thinnest column. A component wide
// enough for three or more glyphs is also cut where a single thin run of ink
// crosses a column at a minimum of the profile, the bridge blur leaves
// between letters at a stroke's full thickness; that over-cuts an n or a u
// at its arch, which the alignment's split path rejoins, and is worth it
// because a word fused whole is otherwise lost. Pieces narrower than a
// quarter of typical, or two pixels, are not cut off: a crossbar end or a
// serif is part of its letter.
func cutAtNecks(b *bitmap.Bitmap, c Component, typical float64) []Component {
	crop := b.Crop(c.Box)
	w := crop.W
	if w < 4 {
		return []Component{c}
	}
	cues := measureColumns(crop)
	profile, runs, neck := cues.profile, cues.runs, cues.neck
	maxProfile, halfStroke := cues.maxProfile, cues.halfStroke
	if maxProfile == 0 || halfStroke <= 0 {
		return []Component{c}
	}
	minPiece := max(2, int(math.Round(0.25*typical)))
	wide := float64(w) > 2.5*typical
	thin := func(x int) bool {
		if neck[x] <= 0.75*halfStroke && profile[x] <= 0.5*maxProfile {
			return true
		}
		return wide && runs[x] == 1 && profile[x] <= 0.4*maxProfile
	}
	var cuts []int
	for x := minPiece; x < w-minPiece; {
		if !thin(x) {
			x++
			continue
		}
		// One cut per run of thin columns, at its thinnest.
		best, bestS := x, math.Inf(1)
		for ; x < w-minPiece && thin(x); x++ {
			if s := neck[x]/halfStroke + profile[x]/maxProfile; s < bestS {
				best, bestS = x, s
			}
		}
		if len(cuts) == 0 || best-cuts[len(cuts)-1] >= minPiece {
			cuts = append(cuts, best)
		}
	}
	if len(cuts) == 0 {
		return []Component{c}
	}
	sort.Ints(cuts)
	var pieces []Component
	start := 0
	for i := 0; i <= len(cuts); i++ {
		end := w
		if i < len(cuts) {
			end = cuts[i]
		}
		if p, ok := pieceBounds(crop, start, end, c.Box.Min); ok {
			pieces = append(pieces, p)
		}
		start = end
	}
	if len(pieces) < 2 {
		return []Component{c}
	}
	return pieces
}

func medianOf(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	sort.Float64s(v)
	return v[len(v)/2]
}

// pieceBounds is the ink bounds of columns [x0, x1) of crop as a component
// in image coordinates.
func pieceBounds(crop *bitmap.Bitmap, x0, x1 int, origin image.Point) (Component, bool) {
	minX, minY, maxX, maxY := crop.W, crop.H, -1, -1
	area := 0
	for y := range crop.H {
		for x := x0; x < x1; x++ {
			if crop.Pix[y*crop.W+x] == 0 {
				continue
			}
			area++
			minX, maxX = min(minX, x), max(maxX, x)
			minY, maxY = min(minY, y), max(maxY, y)
		}
	}
	if area == 0 {
		return Component{}, false
	}
	return Component{Box: image.Rect(minX, minY, maxX+1, maxY+1).Add(origin), Area: area}, true
}

// columnCues are the per-column cut cues of a component: ink count, number
// of separate ink runs, and the thickest ink crossing the column (the
// largest chamfer distance to background), with the fullest column and
// the median thickest ink, which is half the stroke width.
type columnCues struct {
	profile, neck []float64
	runs          []int
	maxProfile    float64
	halfStroke    float64
}

func measureColumns(crop *bitmap.Bitmap) columnCues {
	w, h := crop.W, crop.H
	cu := columnCues{profile: make([]float64, w), neck: make([]float64, w), runs: make([]int, w)}
	dt := crop.DistanceInside()
	for x := range w {
		inRun := false
		for y := range h {
			if crop.Pix[y*w+x] != 0 {
				cu.profile[x]++
				cu.neck[x] = math.Max(cu.neck[x], float64(dt[y*w+x]))
				if !inRun {
					cu.runs[x]++
					inRun = true
				}
			} else {
				inRun = false
			}
		}
		cu.maxProfile = math.Max(cu.maxProfile, cu.profile[x])
	}
	// The median over columns is robust to blobs and to the necks themselves.
	cu.halfStroke = medianOf(append([]float64(nil), cu.neck...))
	return cu
}
