package alphabet

import (
	"errors"
	"image"
	"math"
	"sort"
	"unicode"

	"treasury/internal/bitcode"
	"treasury/internal/bitmap"
	"treasury/internal/encoder"
	"treasury/internal/region"
)

// Span is a half-open rune range of the reference expected in a heavier weight.
type Span struct{ Start, End int }

// Options control learning.
type Options struct {
	BandFrac  float64 // DP band as a fraction of the glyph count
	MinBand   int
	MaxPasses int
	Penalties Penalties
	Encoder   encoder.Encoder // glyph encoder
	MinGlyphs float64 // a block needs at least this fraction of the reference's non-space characters
}

// DefaultOptions are the starting values.
func DefaultOptions() Options {
	return Options{BandFrac: 0.05, MinBand: 6, MaxPasses: 3, Penalties: DefaultPenalties(), Encoder: encoder.Glyph(), MinGlyphs: 0.5}
}

// Key identifies a sample pool: a character in the body (Span -1) or inside
// an emphasis span.
type Key struct {
	R    rune
	Span int
}

// RowQuality is the alignment quality of one row of the block.
type RowQuality struct {
	Row          int
	Glyphs       int
	Matched      int
	Penalized    int     // merge, split, insert, delete steps on the row
	Unexplained  int     // insert and delete steps only: what the reference cannot account for
	MeanDistance float64 // mean normalized Hamming of matched glyphs to their centroids
}

// Alphabet is what the block taught: glyph samples per character.
type Alphabet struct {
	Block    *Block
	Path     Path
	Assign   [][]int // per glyph, the character indices it stands for; empty for inserts
	Samples  map[rune][]Glyph
	Emphasis map[int]map[rune][]Glyph
	Centroid map[Key]bitcode.Code
	Bits     int

	XHeight   float64
	CapHeight float64
	LetterGap float64 // px
	WordGap   float64 // px

	Spread      float64 // mean within-character normalized Hamming spread
	Matched     int
	Penalized   int // merge, split, insert, delete
	Unexplained int // insert, delete
	Recovered   int // samples cut out of merged pairs
	Rows        []RowQuality
	Cost      float64
	Band      int
	Passes    int
}

// ErrNoBlock is returned when no candidate block aligns.
var ErrNoBlock = errors.New("alphabet: no block aligns to the reference")

// Find locates candidate blocks among the lines, learns from each, and keeps
// the one with the lowest alignment cost.
func Find(lines []region.Line, bin *bitmap.Bitmap, gray *image.Gray, ref string, spans []Span, opt Options) (*Alphabet, error) {
	need := 0
	for _, r := range ref {
		if r != ' ' {
			need++
		}
	}
	var best *Alphabet
	for _, cl := range Locate(lines, 3) {
		blk := Extract(cl, lines, bin, gray, opt.Encoder)
		if float64(len(blk.Glyphs)) < opt.MinGlyphs*float64(need) {
			continue
		}
		a, err := learn(blk, gray, ref, spans, opt)
		if err != nil {
			continue
		}
		if best == nil || a.Cost < best.Cost {
			best = a
		}
	}
	if best == nil {
		return nil, ErrNoBlock
	}
	return best, nil
}

// Learn aligns the block to ref and collects samples per character. Gray is
// the grayscale the block was extracted from; it is needed to cut samples
// out of merged pairs.
func Learn(b *Block, ref string, spans []Span, opt Options) (*Alphabet, error) {
	return learn(b, nil, ref, spans, opt)
}

