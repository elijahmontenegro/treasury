package preprocess

import (
	"image"
	"math"
	"sort"

	"treasury/internal/bitmap"
)

// Text separation on artwork.
//
// A label is not text on paper. It is artwork, borders, patterns, gradients
// and photographs, with text composited over it, and a label whose text is
// not legible has failed its own purpose. So the text is separable, but not
// by luminance: a dark border is darker than the type beside it, a
// background pattern is the same grey as the words over it, and white type
// inside a dark band is not ink at all under a threshold that calls dark
// pixels ink.
//
// What makes a line of type legible to a person is three things, and a
// border, a pattern and a photograph fail all three. A letter has one
// stroke width, and that width is small against the letter's size. A letter
// stands out from what is immediately behind it, not from the average of
// the whole label. And a letter has company: letters of its own size on a
// common baseline. Separation here measures those three and keeps what
// passes, from both polarities at once, so a label with a dark band and a
// light panel gives up the text in both.

// Piece is one candidate component and what was measured about it.
type Piece struct {
	Box      image.Rectangle
	Area     int
	Dark     bool    // dark ink on a lighter ground; false is the reverse
	Stroke   float64 // twice the median distance to the edge: the stroke width
	Ratio    float64 // stroke against the longer side of the box
	Spread   float64 // the stroke's own variation, its standard deviation over its mean
	Contrast float64 // the ink's grey against the ring just outside it, over 255
	Ground   float64 // the grey of the ring in the image as taken, which says which polarity can be right
	Kept     bool
	Reason   string // why it was rejected, empty when kept
	pix      []int32
}

// SepParams are the separation's measurements and their bounds. They are
// stated here rather than tuned into hiding; step 10c retunes them on a
// corpus rebuilt from the population.
type SepParams struct {
	MinHeight   int     // a mark shorter than this cannot carry a letter
	MaxHeightFr float64 // taller than this fraction of the image is not a letter
	MaxWidthFr  float64 // wider than this fraction is a rule or a border
	MinArea     int
	MaxRatio    float64 // stroke over the longer side: a letter is thin along its length, a blob is not
	MaxSpread   float64 // the stroke's variation over its mean
	MinContrast float64 // against the immediate surround, not the image
	DarkGround  float64 // dark ink must sit on a ground at least this bright
	LightGround float64 // light ink must sit on a ground no brighter than this
	SolidHeight float64 // a solid piece is not text when it is this many times the median height
	BarField    int     // this many bars sharing a top and a bottom are a barcode
	MinRun      int     // a letter has company: this many on a common baseline
	RunHeightFr float64 // members of a run agree in height to this fraction
}

// DefaultSep returns the separation's bounds.
func DefaultSep() SepParams {
	return SepParams{
		MinHeight: 2, MaxHeightFr: 0.25, MaxWidthFr: 0.6, MinArea: 4,
		MaxRatio: 0.55, MaxSpread: 0.62, MinContrast: 0.10, SolidHeight: 1.6, DarkGround: 90, LightGround: 165, BarField: 8,
		MinRun: 3, RunHeightFr: 0.45,
	}
}

// Text is what separation produced.
type Text struct {
	Mask   *bitmap.Bitmap // the kept text of both polarities, ink = 1
	Gray   *image.Gray    // grey with light-on-dark text inverted in place, so every kept glyph is dark on light
	Pieces []Piece
}

// SeparateText finds the text in g.
func SeparateText(g *image.Gray, p Params, sp SepParams) *Text {
	w, h := g.Rect.Dx(), g.Rect.Dy()
	inv := image.NewGray(g.Rect)
	for i, v := range g.Pix {
		inv.Pix[i] = 255 - v
	}
	var pieces []Piece
	pieces = append(pieces, candidates(g, g, Threshold(g, p), true, sp)...)
	pieces = append(pieces, candidates(inv, g, Threshold(inv, p), false, sp)...)

	// The hole inside a letter is not a letter. The counter of an O is
	// light where the letter is dark, so the other polarity's pass finds
	// it, and taking it for text inverts the middle of the glyph.
	counters(pieces)

	// A barcode passes every test a letter passes: its bars are one stroke
	// wide, they stand against their ground, and they keep company in a
	// row. It is told apart by what letters never do, which is share a top
	// and a bottom exactly, a dozen times over.
	barcodes(pieces, sp)

	// A letter has company. What passed the per-piece measurements is kept
	// only where it sits among others of its own height on a common
	// baseline: that is what a line of type has and a decorative mark,
	// however letter-like, does not.
	local := coherent(pieces, sp)

	// Solidity is judged against the type the piece sits with, not against
	// the label. At ten pixels of type the counter of an O closes and the
	// letter is a solid blob, so an absolute test throws away every O, D,
	// B and 0; what is not text is the solid thing several times the
	// height of its own line.
	solid(pieces, local, sp)

	mask := bitmap.New(w, h)
	gray := image.NewGray(g.Rect)
	copy(gray.Pix, g.Pix)
	for _, pc := range pieces {
		if !pc.Kept {
			continue
		}
		for _, off := range pc.pix {
			mask.Pix[off] = 1
		}
		if !pc.Dark {
			// The glyph is light on dark. Downstream reads grey crops for
			// digits and for weight, and both expect dark on light, so the
			// glyph's own box is inverted in place.
			for y := pc.Box.Min.Y; y < pc.Box.Max.Y; y++ {
				for x := pc.Box.Min.X; x < pc.Box.Max.X; x++ {
					gray.Pix[y*w+x] = 255 - g.Pix[y*w+x]
				}
			}
		}
	}
	return &Text{Mask: mask, Gray: gray, Pieces: pieces}
}

