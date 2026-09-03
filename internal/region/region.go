// Package region proposes line-level regions from a binary image: connected
// components, grouped into bands by vertical centre, split into lines at wide
// gaps, plus sub-regions at wider gaps inside a line.
package region

import (
	"image"
	"sort"

	"treasury/internal/bitmap"
)

// Component is one 8-connected blob of ink.
type Component struct {
	Box  image.Rectangle
	Area int
}

// Line is a run of components on one baseline.
type Line struct {
	Box      image.Rectangle
	Comps    []Component // sorted by Box.Min.X
	Baseline int         // mode of component bottoms (exclusive max y)
	XLine    int         // mode of component tops
	MedW     int         // median component width in this line
	MedH     int         // median component height in this line
	Band     int         // index of the band this line came from
}

// Kind says how a region was produced.
type Kind int

const (
	KindLine Kind = iota // a line after gap splitting
	KindBand             // a whole vertical band, when it differs from its lines
	KindSub              // a piece of a line split at a wide internal gap
)

func (k Kind) String() string {
	switch k {
	case KindLine:
		return "line"
	case KindBand:
		return "band"
	case KindSub:
		return "sub"
	}
	return "?"
}

// Region is a candidate box for decoding.
type Region struct {
	Box   image.Rectangle
	Kind  Kind
	Line  int     // index into the lines slice; -1 for bands
	Glare float64 // fraction of the box under the glare mask
}

// Params are the proposal knobs; Default matches the spec.
type Params struct {
	MinArea       int     // components smaller than this are noise
	MinHeight     int     // components shorter than this are noise
	MaxHeightFrac float64 // components taller than this fraction of the image are art or borders
	VCenterFrac   float64 // same band when vertical centres differ by less than this × median height
	HGapFrac      float64 // same line when the gap is less than this × global median width
	SubGapFrac    float64 // sub-region split at gaps above this × the line's median width
	Pad           int     // region padding in px
}

// Default returns the spec's parameters, except MinHeight: the spec's 6 px
// discards periods, colons, and the dots of i at body sizes, and the
// reference alignment needs them. MinArea still removes specks.
func Default() Params {
	return Params{MinArea: 8, MinHeight: 3, MaxHeightFrac: 0.4, VCenterFrac: 0.6, HGapFrac: 1.5, SubGapFrac: 2.5, Pad: 4}
}

// Propose runs components → filter → dot merging → lines → regions.
func Propose(b *bitmap.Bitmap, glare *bitmap.Bitmap, p Params) ([]Line, []Region) {
	cs := MergeDots(Filter(Components(b), b.H, p))
	lines, bands := Lines(cs, p)
	return lines, Regions(lines, bands, b.Bounds(), glare, p)
}

// MergeDots folds a small mark that sits directly above a clearly larger
// component into that component: the dots of i and j, accents. A colon's two
// dots are the same size, so they stay apart.
func MergeDots(cs []Component) []Component {
	if len(cs) < 2 {
		return cs
	}
	hs := make([]int, len(cs))
	for i, c := range cs {
		hs[i] = c.Box.Dy()
	}
	medH := median(hs)
	target := make([]int, len(cs))
	for i := range target {
		target[i] = -1
	}
	for i, c := range cs {
		if float64(c.Box.Dy()) >= 0.6*float64(medH) {
			continue
		}
		bestGap := int(^uint(0) >> 1)
		for j, d := range cs {
			if j == i || d.Box.Dy()*2 <= c.Box.Dy()*3 {
				continue
			}
			gap := d.Box.Min.Y - c.Box.Max.Y
			if gap < -1 || float64(gap) > 0.6*float64(d.Box.Dy()) {
				continue
			}
			overlap := min(c.Box.Max.X, d.Box.Max.X) - max(c.Box.Min.X, d.Box.Min.X)
			if overlap*2 < c.Box.Dx() {
				continue
			}
			if gap < bestGap {
				target[i], bestGap = j, gap
			}
		}
	}
	out := make([]Component, len(cs))
	copy(out, cs)
	removed := make([]bool, len(cs))
	for i, j := range target {
		if j < 0 {
			continue
		}
		for target[j] >= 0 { // follow chains, e.g. an accent above a dot
			j = target[j]
		}
		out[j].Box = out[j].Box.Union(out[i].Box)
		out[j].Area += out[i].Area
		removed[i] = true
	}
	kept := out[:0]
	for i, c := range out {
		if !removed[i] {
			kept = append(kept, c)
		}
	}
	return kept
}

