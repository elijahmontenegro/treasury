package alphabet

import (
	"errors"
	"image"
	"math"
	"sort"
	"strings"
	"unicode"

	"treasury/internal/bitcode"
	"treasury/internal/bitmap"
	"treasury/internal/encoder"
	"treasury/internal/imgops"
	"treasury/internal/region"
)

// Span is a half-open rune range of the reference expected in a heavier weight.
type Span struct{ Start, End int }

// Options control learning.
type Options struct {
	BandFrac      float64 // DP band as a fraction of the glyph count
	MinBand       int
	MaxPasses     int
	Penalties     Penalties
	Encoder       encoder.Encoder // glyph encoder
	MinGlyphs     float64         // a block needs at least this fraction of the reference's non-space characters
	MaxCharSpread float64         // a character with two or more samples whose mean distance to their centroid exceeds this is unlearned
}

// DefaultOptions are the starting values.
func DefaultOptions() Options {
	return Options{BandFrac: 0.05, MinBand: 6, MaxPasses: 3, Penalties: DefaultPenalties(), Encoder: encoder.Glyph(), MinGlyphs: 0.4, MaxCharSpread: 0.12}
}

// Key identifies a sample pool: a character in the body (Span -1) or inside
// an emphasis span.
type Key struct {
	R    rune
	Span int
}

// Anomaly is one glyph the reference does not account for well: an
// unexplained glyph or character, or a matched or split glyph far from its
// character's centroid.
type Anomaly struct {
	Kind     Kind            `json:"kind"`
	Glyph    int             `json:"glyph"`
	Char     string          `json:"char"`              // the character the reference expects
	Nearest  string          `json:"nearest,omitempty"` // the character the glyph looks most like, for outliers
	Distance float64         `json:"distance"`          // normalized Hamming to the centroid; 0 for inserts and deletes
	Strong   bool            `json:"strong"`            // far beyond the outlier threshold, or unexplained
	Box      image.Rectangle `json:"box"`
}

// RowQuality is the alignment quality of one row of the block.
type RowQuality struct {
	Row             int
	Glyphs          int
	Matched         int
	Penalized       int       // merge, split, insert, delete steps on the row
	Unexplained     int       // insert and delete steps only: what the reference cannot account for
	PriorViolations int       // matched glyphs whose shape features contradict their character (a lowercase glyph for a capital)
	Outliers        int       // matched or split glyphs far from their character's centroid (a substituted letter)
	StrongOutliers  int       // outliers beyond one and a half times the threshold
	MeanDistance    float64   // mean normalized Hamming of matched glyphs to their centroids
	Anomalies       []Anomaly // the unexplained and outlier glyphs, with evidence
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

	Spans []Span

	// Unlearned are characters the reference contains whose samples did
	// not agree with each other: their samples are dropped, the speller
	// synthesizes them, and a verdict that rests on one is only a review.
	Unlearned map[rune]bool
	KeySpread map[Key]float64 // mean distance to centroid per pool with two or more samples

	Spread          float64 // mean within-character normalized Hamming spread over learned pools
	Matched         int
	Penalized       int // merge, split, insert, delete
	Unexplained     int // insert, delete
	PriorViolations int // matched glyphs contradicting their character's shape class
	Deleted         int // reference characters no glyph accounts for
	Chars           int // non-space characters of the reference
	Outliers        int // matched glyphs far from their centroid
	Recovered       int // samples cut out of merged pairs
	Rows            []RowQuality
	Cost            float64
	Band            int
	Passes          int
}

// ErrNoBlock is returned when no candidate block aligns.
var ErrNoBlock = errors.New("alphabet: no block aligns to the reference")

// Tracef, when set, receives one line per shape-class violation found while
// learning: which glyph, which characters, and the measured features that
// contradicted the prior. A diagnostic hook; nil in normal use.
var Tracef func(format string, args ...any)