// candidates labels one polarity's ink and measures every component.
func candidates(src, orig *image.Gray, bin *bitmap.Bitmap, dark bool, sp SepParams) []Piece {
	w, h := bin.W, bin.H
	boxes, areas, pix := label(bin)
	out := make([]Piece, 0, len(boxes))
	for i := range boxes {
		pc := Piece{Box: boxes[i], Area: areas[i], Dark: dark, pix: pix[i]}
		bw, bh := pc.Box.Dx(), pc.Box.Dy()
		switch {
		case bh < sp.MinHeight || pc.Area < sp.MinArea:
			pc.Reason = "too small"
		case float64(bh) > sp.MaxHeightFr*float64(h):
			pc.Reason = "taller than a letter"
		case float64(bw) > sp.MaxWidthFr*float64(w):
			pc.Reason = "a rule or a border"
		}
		if pc.Reason == "" {
			pc.Stroke, pc.Spread = stroke(bin, pc.Box)
			// Against the longer side. A stem is exactly one stroke wide,
			// so measuring against the shorter side would call every
			// narrow letter a solid blob.
			pc.Ratio = pc.Stroke / float64(max(bw, bh))
			pc.Contrast = contrast(src, pc)
			pc.Ground = ringGrey(orig, pc)
			switch {
			case pc.Spread > sp.MaxSpread:
				pc.Reason = "stroke is not one width"
			case pc.Contrast < sp.MinContrast:
				pc.Reason = "no contrast with its surround"
			case dark && pc.Ground < sp.DarkGround:
				pc.Reason = "dark ink on a dark ground"
			case !dark && pc.Ground > sp.LightGround:
				pc.Reason = "light ink on a light ground"
			default:
				pc.Kept = true
			}
		}
		out = append(out, pc)
	}
	return out
}

// stroke measures a component's stroke width and how much it varies: twice
// the median distance from ink to the nearest edge, and the standard
// deviation of that over its mean. A letter is one width; a gradient blob
// and a photograph's shadow are not.
func stroke(bin *bitmap.Bitmap, box image.Rectangle) (width, spread float64) {
	crop := bin.Crop(box)
	d := crop.DistanceInside()
	vals := make([]float64, 0, len(d))
	for _, v := range d {
		if v > 0 {
			vals = append(vals, 2*float64(v))
		}
	}
	if len(vals) == 0 {
		return 0, 0
	}
	sort.Float64s(vals)
	width = vals[len(vals)/2]
	mean := 0.0
	for _, v := range vals {
		mean += v
	}
	mean /= float64(len(vals))
	if mean == 0 {
		return width, 0
	}
	varsum := 0.0
	for _, v := range vals {
		varsum += (v - mean) * (v - mean)
	}
	return width, math.Sqrt(varsum/float64(len(vals))) / mean
}

