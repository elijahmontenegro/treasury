// Package alphabet learns an image's glyph shapes by aligning a block of
// components to a reference string known to be printed there. Nothing in it
// knows what the string says.
package alphabet

import (
	"image"
	"image/draw"
	"math"
	"sort"

	"treasury/internal/bitcode"
	"treasury/internal/bitmap"
	"treasury/internal/encoder"
	"treasury/internal/region"
)

// Glyph is one component of the reference block with its frame code.
type Glyph struct {
	Box      image.Rectangle // preprocessed-image coordinates
	Row      int             // reading-order row within the block
	Baseline int             // the row's baseline
	Bin      *bitmap.Bitmap  // binary crop at Box
	Gray     *image.Gray     // grayscale crop at Box
	Code     bitcode.Code    // frame code, see Frame
	Derived  bool            // cut out of a merged pair rather than a component of its own
}

// Encoder is the glyph encoder the block's codes were made with.
func (b *Block) Encoder() encoder.Encoder { return b.enc }

// glyphAt builds a Glyph for a box inside the block.
func (b *Block) glyphAt(box image.Rectangle, row, baseline int, gray *image.Gray) Glyph {
	return Glyph{
		Box:      box,
		Row:      row,
		Baseline: baseline,
		Bin:      b.bin.Crop(box),
		Gray:     grayCrop(gray, box),
		Code:     b.enc.Encode(Frame(b.bin, box, baseline, b.XHeight)),
	}
}

// thinnestColumn returns the column of a bitmap with the least ink inside
// the window [centre−spread, centre+spread] of its width, or -1 if the
// window is degenerate. Touching letters join through a thin neck.
func thinnestColumn(bm *bitmap.Bitmap, centre, spread float64) int {
	lo := max(1, int(float64(bm.W)*(centre-spread)))
	hi := min(bm.W-1, int(float64(bm.W)*(centre+spread))+1)
	if lo >= hi {
		return -1
	}
	counts := make([]int, hi-lo)
	minN := bm.H + 1
	for x := lo; x < hi; x++ {
		n := 0
		for y := range bm.H {
			n += int(bm.Pix[y*bm.W+x])
		}
		counts[x-lo] = n
		minN = min(minN, n)
	}
	// Among columns within one pixel of the thinnest, take the one nearest
	// the expected split.
	want := centre * float64(bm.W)
	best := -1
	for x := lo; x < hi; x++ {
		if counts[x-lo] > minN+1 {
			continue
		}
		if best < 0 || math.Abs(float64(x)-want) < math.Abs(float64(best)-want) {
			best = x
		}
	}
	return best
}

// Row is one reading-order line of the block.
type Row struct {
	Box      image.Rectangle
	Baseline int
	XHeight  float64
	Glyphs   []int // indices into Block.Glyphs, left to right
}

// Block is a candidate reference block in reading order.
type Block struct {
	Box     image.Rectangle
	Rows    []Row
	Glyphs  []Glyph
	XHeight float64   // block x-height in px
	Gap     []float64 // Gap[g] is the horizontal gap before glyph g in x-heights; +Inf at a row start

	bin *bitmap.Bitmap
	enc encoder.Encoder
}

// Locate clusters lines into vertically adjacent groups of similar type size
// and returns up to top clusters, most glyphs first, each as line indices.
func Locate(lines []region.Line, top int) [][]int {
	if len(lines) == 0 {
		return nil
	}
	idx := make([]int, len(lines))
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(a, b int) bool {
		la, lb := lines[idx[a]], lines[idx[b]]
		if la.Baseline != lb.Baseline {
			return la.Baseline < lb.Baseline
		}
		return la.Box.Min.X < lb.Box.Min.X
	})
	// A line of one or two components, a speck or a stray mark between two
	// rows, neither continues a block nor breaks it; it is left out.
	var clusters [][]int
	var cur []int
	for _, i := range idx {
		if len(lines[i].Comps) < 3 {
			continue
		}
		if cur != nil && adjacent(lines[cur[len(cur)-1]], lines[i]) {
			cur = append(cur, i)
			continue
		}
		if cur != nil {
			clusters = append(clusters, cur)
		}
		cur = []int{i}
	}
	if cur != nil {
		clusters = append(clusters, cur)
	}
	count := func(cl []int) int {
		n := 0
		for _, i := range cl {
			n += len(lines[i].Comps)
		}
		return n
	}
	sort.SliceStable(clusters, func(a, b int) bool { return count(clusters[a]) > count(clusters[b]) })
	if len(clusters) > top {
		clusters = clusters[:top]
	}
	return clusters
}