func traceViolation(kind string, gl Glyph, box image.Rectangle, xh float64, text string, p prior) {
	if Tracef == nil {
		return
	}
	f, w := measured(box, gl.Baseline, xh)
	Tracef("violation %-6s row %d box %v base %d xh %.1f chars %q above=%.2f below=%.2f h=%.2f w=%.2f meas(tall=%d desc=%d small=%d) prior(tall=%d desc=%d small=%d width=%.2f)",
		kind, gl.Row, box, gl.Baseline, xh, text,
		float64(gl.Baseline-box.Min.Y)/xh, float64(box.Max.Y-gl.Baseline)/xh, float64(box.Dy())/xh, w,
		f.tall, f.desc, f.small, p.f.tall, p.f.desc, p.f.small, p.width)
}

// Find locates candidate blocks among the lines, learns from each, and keeps
// the one with the lowest alignment cost.
func Find(lines []region.Line, bin *bitmap.Bitmap, gray *image.Gray, ref string, spans []Span, opt Options) (*Alphabet, error) {
	need := 0
	for _, r := range ref {
		if r != ' ' {
			need++
		}
	}
	// Text set in capitals has no x-height to measure; see extract.
	const capToX = 1.45
	xhScale := 1.0
	if !strings.ContainsFunc(ref, unicode.IsLower) {
		xhScale = 1 / capToX
	}
	var best *Alphabet
	try := func(cl []int) {
		blk := extract(cl, lines, bin, gray, opt.Encoder, xhScale)
		if float64(len(blk.Glyphs)) < opt.MinGlyphs*float64(need) {
			return
		}
		a, err := learn(blk, gray, ref, spans, opt)
		if err != nil {
			return
		}
		if best == nil || a.Cost < best.Cost {
			best = a
		}
	}
	for _, cl := range Locate(lines, 3) {
		count := 0
		for _, i := range cl {
			count += len(lines[i].Comps)
		}
		if float64(count) <= 1.3*float64(need) {
			try(cl)
			continue
		}
		// The cluster holds more than the reference: rows of other text
		// set in the same size, above or below it. The windows of
		// consecutive rows nearest the reference's glyph count are
		// aligned, at most eight, and the cheapest alignment wins; a
		// window missing rows pays in deletions, one with extra rows in
		// insertions.
		type window struct {
			i, j int
			miss float64
		}
		var windows []window
		for i := range cl {
			n := 0
			for j := i; j < len(cl); j++ {
				n += len(lines[cl[j]].Comps)
				if float64(n) < 0.7*float64(need) {
					continue
				}
				if float64(n) > 1.3*float64(need) {
					break
				}
				windows = append(windows, window{i, j, math.Abs(float64(n) - float64(need))})
			}
		}
		sort.Slice(windows, func(a, b int) bool { return windows[a].miss < windows[b].miss })
		if len(windows) > 8 {
			windows = windows[:8]
		}
		for _, w := range windows {
			try(cl[w.i : w.j+1])
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
	pr.triple = func(g, c int) float64 {
		return priorCost(feats[g], widths[g], mergedPrior(mergedPrior(priors[c], priors[c+1]), priors[c+2]))
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
	// Two adjacent pieces as two touching characters: the union must look
	// like the pair. From the second pass on, the pair is composed from the
	// characters' own samples of the previous pass; in the first, the prior
	// decides.
	var pools map[Key][]int
	pairCodes := map[int]bitcode.Code{}
	pr.rejoin = func(g, c int) float64 {
		box, ok := unionBox(g, 2)
		if !ok {
			return math.NaN()
		}
		f, w := measured(box, b.Glyphs[g].Baseline, b.XHeight)
		pc := priorCost(f, w, mergedPrior(priors[c], priors[c+1]))
		if centroids == nil {
			return pc
		}
		pair, ok := pairCodes[c]
		if !ok {
			pair = nil
			ga, okA := pools[Key{chars[c], spanOf(c)}]
			gb, okB := pools[Key{chars[c+1], spanOf(c + 1)}]
			if okA && okB && len(ga) > 0 && len(gb) > 0 {
				pair = composePair(b, b.Glyphs[ga[0]], b.Glyphs[gb[0]], int(math.Round(0.1*b.XHeight)))
			}
			pairCodes[c] = pair
		}
		if pair == nil {
			return 0.5*pc + 0.6
		}
		key := g*4 + 2
		code, ok := unionCode[key]
		if !ok {
			code = b.enc.Encode(Frame(b.bin, box, b.Glyphs[g].Baseline, b.XHeight))
			unionCode[key] = code
		}
		return 0.5*pc + 4*encoder.NormalizedDistance(code, pair, bits)
	}

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
		centroids, pools = centroidsOf(b, chars, spanOf, path, bits)
		pairCodes = map[int]bitcode.Code{}
	}
	if centroids == nil {
		centroids, pools = centroidsOf(b, chars, spanOf, path, bits)
	}
	a := &Alphabet{
		Block: b, Path: path, Centroid: centroids, Bits: bits, Spans: spans,
		Samples: map[rune][]Glyph{}, Emphasis: map[int]map[rune][]Glyph{},
		Unlearned: map[rune]bool{}, KeySpread: map[Key]float64{},
		XHeight: b.XHeight, Cost: cost, Band: band, Passes: passes,
		Chars: n - spaces[n],
	}
	type scoredGlyph struct {
		glyph int
		char  int
		kind  Kind
		d     float64
	}
	rowDist := make([][]scoredGlyph, len(b.Rows))
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
			rowDist[gl.Row] = append(rowDist[gl.Row], scoredGlyph{st.Glyph, st.Char, Match, d})
			if priorCost(feats[st.Glyph], widths[st.Glyph], priors[st.Char]) >= 1 {
				rq.PriorViolations++
				a.PriorViolations++
				traceViolation("match", gl, gl.Box, b.XHeight, string(r), priors[st.Char])
			}
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
			if mp := mergedPrior(priors[st.Char], priors[st.Char+1]); priorCost(feats[st.Glyph], widths[st.Glyph], mp) >= 1 {
				rowQ[b.Glyphs[st.Glyph].Row].PriorViolations += 2
				a.PriorViolations += 2
				traceViolation("merge", b.Glyphs[st.Glyph], b.Glyphs[st.Glyph].Box, b.XHeight, string(chars[st.Char:st.Char+2]), mp)
			}
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
		case Merge3:
			a.Assign[st.Glyph] = []int{st.Char, st.Char + 1, st.Char + 2}
			rowQ[b.Glyphs[st.Glyph].Row].Penalized++
			a.Penalized++
			// A fused run whose shape class contradicts its characters
			// (lowercase glyphs standing for capitals) violates like a
			// match would; otherwise a cheap structural step could hide it.
			if mp := mergedPrior(mergedPrior(priors[st.Char], priors[st.Char+1]), priors[st.Char+2]); priorCost(feats[st.Glyph], widths[st.Glyph], mp) >= 1 {
				rowQ[b.Glyphs[st.Glyph].Row].PriorViolations += 3
				a.PriorViolations += 3
				traceViolation("merge3", b.Glyphs[st.Glyph], b.Glyphs[st.Glyph].Box, b.XHeight, string(chars[st.Char:st.Char+3]), mp)
			}
		case Rejoin:
			// Which piece holds which character is unknown; both stand for both.
			a.Assign[st.Glyph] = []int{st.Char, st.Char + 1}
			a.Assign[st.Glyph+1] = []int{st.Char, st.Char + 1}
			rowQ[b.Glyphs[st.Glyph].Row].Penalized++
			a.Penalized++
			if box, ok := unionBox(st.Glyph, 2); ok {
				if f, w := measured(box, b.Glyphs[st.Glyph].Baseline, b.XHeight); priorCost(f, w, mergedPrior(priors[st.Char], priors[st.Char+1])) >= 1 {
					rowQ[b.Glyphs[st.Glyph].Row].PriorViolations += 2
					a.PriorViolations += 2
					traceViolation("rejoin", b.Glyphs[st.Glyph], box, b.XHeight, string(chars[st.Char:st.Char+2]), mergedPrior(priors[st.Char], priors[st.Char+1]))
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
			row := b.Glyphs[st.Glyph].Row
			rowQ[row].Penalized++
			a.Penalized++
			// A split whose union does not look like the character is a
			// substitution the alignment absorbed in pieces.
			if box, ok := unionBox(st.Glyph, k); ok {
				if cen, has := centroids[Key{chars[st.Char], spanOf(st.Char)}]; has {
					code := b.enc.Encode(Frame(b.bin, box, b.Glyphs[st.Glyph].Baseline, b.XHeight))
					rowDist[row] = append(rowDist[row], scoredGlyph{st.Glyph, st.Char, st.Kind, encoder.NormalizedDistance(code, cen, bits)})
				}
			}
		case Insert:
			row := b.Glyphs[st.Glyph].Row
			rowQ[row].Penalized++
			rowQ[row].Unexplained++
			rowQ[row].Anomalies = append(rowQ[row].Anomalies, Anomaly{Kind: Insert, Glyph: st.Glyph, Strong: true, Box: b.Glyphs[st.Glyph].Box})
			a.Penalized++
			a.Unexplained++
		case Delete:
			g := min(st.Glyph, m-1)
			row := b.Glyphs[g].Row
			rowQ[row].Penalized++
			rowQ[row].Unexplained++
			rowQ[row].Anomalies = append(rowQ[row].Anomalies, Anomaly{Kind: Delete, Glyph: g, Char: string(chars[st.Char]), Strong: true, Box: b.Glyphs[g].Box})
			a.Penalized++
			a.Unexplained++
			a.Deleted++
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
	// Per-character acceptance: a pool of two or more samples whose mean
	// distance to their centroid exceeds the threshold did not teach that
	// character; its samples go, and the character is flagged. A pool of one
	// sample has no spread and stays, since the alignment as a whole has
	// already passed the unexplained and shape-class checks.
	for key, s := range distSum {
		if s[1] < 2 {
			continue
		}
		sp := s[0] / s[1]
		a.KeySpread[key] = sp
		if sp > opt.MaxCharSpread {
			a.Unlearned[key.R] = true
			if key.Span < 0 {
				delete(a.Samples, key.R)
			} else if a.Emphasis[key.Span] != nil {
				delete(a.Emphasis[key.Span], key.R)
			}
			delete(a.Centroid, key)
			continue
		}
		spreadSum += sp
		spreadN++
	}
	if spreadN > 0 {
		a.Spread = spreadSum / float64(spreadN)
	}
	// A matched glyph well beyond the alphabet's own spread that lies
	// nearer some other character's centroid than its own is not that
	// character: a substituted letter the alignment absorbed. A glyph far
	// from everything, twice the threshold, is an outlier regardless.
	outlier := math.Max(0.1, 4*a.Spread)
	codeOf := func(sg scoredGlyph) bitcode.Code {
		if sg.kind == Match {
			return b.Glyphs[sg.glyph].Code
		}
		k := 2
		if sg.kind == Split3 {
			k = 3
		}
		box, _ := unionBox(sg.glyph, k)
		return b.enc.Encode(Frame(b.bin, box, b.Glyphs[sg.glyph].Baseline, b.XHeight))
	}
	for i, ds := range rowDist {
		for _, sg := range ds {
			if sg.d <= outlier {
				continue
			}
			own := Key{chars[sg.char], spanOf(sg.char)}
			code := codeOf(sg)
			nearest, nearestD := "", math.Inf(1)
			for k, cen := range centroids {
				if k == own {
					continue
				}
				if d := encoder.NormalizedDistance(code, cen, bits); d < nearestD {
					nearest, nearestD = string(k.R), d
				}
			}
			if sg.d <= 2*outlier && nearestD+0.02 >= sg.d {
				continue
			}
			strong := sg.d > 1.5*outlier
			rowQ[i].Outliers++
			if strong {
				rowQ[i].StrongOutliers++
			}
			rowQ[i].Anomalies = append(rowQ[i].Anomalies, Anomaly{Kind: sg.kind, Glyph: sg.glyph, Char: string(chars[sg.char]), Nearest: nearest, Distance: sg.d, Strong: strong, Box: b.Glyphs[sg.glyph].Box})
			a.Outliers++
		}
	}
	a.CapHeight = median(capHeights)
	a.LetterGap = median(letterGaps)
	a.WordGap = median(wordGaps)
	return a, nil
}

// centroidsOf pools the glyphs matched to each key and takes the per-bit
// majority of their frame codes; it also returns the pools as glyph indices.
func centroidsOf(b *Block, chars []rune, spanOf func(int) int, path Path, bits int) (map[Key]bitcode.Code, map[Key][]int) {
	pools := map[Key][]int{}
	for _, st := range path {
		if st.Kind != Match {
			continue
		}
		k := Key{chars[st.Char], spanOf(st.Char)}
		pools[k] = append(pools[k], st.Glyph)
	}
	out := make(map[Key]bitcode.Code, len(pools))
	for k, glyphs := range pools {
		codes := make([]bitcode.Code, len(glyphs))
		for i, g := range glyphs {
			codes[i] = b.Glyphs[g].Code
		}
		out[k] = bitcode.Majority(codes, bits)
	}
	return out, pools
}

// composePair frames two glyph samples set side by side at the given gap,
// as a touching pair in the image would be framed.
func composePair(b *Block, x, y Glyph, gap int) bitcode.Code {
	xh := b.XHeight
	side := int(math.Ceil(2.2 * xh))
	w := x.Box.Dx() + gap + y.Box.Dx()
	canvas := bitmap.New(w+2*side, 4*side)
	base := 2 * side
	place := func(g Glyph, left int) image.Rectangle {
		top := base - (g.Baseline - g.Box.Min.Y)
		for yy := range g.Bin.H {
			for xx := range g.Bin.W {
				if g.Bin.Pix[yy*g.Bin.W+xx] != 0 {
					canvas.Set(left+xx, top+yy, 1)
				}
			}
		}
		return image.Rect(left, top, left+g.Bin.W, top+g.Bin.H)
	}
	box := place(x, side).Union(place(y, side+x.Box.Dx()+gap))
	return b.enc.Encode(Frame(canvas, box, base, xh))
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

// AddSample adds a glyph learned elsewhere on the image (from a claim that
// verified decisively) to the body pool, or to an emphasis pool when span
// is not negative, and refreshes that character's centroid. The glyph must
// already be at the alphabet's scale and framed with its encoder.
func (a *Alphabet) AddSample(r rune, span int, g Glyph) {
	key := Key{R: r, Span: span}
	if span < 0 {
		a.Samples[r] = append(a.Samples[r], g)
	} else {
		if a.Emphasis[span] == nil {
			a.Emphasis[span] = map[rune][]Glyph{}
		}
		a.Emphasis[span][r] = append(a.Emphasis[span][r], g)
	}
	var codes []bitcode.Code
	pool := a.Samples[r]
	if span >= 0 {
		pool = a.Emphasis[span][r]
	}
	for _, s := range pool {
		codes = append(codes, s.Code)
	}
	a.Centroid[key] = bitcode.Majority(codes, a.Bits)
}

// Rescaled builds a Glyph at the alphabet's scale from a component seen at
// another size: the crops are resampled by xhRef/xhSeen and the frame code
// is taken at the reference x-height, so the sample sits beside the learned
// ones as if printed at the reference's size.
func (a *Alphabet) Rescaled(bin *bitmap.Bitmap, gray *image.Gray, box image.Rectangle, baseline int, xhSeen float64) Glyph {
	scale := a.XHeight / xhSeen
	crop := bin.Crop(box)
	w, h := max(1, int(math.Round(float64(crop.W)*scale))), max(1, int(math.Round(float64(crop.H)*scale)))
	scaled := bitmap.New(w, h)
	cov := encoder.Coverage(crop, w, h)
	for i, v := range cov {
		if v >= 0.5 {
			scaled.Pix[i] = 1
		}
	}
	g := grayCrop(gray, box)
	gs := imgops.Resize(g, w, h)
	// Place on a canvas at a baseline and frame it like a reference glyph.
	side := int(math.Ceil(2.2 * a.XHeight))
	canvas := bitmap.New(w+2*side, 4*side)
	base := 2 * side
	rise := int(math.Round(float64(baseline-box.Min.Y) * scale)) // ink top above baseline
	top := base - rise
	for y := range h {
		for x := range w {
			if scaled.Pix[y*w+x] != 0 {
				canvas.Set(side+x, top+y, 1)
			}
		}
	}
	fbox := image.Rect(side, top, side+w, top+h)
	return Glyph{
		Box:      fbox,
		Baseline: base,
		Bin:      scaled,
		Gray:     gs,
		Code:     a.Block.enc.Encode(Frame(canvas, fbox, base, a.XHeight)),
		Derived:  true,
	}
}

// BodyStroke is the median stroke width in pixels over the body samples.
func (a *Alphabet) BodyStroke() float64 { return medianStroke(a.Samples) }

// SpanStroke is the median stroke width over an emphasis span's samples.
func (a *Alphabet) SpanStroke(span int) float64 { return medianStroke(a.Emphasis[span]) }

// medianStroke skips samples cut out of merged pairs.
func medianStroke(pool map[rune][]Glyph) float64 {
	var ws []float64
	for _, samples := range pool {
		for _, g := range samples {
			if g.Derived {
				continue
			}
			if w := g.Bin.StrokeWidth(); w > 0 {
				ws = append(ws, w)
			}
		}
	}
	return median(ws)
}

// SpanGlyphs returns, in reading order, the glyphs assigned to characters
// inside the given emphasis span.
func (a *Alphabet) SpanGlyphs(span int) []int {
	if span < 0 || span >= len(a.Spans) {
		return nil
	}
	s := a.Spans[span]
	var out []int
	for g, chars := range a.Assign {
		for _, c := range chars {
			if c >= s.Start && c < s.End {
				out = append(out, g)
				break
			}
		}
	}
	return out
}

// UnexplainedFraction is the share of glyphs the reference could not account
// for: inserted glyphs and deleted characters over the glyph count.
func (a *Alphabet) UnexplainedFraction() float64 {
	if len(a.Block.Glyphs) == 0 {
		return 1
	}
	return float64(a.Unexplained) / float64(len(a.Block.Glyphs))
}

// ViolationFraction is the share of the reference's explained characters
// whose glyph's shape class contradicts them: tall where short, descending
// where not, a quarter as wide as the characters it stands for. A merge
// counts for each character it covers.
// Text aligned to the wrong reference contradicts it in about a quarter of
// its glyphs; a real block in a few percent.
func (a *Alphabet) ViolationFraction() float64 {
	explained := a.Chars - a.Deleted
	if a.Matched == 0 || explained <= 0 {
		return 1
	}
	return float64(a.PriorViolations) / float64(explained)
}

// OK says whether the alignment is trustworthy at all: unexplained fraction
// at or under maxUnexplained and shape-class violations at or under
// maxViolations, both signs of text aligned to the wrong reference. Which
// characters were learned is decided per character, not here.
func (a *Alphabet) OK(maxUnexplained, maxViolations float64) bool {
	return a.UnexplainedFraction() <= maxUnexplained && a.ViolationFraction() <= maxViolations
}

// HeightUniformity is the share of the block's glyphs standing at least
// four fifths as tall as its tall glyphs (the 90th percentile of heights).
// Text set in capitals is uniform, about nine tenths; mixed-case text,
// with its x-height letters, is about a third. It tells which casing of a
// reference a block printed, which the alignment alone cannot: read as
// capitals with the x-height scaled to match, every glyph of a
// mixed-case block measures tall and contradicts nothing.
func (a *Alphabet) HeightUniformity() float64 {
	if a == nil || a.Block == nil || len(a.Block.Glyphs) == 0 {
		return 0
	}
	hs := make([]int, 0, len(a.Block.Glyphs))
	for _, g := range a.Block.Glyphs {
		hs = append(hs, g.Box.Dy())
	}
	sort.Ints(hs)
	tall := float64(hs[len(hs)*9/10])
	n := 0
	for _, h := range hs {
		if float64(h) >= 0.8*tall {
			n++
		}
	}
	return float64(n) / float64(len(hs))
}

// Recode is g's frame code under another encoder, at the alphabet's
// x-height, from the glyph's own crop.
func (a *Alphabet) Recode(g Glyph, enc encoder.Encoder) bitcode.Code {
	return enc.Encode(FrameCrop(g.Bin, g.Box, g.Baseline, a.XHeight))
}
