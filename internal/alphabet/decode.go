package alphabet

import (
	"image"
	"math"

	"treasury/internal/bitcode"
	"treasury/internal/bitmap"
	"treasury/internal/encoder"
)

// Observed is a run of components in reading order with their frame codes.
// Union codes of neighbouring components, which depend on the run alone,
// are computed on demand and shared by every alignment of the run.
type Observed struct {
	Boxes     []image.Rectangle
	Codes     []bitcode.Code
	Gaps      []float64 // gap before each glyph in x-heights; +Inf for the first
	Baselines []int     // per glyph, from the run's fitted baseline
	Baseline  int       // the run's baseline at its centre, for reporting
	XHeight   float64
	Bin       *bitmap.Bitmap
	Enc       encoder.Encoder
	// Coder, when set, supplies the code of a frame instead of Enc: an
	// encoder whose code tolerates the framing's error by construction
	// can be asked once per box, whatever the framing.
	Coder func(box image.Rectangle, baseline int, xh float64) bitcode.Code

	unions map[int]bitcode.Code
}

func (o Observed) code(box image.Rectangle, baseline int, xh float64) bitcode.Code {
	if o.Coder != nil {
		return o.Coder(box, baseline, xh)
	}
	return o.Enc.Encode(Frame(o.Bin, box, baseline, xh))
}

// unionBox is the box spanning glyphs g through g+k-1.
func (o Observed) unionBox(g, k int) image.Rectangle {
	box := o.Boxes[g]
	for i := g + 1; i < g+k; i++ {
		box = box.Union(o.Boxes[i])
	}
	return box
}

// unionCode is the frame code of glyphs g through g+k-1 taken as one, or
// false when they are not close enough to be one broken character.
// UnionCode is the code of glyphs g..g+k-1 framed as one, when they are
// close enough to be pieces of one glyph.
func (o Observed) UnionCode(g, k int) (bitcode.Code, bool) { return o.unionCode(g, k) }

func (o Observed) unionCode(g, k int) (bitcode.Code, bool) {
	// Pieces of one broken glyph touch or nearly touch; letters set
	// apart by their normal spacing, a tenth of an x-height and more, are
	// not one glyph, and their union would only cost an encoding.
	for i := g + 1; i < g+k; i++ {
		if o.Gaps[i] > 0.15 {
			return nil, false
		}
	}
	key := g*4 + k
	if code, ok := o.unions[key]; ok {
		return code, true
	}
	code := o.code(o.unionBox(g, k), o.Baselines[g], o.XHeight)
	o.unions[key] = code
	return code, true
}

// NewObserved frames and encodes boxes (sorted left to right) at the given
// x-height, each on its own baseline; dy shifts every baseline.
func NewObserved(bin *bitmap.Bitmap, enc encoder.Encoder, boxes []image.Rectangle, baselines []int, dy int, xh float64) Observed {
	return NewObservedWith(bin, enc, boxes, baselines, dy, xh, nil)
}

// NewObservedWith is NewObserved with a code supplier; see Observed.Coder.
func NewObservedWith(bin *bitmap.Bitmap, enc encoder.Encoder, boxes []image.Rectangle, baselines []int, dy int, xh float64, coder func(image.Rectangle, int, float64) bitcode.Code) Observed {
	o := Observed{Boxes: boxes, XHeight: xh, Bin: bin, Enc: enc, Coder: coder, unions: map[int]bitcode.Code{}}
	o.Codes = make([]bitcode.Code, len(boxes))
	o.Gaps = make([]float64, len(boxes))
	o.Baselines = make([]int, len(boxes))
	maxX := 0
	for i, b := range boxes {
		o.Baselines[i] = baselines[i] + dy
		o.Codes[i] = o.code(b, o.Baselines[i], xh)
		if i == 0 {
			o.Gaps[i] = math.Inf(1)
		} else {
			o.Gaps[i] = float64(b.Min.X-maxX) / xh
		}
		maxX = max(maxX, b.Max.X)
	}
	if n := len(boxes); n > 0 {
		o.Baseline = o.Baselines[n/2]
	}
	return o
}

// Target is text with a frame code per non-space character, indexed by rune
// position; entries at spaces are nil. Pair, when set, returns the frame
// code of characters c and c+1 composed as one glyph, so a touching pair in
// the image can be scored by shape.
type Target struct {
	Text   []rune
	Codes  []bitcode.Code
	Alt    map[int][]bitcode.Code // alternative codes per character (synthesized in other faces); the nearest counts
	Pair   func(c int) bitcode.Code
	Triple func(c int) bitcode.Code // characters c, c+1, c+2 composed as one glyph; may be nil
}

// distance is the Hamming distance from an observed code to the target's
// character c: its code, or an alternative when that beats the code by a
// clear margin. Alternatives exist so a true digit synthesized in the wrong
// face can find its shape in another; a wrong digit must not borrow one by
// a hair, or every competitor closes in and nothing decides.
func (t Target) distance(code bitcode.Code, c int) int {
	d := bitcode.Distance(code, t.Codes[c])
	if alts := t.Alt[c]; len(alts) > 0 {
		gain := len(t.Codes[c]) * 64 / 50 // two percent of the code length
		for _, alt := range alts {
			if ad := bitcode.Distance(code, alt); ad+gain < d {
				d = ad + gain
			}
		}
	}
	return d
}

// DecodeStats summarizes an alignment of observed glyphs to a target.
type DecodeStats struct {
	Hamming     int // summed over matched glyphs, unions, and scored pairs
	Matched     int
	Structural  int // merge and split steps: the image explains the text, in pieces
	Unexplained int // insert and delete steps
	Cost        float64
}

