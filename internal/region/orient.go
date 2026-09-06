package region

import (
	"fmt"
	"image"
	"math"
	"sort"
)

// Direction measures which way the text on a page runs, from the
// arrangement of its components rather than by trying orientations and
// keeping whichever one reads (amendment step 14b).
//
// Two things are measured. The axis: for every component, the angle to its
// nearest neighbour, taken modulo a half turn, since a line of type is a
// row of components a letter's width apart and nothing else on a label is
// arranged that way in such numbers. And the sense of that axis: within a
// line, the bottoms of the components cluster on the baseline while their
// tops are spread between x-height, capitals and ascenders, so the tighter
// end is the bottom. Reading the tighter end as the top would be reading
// the page upside down.
//
// The return is the quarter turns to apply so that the text stands upright:
// 0, 1, 2 or 3, in the sense of imgops.Rotate90.
func Direction(comps []image.Rectangle, w, h int) int {
	q, _ := DirectionDebug(comps, w, h)
	return q
}

// DirectionDebug is Direction with the measurements it made.
func DirectionDebug(comps []image.Rectangle, w, h int) (int, string) {
	glyphs := plausible(comps)
	if len(glyphs) < 8 {
		return 0, "too few components"
	}
	axis := dominantAxis(glyphs)
	// Horizontal text is a row of neighbours at about zero degrees;
	// vertical text is a column at about ninety. A page rotated a quarter
	// turn clockwise is put upright by turning it back.
	var cand [2]int
	if axis < 45 || axis >= 135 {
		cand = [2]int{0, 2}
	} else {
		cand = [2]int{1, 3}
	}
	// The first candidate is the one that leaves the page as it is when
	// the text runs horizontally; turning it takes clear evidence, since
	// a page turned wrongly reads nothing at all.
	a := uprightScore(rotateBoxes(glyphs, cand[0], w, h))
	b := uprightScore(rotateBoxes(glyphs, cand[1], w, h))
	ab, am := uprightCues(rotateBoxes(glyphs, cand[0], w, h))
	bb, bm := uprightCues(rotateBoxes(glyphs, cand[1], w, h))
	note := fmt.Sprintf("axis %.0f deg, %d of %d components, %d: base %.2f marks %.2f | %d: base %.2f marks %.2f",
		axis, len(glyphs), len(comps), cand[0], ab, am, cand[1], bb, bm)
	// Turning a page that was the right way up reads nothing at all,
	// where leaving an upside-down one costs that page alone, so the
	// half turn is only taken on clear evidence. On the fifty real
	// labels neither cue is decisive: they are measured and reported
	// with step 14b rather than trusted.
	if b < a-0.5 {
		return cand[1], note
	}
	return cand[0], note
}

// plausible keeps the components that could be letters: a page carries
// rules, borders and pictures, and their arrangement says nothing about
// the text.
func plausible(comps []image.Rectangle) []image.Rectangle {
	if len(comps) == 0 {
		return nil
	}
	hs := make([]int, 0, len(comps))
	for _, c := range comps {
		hs = append(hs, c.Dy())
	}
	sort.Ints(hs)
	med := float64(hs[len(hs)/2])
	if med <= 0 {
		return nil
	}
	out := make([]image.Rectangle, 0, len(comps))
	for _, c := range comps {
		hh, ww := float64(c.Dy()), float64(c.Dx())
		if hh < 0.4*med || hh > 3*med || ww > 6*med {
			continue
		}
		out = append(out, c)
	}
	return out
}

// dominantAxis is the modal angle, in degrees from 0 to 180, between a
// component's centre and its nearest neighbour's, each vote weighted by
// the component's area: a paragraph of six-pixel type has many more
// components than a headline and would otherwise decide the page.
func dominantAxis(comps []image.Rectangle) float64 {
	const bins = 36 // five degrees each
	var hist [bins]float64
	for i, a := range comps {
		best, bd := -1, math.Inf(1)
		ax, ay := center(a)
		for j, b := range comps {
			if i == j {
				continue
			}
			// The next letter of a line is about as tall as this one;
			// a blob of artwork beside a word is not.
			r := float64(a.Dy()) / float64(max(1, b.Dy()))
			if r < 0.6 || r > 1.7 {
				continue
			}
			bx, by := center(b)
			d := (ax-bx)*(ax-bx) + (ay-by)*(ay-by)
			if d < bd {
				bd, best = d, j
			}
		}
		if best < 0 || bd > 9*float64(a.Dy()*a.Dy()) {
			continue
		}
		bx, by := center(comps[best])
		ang := math.Atan2(by-ay, bx-ax) * 180 / math.Pi
		for ang < 0 {
			ang += 180
		}
		for ang >= 180 {
			ang -= 180
		}
		w := float64(a.Dx() * a.Dy())
		hist[int(ang/5)%bins] += w
	}
	// The neighbour of a letter is the next letter along the line, so the
	// mode is the line's direction; a bin and its neighbours are counted
	// together, since a page is never perfectly square to the camera.
	bestBin, bestV := 0, -1.0
	for i := range hist {
		v := hist[(i-1+bins)%bins] + hist[i] + hist[(i+1)%bins]
		if v > bestV {
			bestBin, bestV = i, v
		}
	}
	return float64(bestBin)*5 + 2.5
}