func learn(b *Block, gray *image.Gray, ref string, spans []Span, opt Options) (*Alphabet, error) {
	chars := []rune(ref)
	n, m := len(chars), len(b.Glyphs)
	if m == 0 || n == 0 {
		return nil, errors.New("alphabet: empty block or reference")
	}
	spaces := make([]int, n+1)
	for c, r := range chars {
		spaces[c+1] = spaces[c]
		if r == ' ' {
			spaces[c+1]++
		}
	}
	spanOf := func(c int) int {
		for i, s := range spans {
			if c >= s.Start && c < s.End {
				return i
			}
		}
		return -1
	}
	priors := make([]prior, n)
	for c, r := range chars {
		priors[c] = charPrior(r)
	}
	feats := make([]feat, m)
	widths := make([]float64, m)
	for g, gl := range b.Glyphs {
		feats[g], widths[g] = measured(gl.Box, gl.Baseline, b.XHeight)
	}
	bits := opt.Encoder.Bits()
	unionCode := map[int]bitcode.Code{}
	unionBox := func(g, k int) (image.Rectangle, bool) {
		box := b.Glyphs[g].Box
		for i := g + 1; i < g+k; i++ {
			if b.Glyphs[i].Row != b.Glyphs[g].Row || b.Gap[i] > 0.3 {
				return image.Rectangle{}, false
			}
			box = box.Union(b.Glyphs[i].Box)
		}
		return box, true
	}

	var centroids map[Key]bitcode.Code
	pr := &problem{chars: chars, m: m, gap: b.Gap, spaces: spaces, pen: opt.Penalties}
	pr.shape = func(g, c int) float64 {
		pc := priorCost(feats[g], widths[g], priors[c])
		if centroids == nil {
			return pc
		}
		if cen, ok := centroids[Key{chars[c], spanOf(c)}]; ok {
			return 0.5*pc + 4*encoder.NormalizedDistance(b.Glyphs[g].Code, cen, bits)
		}
		return 0.5*pc + 0.6
	}
	pr.pair = func(g, c int) float64 {
		return priorCost(feats[g], widths[g], mergedPrior(priors[c], priors[c+1]))
	}
	unionCost := func(g, k, c int) float64 {
		box, ok := unionBox(g, k)
		if !ok {
			return math.NaN()
		}
		f, w := measured(box, b.Glyphs[g].Baseline, b.XHeight)
		pc := priorCost(f, w, priors[c])
		if centroids == nil {
			return pc
		}
		cen, ok := centroids[Key{chars[c], spanOf(c)}]
		if !ok {
			return 0.5*pc + 0.6
		}
		key := g*4 + k
		code, ok := unionCode[key]
		if !ok {
			code = b.enc.Encode(Frame(b.bin, box, b.Glyphs[g].Baseline, b.XHeight))
			unionCode[key] = code
		}
		return 0.5*pc + 4*encoder.NormalizedDistance(code, cen, bits)
	}
	pr.union = func(g, c int) float64 { return unionCost(g, 2, c) }
	pr.union3 = func(g, c int) float64 { return unionCost(g, 3, c) }

	band := max(opt.MinBand, int(opt.BandFrac*float64(m)))
	var path, prev Path
	var cost float64
	passes := 0
	for pass := 0; pass < max(1, opt.MaxPasses); pass++ {
		passes++
		for {
			var touched bool
			path, cost, touched = pr.align(band)
			if !touched || band >= m {
				break
			}
			band *= 2
		}
		if path == nil {
			return nil, ErrNoBlock
		}
		if prev != nil && samePath(prev, path) {
			break
		}
		prev = path
		centroids = centroidsOf(b, chars, spanOf, path, bits)
	}
	if centroids == nil {
		centroids = centroidsOf(b, chars, spanOf, path, bits)
	}
	a := &Alphabet{
		Block: b, Path: path, Centroid: centroids, Bits: bits,
		Samples: map[rune][]Glyph{}, Emphasis: map[int]map[rune][]Glyph{},
		XHeight: b.XHeight, Cost: cost, Band: band, Passes: passes,
	}
	a.Assign = make([][]int, m)
	rowQ := make([]RowQuality, len(b.Rows))
	for i := range rowQ {
		rowQ[i].Row = i
		rowQ[i].Glyphs = len(b.Rows[i].Glyphs)
	}
	var capHeights, letterGaps, wordGaps []float64
	spreadSum, spreadN := 0.0, 0
	distSum := map[Key][2]float64{}
	for i, st := range path {
		switch st.Kind {
		case Match:
			gl := b.Glyphs[st.Glyph]
			r := chars[st.Char]
			a.Assign[st.Glyph] = []int{st.Char}
			key := Key{r, spanOf(st.Char)}
			if key.Span < 0 {
				a.Samples[r] = append(a.Samples[r], gl)
			} else {
				if a.Emphasis[key.Span] == nil {
					a.Emphasis[key.Span] = map[rune][]Glyph{}
				}
				a.Emphasis[key.Span][r] = append(a.Emphasis[key.Span][r], gl)
			}
			if unicode.IsUpper(r) {
				capHeights = append(capHeights, float64(gl.Box.Dy()))
			}
			d := encoder.NormalizedDistance(gl.Code, centroids[key], bits)
			rq := &rowQ[gl.Row]
			rq.Matched++
			rq.MeanDistance += d
			a.Matched++
			s := distSum[key]
			distSum[key] = [2]float64{s[0] + d, s[1] + 1}
			if i > 0 && path[i-1].Kind == Match && path[i-1].Glyph == st.Glyph-1 && !math.IsInf(b.Gap[st.Glyph], 1) {
				letterGaps = append(letterGaps, b.Gap[st.Glyph]*b.XHeight)
			}
		case Merge:
			a.Assign[st.Glyph] = []int{st.Char, st.Char + 1}
			rowQ[b.Glyphs[st.Glyph].Row].Penalized++
			a.Penalized++
			// Recover both letters: cut at the thinnest column near where
			// the width priors say the first letter ends.
			gl := b.Glyphs[st.Glyph]
			wa, wb := priors[st.Char].width, priors[st.Char+1].width
			if wa <= 0 {
				wa = 1
			}
			if wb <= 0 {
				wb = 1
			}
			if cut := thinnestColumn(gl.Bin, wa/(wa+wb), 0.2); cut > 0 && gray != nil {
				left := image.Rect(gl.Box.Min.X, gl.Box.Min.Y, gl.Box.Min.X+cut, gl.Box.Max.Y)
				right := image.Rect(gl.Box.Min.X+cut, gl.Box.Min.Y, gl.Box.Max.X, gl.Box.Max.Y)
				// Both halves must look like their characters: features
				// consistent with the prior (a factor-of-two width miss
				// fails) and, when clean samples exist, within a loose
				// radius of their centroid. A wrong cut leaves one half
				// plausible and the other not; accept neither.
				var subs [2]Glyph
				ok := true
				for k, box := range []image.Rectangle{left, right} {
					sub := b.glyphAt(box, gl.Row, gl.Baseline, gray)
					ib, has := sub.Bin.InkBounds()
					if !has {
						ok = false
						break
					}
					sub = b.glyphAt(ib.Add(box.Min), gl.Row, gl.Baseline, gray)
					sub.Derived = true
					c := st.Char + k
					if f, w := measured(sub.Box, sub.Baseline, b.XHeight); priorCost(f, w, priors[c]) > 0.4 {
						ok = false
						break
					}
					if cen, has := centroids[Key{chars[c], spanOf(c)}]; has && encoder.NormalizedDistance(sub.Code, cen, bits) > 0.15 {
						ok = false
						break
					}
					subs[k] = sub
				}
				if ok {
					for k, sub := range subs {
						c := st.Char + k
						r := chars[c]
						if span := spanOf(c); span < 0 {
							a.Samples[r] = append(a.Samples[r], sub)
						} else {
							if a.Emphasis[span] == nil {
								a.Emphasis[span] = map[rune][]Glyph{}
							}
							a.Emphasis[span][r] = append(a.Emphasis[span][r], sub)
						}
						a.Recovered++
					}
				}
			}
		case Split, Split3:
			k := 2
			if st.Kind == Split3 {
				k = 3
			}
			for i := range k {
				a.Assign[st.Glyph+i] = []int{st.Char}
			}
			rowQ[b.Glyphs[st.Glyph].Row].Penalized++
			a.Penalized++
		case Insert:
			rowQ[b.Glyphs[st.Glyph].Row].Penalized++
			rowQ[b.Glyphs[st.Glyph].Row].Unexplained++
			a.Penalized++
			a.Unexplained++
		case Delete:
			g := min(st.Glyph, m-1)
			rowQ[b.Glyphs[g].Row].Penalized++
			rowQ[b.Glyphs[g].Row].Unexplained++
			a.Penalized++
			a.Unexplained++
		case Space:
			if st.Glyph > 0 && st.Glyph < m && !math.IsInf(b.Gap[st.Glyph], 1) {
				wordGaps = append(wordGaps, b.Gap[st.Glyph]*b.XHeight)
			}
		}
	}
	for i := range rowQ {
		if rowQ[i].Matched > 0 {
			rowQ[i].MeanDistance /= float64(rowQ[i].Matched)
		}
	}
	a.Rows = rowQ
	for _, s := range distSum {
		if s[1] >= 2 {
			spreadSum += s[0] / s[1]
			spreadN++
		}
	}
	if spreadN > 0 {
		a.Spread = spreadSum / float64(spreadN)
	}
	a.CapHeight = median(capHeights)
	a.LetterGap = median(letterGaps)
	a.WordGap = median(wordGaps)
	return a, nil
}