// Components labels 8-connected ink with union-find and returns one entry per blob.
func Components(b *bitmap.Bitmap) []Component {
	w, h := b.W, b.H
	labels := make([]int32, w*h)
	parent := make([]int32, 1, 1024)
	find := func(x int32) int32 {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	union := func(a, c int32) {
		ra, rc := find(a), find(c)
		if ra == rc {
			return
		}
		if ra < rc {
			parent[rc] = ra
		} else {
			parent[ra] = rc
		}
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if b.Pix[y*w+x] == 0 {
				continue
			}
			var nb [4]int32
			n := 0
			if x > 0 && labels[y*w+x-1] != 0 {
				nb[n] = labels[y*w+x-1]
				n++
			}
			if y > 0 {
				if labels[(y-1)*w+x] != 0 {
					nb[n] = labels[(y-1)*w+x]
					n++
				}
				if x > 0 && labels[(y-1)*w+x-1] != 0 {
					nb[n] = labels[(y-1)*w+x-1]
					n++
				}
				if x+1 < w && labels[(y-1)*w+x+1] != 0 {
					nb[n] = labels[(y-1)*w+x+1]
					n++
				}
			}
			if n == 0 {
				l := int32(len(parent))
				parent = append(parent, l)
				labels[y*w+x] = l
				continue
			}
			m := nb[0]
			for i := 1; i < n; i++ {
				if nb[i] < m {
					m = nb[i]
				}
			}
			labels[y*w+x] = m
			for i := 0; i < n; i++ {
				union(nb[i], m)
			}
		}
	}
	idx := make([]int32, len(parent))
	for i := range idx {
		idx[i] = -1
	}
	var comps []Component
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			l := labels[y*w+x]
			if l == 0 {
				continue
			}
			r := find(l)
			i := idx[r]
			if i < 0 {
				i = int32(len(comps))
				idx[r] = i
				comps = append(comps, Component{Box: image.Rect(x, y, x+1, y+1)})
			}
			c := &comps[i]
			c.Area++
			c.Box = c.Box.Union(image.Rect(x, y, x+1, y+1))
		}
	}
	return comps
}

// Filter drops noise and oversized components.
func Filter(cs []Component, imgH int, p Params) []Component {
	out := cs[:0:0]
	maxH := int(p.MaxHeightFrac * float64(imgH))
	for _, c := range cs {
		h := c.Box.Dy()
		if c.Area < p.MinArea || h < p.MinHeight || h > maxH {
			continue
		}
		out = append(out, c)
	}
	return out
}

// Lines groups components into bands by vertical centre, then splits each
// band into lines at gaps wider than HGapFrac × the global median width.
// It returns the lines and the band boxes.
func Lines(cs []Component, p Params) ([]Line, []image.Rectangle) {
	if len(cs) == 0 {
		return nil, nil
	}
	ws := make([]int, len(cs))
	hs := make([]int, len(cs))
	for i, c := range cs {
		ws[i], hs[i] = c.Box.Dx(), c.Box.Dy()
	}
	medW, medH := median(ws), median(hs)
	vThr := p.VCenterFrac * float64(medH)
	hThr := p.HGapFrac * float64(medW)

	sorted := append([]Component(nil), cs...)
	sort.Slice(sorted, func(i, j int) bool { return cy(sorted[i]) < cy(sorted[j]) })
	type band struct {
		comps []Component
		sumCy float64
	}
	var bands []band
	for _, c := range sorted {
		y := cy(c)
		best, bestD := -1, vThr
		for i := range bands {
			d := y - bands[i].sumCy/float64(len(bands[i].comps))
			if d < 0 {
				d = -d
			}
			if d < bestD {
				best, bestD = i, d
			}
		}
		if best < 0 {
			bands = append(bands, band{comps: []Component{c}, sumCy: y})
			continue
		}
		bands[best].comps = append(bands[best].comps, c)
		bands[best].sumCy += y
	}

	var lines []Line
	var boxes []image.Rectangle
	for bi := range bands {
		comps := bands[bi].comps
		sort.Slice(comps, func(i, j int) bool { return comps[i].Box.Min.X < comps[j].Box.Min.X })
		bbox := comps[0].Box
		for _, c := range comps[1:] {
			bbox = bbox.Union(c.Box)
		}
		boxes = append(boxes, bbox)
		start, maxX := 0, comps[0].Box.Max.X
		for i := 1; i <= len(comps); i++ {
			if i < len(comps) && float64(comps[i].Box.Min.X-maxX) <= hThr {
				maxX = max(maxX, comps[i].Box.Max.X)
				continue
			}
			lines = append(lines, newLine(comps[start:i], bi))
			if i < len(comps) {
				start, maxX = i, comps[i].Box.Max.X
			}
		}
	}
	return lines, boxes
}