// Decode aligns the observed run to the target with the same dynamic
// programme that learns the alphabet, scoring a match by the Hamming
// distance between the observed frame code and the target's, lightly
// blended with the character prior. Broken glyphs are scored by the frame
// of their union, touching pairs by the target's composed pair. It reports
// false when no alignment exists.
func Decode(obs Observed, t Target, pen Penalties) (Path, DecodeStats, bool) {
	n, m := len(t.Text), len(obs.Codes)
	if n == 0 || m == 0 {
		return nil, DecodeStats{}, false
	}
	bits := obs.Enc.Bits()
	spaces := make([]int, n+1)
	priors := make([]prior, n)
	for c, r := range t.Text {
		spaces[c+1] = spaces[c]
		if r == ' ' {
			spaces[c+1]++
		}
		priors[c] = charPrior(r)
	}
	feats := make([]feat, m)
	widths := make([]float64, m)
	for g, b := range obs.Boxes {
		feats[g], widths[g] = measured(b, obs.Baselines[g], obs.XHeight)
	}
	shapeOf := func(code bitcode.Code, f feat, w float64, c int) float64 {
		pc := priorCost(f, w, priors[c])
		if t.Codes[c] == nil {
			return 0.25*pc + 0.6
		}
		return 0.25*pc + 4*math.Min(1, float64(t.distance(code, c))/float64(bits))
	}
	pairCodes := map[int]bitcode.Code{}
	pairCode := func(c int) bitcode.Code {
		if t.Pair == nil {
			return nil
		}
		if code, ok := pairCodes[c]; ok {
			return code
		}
		code := t.Pair(c)
		pairCodes[c] = code
		return code
	}
	tripleCodes := map[int]bitcode.Code{}
	tripleCode := func(c int) bitcode.Code {
		if t.Triple == nil {
			return nil
		}
		if code, ok := tripleCodes[c]; ok {
			return code
		}
		code := t.Triple(c)
		tripleCodes[c] = code
		return code
	}
	if obs.unions == nil {
		obs.unions = map[int]bitcode.Code{}
	}
	pr := &problem{chars: t.Text, m: m, gap: obs.Gaps, spaces: spaces, pen: pen}
	pr.shape = func(g, c int) float64 { return shapeOf(obs.Codes[g], feats[g], widths[g], c) }
	pr.pair = func(g, c int) float64 {
		pc := priorCost(feats[g], widths[g], mergedPrior(priors[c], priors[c+1]))
		if code := pairCode(c); code != nil {
			return 0.25*pc + 4*encoder.NormalizedDistance(obs.Codes[g], code, bits)
		}
		return pc + 0.6
	}
	pr.triple = func(g, c int) float64 {
		pc := priorCost(feats[g], widths[g], mergedPrior(mergedPrior(priors[c], priors[c+1]), priors[c+2]))
		if code := tripleCode(c); code != nil {
			return 0.25*pc + 4*encoder.NormalizedDistance(obs.Codes[g], code, bits)
		}
		return pc + 0.6
	}
	unionCost := func(g, k, c int) float64 {
		code, ok := obs.unionCode(g, k)
		if !ok {
			return math.NaN()
		}
		f, w := measured(obs.unionBox(g, k), obs.Baselines[g], obs.XHeight)
		return shapeOf(code, f, w, c)
	}
	pr.union = func(g, c int) float64 { return unionCost(g, 2, c) }
	pr.union3 = func(g, c int) float64 { return unionCost(g, 3, c) }
	pr.rejoin = func(g, c int) float64 {
		code, ok := obs.unionCode(g, 2)
		if !ok {
			return math.NaN()
		}
		f, w := measured(obs.unionBox(g, 2), obs.Baselines[g], obs.XHeight)
		pc := priorCost(f, w, mergedPrior(priors[c], priors[c+1]))
		if pair := pairCode(c); pair != nil {
			return 0.25*pc + 4*encoder.NormalizedDistance(code, pair, bits)
		}
		return pc + 0.6
	}

	band := max(6, int(0.05*float64(m)))
	var path Path
	var cost float64
	for {
		var touched bool
		path, cost, touched = pr.align(band)
		if !touched || band >= m {
			break
		}
		band *= 2
	}
	if path == nil {
		return nil, DecodeStats{}, false
	}
	st := DecodeStats{Cost: cost}
	for _, s := range path {
		switch s.Kind {
		case Match:
			if t.Codes[s.Char] != nil {
				st.Hamming += t.distance(obs.Codes[s.Glyph], s.Char)
			}
			st.Matched++
		case Merge:
			st.Structural++
			if code := pairCode(s.Char); code != nil {
				st.Hamming += bitcode.Distance(obs.Codes[s.Glyph], code)
			}
		case Merge3:
			// Three characters in one glyph: two extra characters are
			// explained by one shape, so it counts as two structural steps;
			// otherwise a candidate two units longer than the region fits
			// it for a quarter glyph.
			st.Structural += 2
			if code := tripleCode(s.Char); code != nil {
				st.Hamming += bitcode.Distance(obs.Codes[s.Glyph], code)
			}
		case Rejoin:
			st.Structural++
			if union, ok := obs.unionCode(s.Glyph, 2); ok {
				if pair := pairCode(s.Char); pair != nil {
					st.Hamming += bitcode.Distance(union, pair)
				}
			}
		case Split, Split3:
			// A glyph in pieces is one structural step however many pieces:
			// the cutter made them, and they are not evidence against the
			// candidate.
			st.Structural++
			k := 2
			if s.Kind == Split3 {
				k = 3
			}
			if code, ok := obs.unionCode(s.Glyph, k); ok && t.Codes[s.Char] != nil {
				st.Hamming += t.distance(code, s.Char)
			}
		case Insert, Delete:
			st.Unexplained++
		}
	}
	return path, st, true
}