// adjacent says whether b continues the block a belongs to: similar glyph
// size and a baseline within four and a half glyph heights. The median
// glyph height of running text is about the x-height, and leading runs to
// 1.5 times the size, close to three x-heights; three heights split a
// generously leaded block into single lines.
func adjacent(a, b region.Line) bool {
	ha, hb := float64(a.MedH), float64(b.MedH)
	if ha == 0 || hb == 0 {
		return false
	}
	if r := ha / hb; r < 0.6 || r > 1/0.6 {
		return false
	}
	return math.Abs(float64(b.Baseline-a.Baseline)) <= 4.5*math.Max(ha, hb)
}

// Extract builds a Block from a cluster of lines: rows by baseline, glyphs
// left to right within a row, frame codes from enc.
func Extract(cluster []int, lines []region.Line, bin *bitmap.Bitmap, gray *image.Gray, enc encoder.Encoder) *Block {
	return extract(cluster, lines, bin, gray, enc, 1)
}

// extract is Extract with the x-height estimate scaled: the estimate is the
// most common glyph height of a row, which is the x-height of lowercase
// text and the cap height of text set in capitals; a reference without
// lowercase is aligned with the estimate divided by the usual cap-to-x
// ratio, so its capitals measure tall against their priors.
func extract(cluster []int, lines []region.Line, bin *bitmap.Bitmap, gray *image.Gray, enc encoder.Encoder, xhScale float64) *Block {
	sorted := append([]int(nil), cluster...)
	sort.Slice(sorted, func(a, b int) bool {
		la, lb := lines[sorted[a]], lines[sorted[b]]
		if la.Baseline != lb.Baseline {
			return la.Baseline < lb.Baseline
		}
		return la.Box.Min.X < lb.Box.Min.X
	})
	var rowLines [][]int
	for _, i := range sorted {
		if n := len(rowLines); n > 0 {
			ref := lines[rowLines[n-1][0]]
			if abs(lines[i].Baseline-ref.Baseline) <= max(2, int(0.3*float64(ref.MedH))) {
				rowLines[n-1] = append(rowLines[n-1], i)
				continue
			}
		}
		rowLines = append(rowLines, []int{i})
	}

	type rowData struct {
		comps            []region.Component
		slope, intercept float64 // fitted baseline y = intercept + slope·x
		baseline         int     // at the row's centre, for reporting
		xh               float64
	}
	rows := make([]rowData, len(rowLines))
	type wxh struct {
		xh float64
		n  int
	}
	var xhs []wxh
	total := 0
	for ri, rl := range rowLines {
		var comps []region.Component
		for _, li := range rl {
			comps = append(comps, lines[li].Comps...)
		}
		sort.Slice(comps, func(a, b int) bool { return comps[a].Box.Min.X < comps[b].Box.Min.X })
		boxes := make([]image.Rectangle, len(comps))
		for i, c := range comps {
			boxes[i] = c.Box
		}
		slope, intercept := region.FitBaseline(boxes)
		cx := (comps[0].Box.Min.X + comps[len(comps)-1].Box.Max.X) / 2
		xh := rowXHeightFit(comps, slope, intercept)
		rows[ri] = rowData{comps: comps, slope: slope, intercept: intercept, baseline: region.BaselineAt(slope, intercept, cx), xh: xh}
		xhs = append(xhs, wxh{xh, len(comps)})
		total += len(comps)
	}
	sort.Slice(xhs, func(a, b int) bool { return xhs[a].xh < xhs[b].xh })
	blockXH := xhs[len(xhs)-1].xh
	for acc, w := 0, 0; w < len(xhs); w++ {
		acc += xhs[w].n
		if 2*acc >= total {
			blockXH = xhs[w].xh
			break
		}
	}
	blockXH *= xhScale
	if blockXH < 3 {
		blockXH = 3
	}

	b := &Block{XHeight: blockXH, bin: bin, enc: enc}
	for ri, rd := range rows {
		row := Row{Baseline: rd.baseline, XHeight: rd.xh * xhScale}
		prevMax := 0
		for k, c := range rd.comps {
			gi := len(b.Glyphs)
			baseline := region.BaselineAt(rd.slope, rd.intercept, (c.Box.Min.X+c.Box.Max.X)/2)
			g := Glyph{
				Box:      c.Box,
				Row:      ri,
				Baseline: baseline,
				Bin:      bin.Crop(c.Box),
				Gray:     grayCrop(gray, c.Box),
				Code:     enc.Encode(Frame(bin, c.Box, baseline, blockXH)),
			}
			b.Glyphs = append(b.Glyphs, g)
			row.Glyphs = append(row.Glyphs, gi)
			if k == 0 {
				b.Gap = append(b.Gap, math.Inf(1))
				row.Box = c.Box
			} else {
				b.Gap = append(b.Gap, float64(c.Box.Min.X-prevMax)/blockXH)
				row.Box = row.Box.Union(c.Box)
			}
			prevMax = max(prevMax, c.Box.Max.X)
		}
		if ri == 0 {
			b.Box = row.Box
		} else {
			b.Box = b.Box.Union(row.Box)
		}
		b.Rows = append(b.Rows, row)
	}
	return b
}