func newLine(comps []Component, band int) Line {
	box := comps[0].Box
	ws := make([]int, len(comps))
	hs := make([]int, len(comps))
	tops := make([]int, len(comps))
	bottoms := make([]int, len(comps))
	for i, c := range comps {
		box = box.Union(c.Box)
		ws[i], hs[i] = c.Box.Dx(), c.Box.Dy()
		tops[i], bottoms[i] = c.Box.Min.Y, c.Box.Max.Y
	}
	return Line{
		Box:      box,
		Comps:    append([]Component(nil), comps...),
		Baseline: mode(bottoms),
		XLine:    mode(tops),
		MedW:     median(ws),
		MedH:     median(hs),
		Band:     band,
	}
}

// Regions emits every line, every band that is not identical to one line,
// and the pieces of lines split at gaps wider than SubGapFrac × the line's
// median width. Boxes are padded and deduplicated.
func Regions(lines []Line, bands []image.Rectangle, bounds image.Rectangle, glare *bitmap.Bitmap, p Params) []Region {
	seen := map[image.Rectangle]bool{}
	var out []Region
	add := func(box image.Rectangle, kind Kind, line int) {
		box = box.Inset(-p.Pad).Intersect(bounds)
		if box.Empty() || seen[box] {
			return
		}
		seen[box] = true
		r := Region{Box: box, Kind: kind, Line: line}
		if glare != nil {
			r.Glare = float64(glare.CountIn(box)) / float64(box.Dx()*box.Dy())
		}
		out = append(out, r)
	}
	for i, ln := range lines {
		add(ln.Box, KindLine, i)
	}
	for _, b := range bands {
		add(b, KindBand, -1)
	}
	for i, ln := range lines {
		thr := p.SubGapFrac * float64(ln.MedW)
		start, maxX := 0, ln.Comps[0].Box.Max.X
		var parts []image.Rectangle
		for j := 1; j <= len(ln.Comps); j++ {
			if j < len(ln.Comps) && float64(ln.Comps[j].Box.Min.X-maxX) <= thr {
				maxX = max(maxX, ln.Comps[j].Box.Max.X)
				continue
			}
			box := ln.Comps[start].Box
			for _, c := range ln.Comps[start:j] {
				box = box.Union(c.Box)
			}
			parts = append(parts, box)
			if j < len(ln.Comps) {
				start, maxX = j, ln.Comps[j].Box.Max.X
			}
		}
		if len(parts) > 1 {
			for _, b := range parts {
				add(b, KindSub, i)
			}
		}
	}
	return out
}

func cy(c Component) float64 { return float64(c.Box.Min.Y+c.Box.Max.Y) / 2 }

func median(v []int) int {
	if len(v) == 0 {
		return 0
	}
	s := append([]int(nil), v...)
	sort.Ints(s)
	return s[len(s)/2]
}

// mode returns the value with the most support within ±1.
func mode(v []int) int {
	if len(v) == 0 {
		return 0
	}
	counts := map[int]int{}
	for _, x := range v {
		counts[x]++
	}
	best, bestN := v[0], -1
	for x := range counts {
		n := counts[x-1] + counts[x] + counts[x+1]
		if n > bestN || (n == bestN && x > best) {
			best, bestN = x, n
		}
	}
	return best
}