// contrast is the ink's mean grey against the ring just outside the
// component, in the polarity's own image, where ink is dark. Against the
// immediate surround, not the image: a word over a photograph has to be
// legible against the photograph under it.
func contrast(src *image.Gray, pc Piece) float64 {
	w := src.Rect.Dx()
	ink := 0.0
	for _, off := range pc.pix {
		ink += float64(src.Pix[off])
	}
	ink /= float64(len(pc.pix))
	in := map[int32]bool{}
	for _, off := range pc.pix {
		in[off] = true
	}
	pad := max(2, int(math.Round(pc.Stroke)))
	ring := pc.Box.Inset(-pad).Intersect(src.Rect)
	sum, n := 0.0, 0
	for y := ring.Min.Y; y < ring.Max.Y; y++ {
		for x := ring.Min.X; x < ring.Max.X; x++ {
			if in[int32(y*w+x)] {
				continue // the ink itself is not its own surround
			}
			sum += float64(src.Pix[y*w+x])
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return math.Abs(sum/float64(n)-ink) / 255
}

// counters rejects a piece that sits inside a larger piece of the other
// polarity: the hole of an O, the gap in a B, the inside of a ring.
func counters(pieces []Piece) {
	var idx []int
	for i := range pieces {
		if pieces[i].Kept {
			idx = append(idx, i)
		}
	}
	for _, a := range idx {
		if !pieces[a].Kept {
			continue
		}
		for _, b := range idx {
			if a == b || pieces[a].Dark == pieces[b].Dark || !pieces[b].Kept {
				continue
			}
			if pieces[b].Area <= pieces[a].Area {
				continue
			}
			// Only a letter-sized enclosure. A counter is a large part of
			// its letter; a panel that happens to contain a word is not a
			// letter, and its words are not its holes.
			if pieces[b].Area > 6*pieces[a].Area || pieces[b].Box.Dy() > 4*pieces[a].Box.Dy() {
				continue
			}
			if pieces[a].Box.In(pieces[b].Box) {
				pieces[a].Kept = false
				pieces[a].Reason = "the hole inside a letter"
				break
			}
		}
	}
}

// barcodes rejects fields of bars: eight or more tall, narrow pieces in a
// row whose tops and bottoms agree. No text is set that way, and a
// barcode's bars are as many components as a paragraph.
func barcodes(pieces []Piece, sp SepParams) {
	type bar struct {
		i        int
		top, bot float64
		x0, x1   float64
	}
	var bars []bar
	for i := range pieces {
		if !pieces[i].Kept {
			continue
		}
		b := pieces[i].Box
		if b.Dy() < 5*max(1, b.Dx()) {
			continue
		}
		bars = append(bars, bar{i, float64(b.Min.Y), float64(b.Max.Y), float64(b.Min.X), float64(b.Max.X)})
	}
	sort.Slice(bars, func(a, b int) bool { return bars[a].x0 < bars[b].x0 })
	used := make([]bool, len(bars))
	for a := range bars {
		if used[a] {
			continue
		}
		h := bars[a].bot - bars[a].top
		field := []int{a}
		for b := a + 1; b < len(bars); b++ {
			if used[b] {
				continue
			}
			if bars[b].x0-bars[a].x1 > 6*h {
				break
			}
			if math.Abs(bars[b].top-bars[a].top) > 0.15*h || math.Abs(bars[b].bot-bars[a].bot) > 0.15*h {
				continue
			}
			field = append(field, b)
		}
		if len(field) < sp.BarField {
			continue
		}
		for _, b := range field {
			used[b] = true
			pieces[bars[b].i].Kept = false
			pieces[bars[b].i].Reason = "a bar of a barcode"
		}
	}
}

// solid rejects the pieces that are both thick for their size and large
// for the label's type: a border corner, a logo, a photograph's shadow. A
// small filled bowl at the text's own scale is a letter.
func solid(pieces []Piece, local map[int]float64, sp SepParams) {
	for i := range pieces {
		if !pieces[i].Kept {
			continue
		}
		med, ok := local[i]
		if !ok || med <= 0 {
			continue
		}
		if pieces[i].Ratio > sp.MaxRatio && float64(pieces[i].Box.Dy()) > sp.SolidHeight*med {
			pieces[i].Kept = false
			pieces[i].Reason = "solid, not a stroke"
		}
	}
}

// coherent keeps only the pieces that sit in a line of type: at least
// MinRun components of similar height whose vertical centres agree. A mark
// too small to join a run on its own, a full stop or the dot of an i, is
// kept when it falls inside a run's own band, which is how it is read.
func coherent(pieces []Piece, sp SepParams) map[int]float64 {
	type item struct {
		i      int
		cy, h  float64
		x0, x1 float64
	}
	local := map[int]float64{}
	var items []item
	var heights []float64
	for i := range pieces {
		if !pieces[i].Kept {
			continue
		}
		b := pieces[i].Box
		items = append(items, item{i, float64(b.Min.Y+b.Max.Y) / 2, float64(b.Dy()), float64(b.Min.X), float64(b.Max.X)})
		heights = append(heights, float64(b.Dy()))
	}
	if len(items) == 0 {
		return local
	}
	sort.Float64s(heights)
	med := heights[len(heights)/2]
	var big []item
	for _, it := range items {
		if it.h >= 0.5*med {
			big = append(big, it)
		}
	}
	sort.Slice(big, func(a, b int) bool { return big[a].cy < big[b].cy })
	type band struct{ top, bottom, x0, x1 float64 }
	type run struct {
		bd  band
		med float64
	}
	var runs []run
	inRun := map[int]bool{}
	used := make([]bool, len(big))
	for a := range big {
		if used[a] {
			continue
		}
		members := []int{a}
		for b := a + 1; b < len(big); b++ {
			if used[b] {
				continue
			}
			hi := math.Max(big[a].h, big[b].h)
			if big[b].cy-big[a].cy > 2*hi {
				break
			}
			if math.Abs(big[b].cy-big[a].cy) > 0.6*hi || math.Abs(big[b].h-big[a].h) > sp.RunHeightFr*hi {
				continue
			}
			members = append(members, b)
		}
		if len(members) < sp.MinRun {
			continue
		}
		bd := band{top: big[a].cy - big[a].h, bottom: big[a].cy + big[a].h, x0: big[a].x0, x1: big[a].x1}
		var hs []float64
		for _, b := range members {
			used[b] = true
			inRun[big[b].i] = true
			bd.top = math.Min(bd.top, big[b].cy-big[b].h)
			bd.bottom = math.Max(bd.bottom, big[b].cy+big[b].h)
			bd.x0 = math.Min(bd.x0, big[b].x0)
			bd.x1 = math.Max(bd.x1, big[b].x1)
			hs = append(hs, big[b].h)
		}
		sort.Float64s(hs)
		m := hs[len(hs)/2]
		for _, b := range members {
			local[big[b].i] = m
		}
		runs = append(runs, run{bd, m})
	}
	for _, it := range items {
		if inRun[it.i] {
			continue
		}
		for _, rn := range runs {
			if it.cy >= rn.bd.top && it.cy <= rn.bd.bottom && it.x1 >= rn.bd.x0-2*med && it.x0 <= rn.bd.x1+2*med {
				inRun[it.i] = true
				local[it.i] = rn.med
				break
			}
		}
	}
	for i := range pieces {
		if pieces[i].Kept && !inRun[i] {
			pieces[i].Kept = false
			pieces[i].Reason = "no line of type around it"
		}
	}
	return local
}

// label finds the connected components of b and returns, per component, its
// box, its area, and the offsets of its pixels.
func label(b *bitmap.Bitmap) (boxes []image.Rectangle, areas []int, pix [][]int32) {
	w, h := b.W, b.H
	labels := make([]int32, w*h)
	parent := []int32{0}
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
	for y := range h {
		for x := range w {
			if b.Pix[y*w+x] == 0 {
				continue
			}
			var nb [4]int32
			n := 0
			add := func(l int32) {
				if l != 0 {
					nb[n] = l
					n++
				}
			}
			if x > 0 {
				add(labels[y*w+x-1])
			}
			if y > 0 {
				add(labels[(y-1)*w+x])
				if x > 0 {
					add(labels[(y-1)*w+x-1])
				}
				if x+1 < w {
					add(labels[(y-1)*w+x+1])
				}
			}
			if n == 0 {
				parent = append(parent, int32(len(parent)))
				labels[y*w+x] = int32(len(parent) - 1)
				continue
			}
			labels[y*w+x] = nb[0]
			for i := 1; i < n; i++ {
				union(nb[0], nb[i])
			}
		}
	}
	index := make(map[int32]int, len(parent))
	for y := range h {
		for x := range w {
			l := labels[y*w+x]
			if l == 0 {
				continue
			}
			r := find(l)
			k, ok := index[r]
			if !ok {
				k = len(boxes)
				index[r] = k
				boxes = append(boxes, image.Rect(x, y, x+1, y+1))
				areas = append(areas, 0)
				pix = append(pix, nil)
			}
			boxes[k] = boxes[k].Union(image.Rect(x, y, x+1, y+1))
			areas[k]++
			pix[k] = append(pix[k], int32(y*w+x))
		}
	}
	return boxes, areas, pix
}

// ringGrey is the mean grey of the ring just outside a piece, in the image
// as taken. White type sits on a dark ground and dark type on a light one;
// a piece whose surround contradicts its polarity is the background of the
// other pass, not a letter.
func ringGrey(g *image.Gray, pc Piece) float64 {
	w := g.Rect.Dx()
	in := make(map[int32]bool, len(pc.pix))
	for _, off := range pc.pix {
		in[off] = true
	}
	pad := max(2, int(math.Round(pc.Stroke)))
	ring := pc.Box.Inset(-pad).Intersect(g.Rect)
	sum, n := 0.0, 0
	for y := ring.Min.Y; y < ring.Max.Y; y++ {
		for x := ring.Min.X; x < ring.Max.X; x++ {
			if in[int32(y*w+x)] {
				continue
			}
			sum += float64(g.Pix[y*w+x])
			n++
		}
	}
	if n == 0 {
		return 128
	}
	return sum / float64(n)
}