// uprightScore is small when the page is the right way up. Two cues, and
// the page is only turned when they agree that it is upside down.
//
// The first is the baseline: within a line the bottoms of the components
// cluster where they sit and the tops are spread between x-height,
// capitals and ascenders, so the tighter end is the bottom. Text set
// entirely in capitals is as tight at one end as the other and the cue
// says nothing, which is why the second is needed.
//
// The second is punctuation: a period, a comma and a hyphen sit low in the
// line whatever case the text is in, so a line whose small marks sit high
// is a line being read upside down.
func uprightScore(comps []image.Rectangle) float64 {
	b, m := uprightCues(comps)
	return b + m
}

// uprightCues is uprightScore split into its two parts.
func uprightCues(comps []image.Rectangle) (float64, float64) {
	lines := rows(comps)
	var bottoms, tops float64
	var deepBelow, deepAll float64
	for _, ln := range lines {
		if len(ln) < 4 {
			continue
		}
		b := make([]float64, 0, len(ln))
		t := make([]float64, 0, len(ln))
		var hs []float64
		for _, c := range ln {
			b = append(b, float64(c.Max.Y))
			t = append(t, float64(c.Min.Y))
			hs = append(hs, float64(c.Dy()))
		}
		bottoms += spread(b)
		tops += spread(t)

		// Latin text has room above its baseline and little below it:
		// ascenders and capitals reach well over the x-height, while
		// descenders are shallow and rare. Read upside down, the deep
		// side is below. Counting small marks instead does not work,
		// since the dot of an i sits high in an upright line and there
		// are more i's in the statute than full stops.
		sort.Float64s(b)
		base := b[len(b)/2]
		var above, below float64
		for _, c := range ln {
			above = math.Max(above, base-float64(c.Min.Y))
			below = math.Max(below, float64(c.Max.Y)-base)
		}
		if above > 0 {
			deepAll++
			deepBelow += math.Min(2, below/above)
		}
	}
	ratio := 1.0
	if tops > 0 {
		ratio = bottoms / tops
	}
	deep := 1.0
	if deepAll >= 3 {
		deep = deepBelow / deepAll
	}
	return ratio, deep
}

// rows groups components into lines the way the region proposer does, so
// that panels of different sizes are not run together.
func rows(comps []image.Rectangle) [][]image.Rectangle {
	cs := make([]Component, 0, len(comps))
	for _, b := range comps {
		cs = append(cs, Component{Box: b, Area: b.Dx() * b.Dy()})
	}
	lines, _ := Lines(cs, Default())
	if len(lines) > 0 {
		out := make([][]image.Rectangle, 0, len(lines))
		for _, ln := range lines {
			row := make([]image.Rectangle, 0, len(ln.Comps))
			for _, c := range ln.Comps {
				row = append(row, c.Box)
			}
			out = append(out, row)
		}
		return out
	}
	return rowsByCentre(comps)
}

// rowsByCentre is the fallback when the proposer's grouping finds nothing.
func rowsByCentre(comps []image.Rectangle) [][]image.Rectangle {
	if len(comps) == 0 {
		return nil
	}
	hs := make([]int, 0, len(comps))
	for _, c := range comps {
		hs = append(hs, c.Dy())
	}
	sort.Ints(hs)
	tol := math.Max(2, 0.6*float64(hs[len(hs)/2]))
	sorted := append([]image.Rectangle(nil), comps...)
	sort.Slice(sorted, func(i, j int) bool {
		_, a := center(sorted[i])
		_, b := center(sorted[j])
		return a < b
	})
	var out [][]image.Rectangle
	var cur []image.Rectangle
	last := math.Inf(-1)
	for _, c := range sorted {
		_, cy := center(c)
		if len(cur) > 0 && cy-last > tol {
			out = append(out, cur)
			cur = nil
		}
		cur = append(cur, c)
		last = cy
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

// spread is the mean absolute deviation from the median.
func spread(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	med := s[len(s)/2]
	var sum float64
	for _, x := range v {
		sum += math.Abs(x - med)
	}
	return sum / float64(len(v))
}

// rotateBoxes turns the boxes as imgops.Rotate90 turns the image they came
// from, so that a direction can be scored without rotating any pixels.
func rotateBoxes(comps []image.Rectangle, quarters, w, h int) []image.Rectangle {
	quarters = ((quarters % 4) + 4) % 4
	if quarters == 0 {
		return comps
	}
	out := make([]image.Rectangle, len(comps))
	for i, c := range comps {
		switch quarters {
		case 1:
			out[i] = image.Rect(h-1-c.Max.Y, c.Min.X, h-1-c.Min.Y, c.Max.X)
		case 2:
			out[i] = image.Rect(w-1-c.Max.X, h-1-c.Max.Y, w-1-c.Min.X, h-1-c.Min.Y)
		case 3:
			out[i] = image.Rect(c.Min.Y, w-1-c.Max.X, c.Max.Y, w-1-c.Min.X)
		}
	}
	return out
}

func center(r image.Rectangle) (float64, float64) {
	return float64(r.Min.X+r.Max.X) / 2, float64(r.Min.Y+r.Max.Y) / 2
}
