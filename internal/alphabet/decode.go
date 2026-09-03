package alphabet

import (
	"image"
	"math"

	"treasury/internal/bitcode"
	"treasury/internal/bitmap"
	"treasury/internal/encoder"
)

// Observed is a run of components in reading order with their frame codes.
type Observed struct {
	Boxes    []image.Rectangle
	Codes    []bitcode.Code
	Gaps     []float64 // gap before each glyph in x-heights; +Inf for the first
	Baseline int
	XHeight  float64
	Bin      *bitmap.Bitmap
	Enc      encoder.Encoder
}

// NewObserved frames and encodes boxes (sorted left to right) at the given
// baseline and x-height.
func NewObserved(bin *bitmap.Bitmap, enc encoder.Encoder, boxes []image.Rectangle, baseline int, xh float64) Observed {
	o := Observed{Boxes: boxes, Baseline: baseline, XHeight: xh, Bin: bin, Enc: enc}
	o.Codes = make([]bitcode.Code, len(boxes))
	o.Gaps = make([]float64, len(boxes))
	maxX := 0
	for i, b := range boxes {
		o.Codes[i] = enc.Encode(Frame(bin, b, baseline, xh))
		if i == 0 {
			o.Gaps[i] = math.Inf(1)
		} else {
			o.Gaps[i] = float64(b.Min.X-maxX) / xh
		}
		maxX = max(maxX, b.Max.X)
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
	Pair   func(c int) bitcode.Code
	Triple func(c int) bitcode.Code // characters c, c+1, c+2 composed as one glyph; may be nil
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
		feats[g], widths[g] = measured(b, obs.Baseline, obs.XHeight)
	}
	shapeOf := func(code bitcode.Code, f feat, w float64, c int) float64 {
		pc := priorCost(f, w, priors[c])
		if t.Codes[c] == nil {
			return 0.25*pc + 0.6
		}
		return 0.25*pc + 4*encoder.NormalizedDistance(code, t.Codes[c], bits)
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
	unionBox := func(g, k int) image.Rectangle {
		box := obs.Boxes[g]
		for i := g + 1; i < g+k; i++ {
			box = box.Union(obs.Boxes[i])
		}
		return box
	}
	unionCodes := map[int]bitcode.Code{}
	unionCode := func(g, k int) (bitcode.Code, bool) {
		for i := g + 1; i < g+k; i++ {
			if obs.Gaps[i] > 0.3 {
				return nil, false
			}
		}
		key := g*4 + k
		if code, ok := unionCodes[key]; ok {
			return code, true
		}
		code := obs.Enc.Encode(Frame(obs.Bin, unionBox(g, k), obs.Baseline, obs.XHeight))
		unionCodes[key] = code
		return code, true
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
		code, ok := unionCode(g, k)
		if !ok {
			return math.NaN()
		}
		f, w := measured(unionBox(g, k), obs.Baseline, obs.XHeight)
		return shapeOf(code, f, w, c)
	}
	pr.union = func(g, c int) float64 { return unionCost(g, 2, c) }
	pr.union3 = func(g, c int) float64 { return unionCost(g, 3, c) }

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
				st.Hamming += bitcode.Distance(obs.Codes[s.Glyph], t.Codes[s.Char])
			}
			st.Matched++
		case Merge:
			st.Structural++
			if code := pairCode(s.Char); code != nil {
				st.Hamming += bitcode.Distance(obs.Codes[s.Glyph], code)
			}
		case Merge3:
			st.Structural++
			if code := tripleCode(s.Char); code != nil {
				st.Hamming += bitcode.Distance(obs.Codes[s.Glyph], code)
			}
		case Split, Split3:
			st.Structural++
			k := 2
			if s.Kind == Split3 {
				k = 3
			}
			if code, ok := unionCode(s.Glyph, k); ok && t.Codes[s.Char] != nil {
				st.Hamming += bitcode.Distance(code, t.Codes[s.Char])
			}
		case Insert, Delete:
			st.Unexplained++
		}
	}
	return path, st, true
}