// rowXHeightFit is the most common height among components that sit on the
// fitted baseline; x-height letters outnumber ascenders and capitals in
// running text.
func rowXHeightFit(comps []region.Component, slope, intercept float64) float64 {
	var hs []int
	for _, c := range comps {
		base := region.BaselineAt(slope, intercept, (c.Box.Min.X+c.Box.Max.X)/2)
		if abs(c.Box.Max.Y-base) <= 2 && c.Box.Dy() >= 3 {
			hs = append(hs, c.Box.Dy())
		}
	}
	if len(hs) == 0 {
		for _, c := range comps {
			hs = append(hs, c.Box.Dy())
		}
		sort.Ints(hs)
		return float64(hs[len(hs)/2])
	}
	return float64(mode(hs))
}

// Frame places a glyph inside a square frame of side 2.2 x-heights whose top
// is 1.6 x-heights above the baseline and whose centre column is the glyph's
// centre, so size and vertical position become part of the code.
func Frame(bin *bitmap.Bitmap, box image.Rectangle, baseline int, xh float64) encoder.Patch {
	side := max(8, int(math.Ceil(2.2*xh)))
	top := baseline - int(math.Round(1.6*xh))
	left := (box.Min.X+box.Max.X)/2 - side/2
	canvas := bitmap.New(side, side)
	crop := bin.Crop(box)
	origin := box.Intersect(bin.Bounds()).Min
	for y := range crop.H {
		for x := range crop.W {
			if crop.Pix[y*crop.W+x] != 0 {
				canvas.Set(origin.X+x-left, origin.Y+y-top, 1)
			}
		}
	}
	return encoder.Patch{Bin: canvas}
}

func grayCrop(g *image.Gray, r image.Rectangle) *image.Gray {
	r = r.Intersect(g.Rect)
	out := image.NewGray(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(out, out.Rect, g, r.Min, draw.Src)
	return out
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
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