// centroidsOf pools the frame codes matched to each key and takes the
// per-bit majority.
func centroidsOf(b *Block, chars []rune, spanOf func(int) int, path Path, bits int) map[Key]bitcode.Code {
	pools := map[Key][]bitcode.Code{}
	for _, st := range path {
		if st.Kind != Match {
			continue
		}
		k := Key{chars[st.Char], spanOf(st.Char)}
		pools[k] = append(pools[k], b.Glyphs[st.Glyph].Code)
	}
	out := make(map[Key]bitcode.Code, len(pools))
	for k, codes := range pools {
		out[k] = bitcode.Majority(codes, bits)
	}
	return out
}

func samePath(a, b Path) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Kind != b[i].Kind || a[i].Glyph != b[i].Glyph || a[i].Char != b[i].Char {
			return false
		}
	}
	return true
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	return s[len(s)/2]
}

// UnexplainedFraction is the share of glyphs the reference could not account
// for: inserted glyphs and deleted characters over the glyph count.
func (a *Alphabet) UnexplainedFraction() float64 {
	if len(a.Block.Glyphs) == 0 {
		return 1
	}
	return float64(a.Unexplained) / float64(len(a.Block.Glyphs))
}

// OK says whether the alphabet is trustworthy: within-character spread at or
// under maxSpread and unexplained fraction at or under maxUnexplained.
func (a *Alphabet) OK(maxSpread, maxUnexplained float64) bool {
	return a.Spread <= maxSpread && a.UnexplainedFraction() <= maxUnexplained
}
