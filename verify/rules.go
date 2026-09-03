package verify

import (
	"fmt"
	"image"
	"image/draw"
	"math"
	"sort"

	"treasury/internal/alphabet"
	"treasury/internal/bitcode"
	"treasury/internal/encoder"
	"treasury/internal/preprocess"
	"treasury/internal/region"
	"treasury/internal/spell"
)

// encodedRegion is a region proposal with its line code. The code is taken
// from the tight ink box inside the proposal so that padding never enters
// the comparison; the padded box is kept for evidence.
type encodedRegion struct {
	box           image.Rectangle // padded, for evidence
	ink           image.Rectangle // tight, what was encoded
	code          bitcode.Code
	patch         encoder.Patch
	comps         []image.Rectangle // component boxes, left to right
	baseline      int
	lowConfidence bool
}

func encodeRegions(pre *preprocess.Result, regions []region.Region, enc encoder.Encoder, glareFrac float64) []encodedRegion {
	out := make([]encodedRegion, 0, len(regions))
	for _, r := range regions {
		ib, ok := pre.Bin.Crop(r.Box).InkBounds()
		if !ok {
			continue
		}
		ink := ib.Add(r.Box.Min)
		gray := image.NewGray(image.Rect(0, 0, ink.Dx(), ink.Dy()))
		draw.Draw(gray, gray.Rect, pre.Gray, ink.Min, draw.Src)
		p := encoder.Patch{Bin: pre.Bin.Crop(ink), Gray: gray}
		er := encodedRegion{box: r.Box, ink: ink, code: enc.Encode(p), patch: p, lowConfidence: r.Glare > glareFrac}
		bottoms := make([]int, 0, len(r.Comps))
		for _, c := range r.Comps {
			er.comps = append(er.comps, c.Box)
			bottoms = append(bottoms, c.Box.Max.Y)
		}
		er.baseline = modeInt(bottoms)
		out = append(out, er)
	}
	return out
}

// modeInt returns the value with the most support within ±1.
func modeInt(v []int) int {
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

type match struct {
	region, word, d int
}

// debugScored, when set by a test, receives every refined pair of a claim;
// digitProbe receives each observed digit's distances to all ten digits.
var (
	debugScored func(claim string, regions []encodedRegion, pairs []scored)
	digitProbe  func(line string)
)

// scored is a (region, candidate) pair after refinement: a normalized
// distance, the raw bits behind it, and how it was measured.
type scored struct {
	region, word int
	dist         float64 // normalized to [0, 1]
	raw, bits    int
	glyphs       int  // candidate glyphs when refined
	refined      bool // glyph-wise alignment succeeded
	penalized    int
	word_        spell.Codeword
	path         alphabet.Path
	target       alphabet.Target
	obs          alphabet.Observed
}

// decide spells the claim's candidates and applies the rules in two passes.
// The coarse pass compares every region's line code with every candidate's
// spelled code and keeps the nearest pairs. The refinement aligns each
// kept region's components to the candidate glyph by glyph with the
// alphabet's decoder, which is what makes a single differing digit count;
// where that alignment fails the coarse distance stands.
//
//	d1 ≤ radius, margin ≥ tie, value == expected → VERIFIED
//	d1 ≤ radius, margin ≥ tie, value != expected → MISMATCH (observed = value)
//	d1 ≤ radius, margin <  tie                   → REVIEW  (both values)
//	d1 >  radius                                  → NOT_FOUND or SKIPPED
func (e *Engine) decide(c Claim, sp *spell.Speller, pre *preprocess.Result, regions []encodedRegion) Verdict {
	v := Verdict{Claim: c.Name, Expected: c.Expected}
	var words []spell.Codeword
	unspellable := 0
	for _, cand := range c.Candidates {
		w, err := sp.Spell(cand.Text, cand.Value)
		if err != nil {
			unspellable++
			continue
		}
		words = append(words, w)
	}
	if len(words) == 0 || len(regions) == 0 {
		v.Status = NotFound
		if !c.Required {
			v.Status = Skipped
		}
		if unspellable > 0 {
			v.Reason = "unspellable"
		}
		return v
	}
	lineBits := e.lineEnc.Bits()
	radius := c.Radius
	if radius == 0 {
		radius = e.opt.DefaultRadius
	}

	// Coarse pass: the K nearest (region, word) pairs overall.
	const topK = 24
	top := make([]match, 0, topK+1)
	for ri, r := range regions {
		for wi, w := range words {
			d := bitcode.Distance(r.code, w.Code)
			if len(top) == topK && d >= top[topK-1].d {
				continue
			}
			top = append(top, match{ri, wi, d})
			sort.Slice(top, func(a, b int) bool { return top[a].d < top[b].d })
			if len(top) > topK {
				top = top[:topK]
			}
		}
	}

	glyphBits := sp.GlyphEnc.Bits()
	// score aligns one candidate to one framing of a region's components.
	score := func(m match, obs alphabet.Observed, t alphabet.Target, n int) (scored, bool) {
		path, st, ok := alphabet.Decode(obs, t, alphabet.DefaultPenalties())
		if !ok {
			return scored{}, false
		}
		// Structural steps are scored by their union or pair shape and carry
		// a quarter-glyph penalty; unexplained glyphs and characters cost
		// half a glyph each.
		raw := st.Hamming + st.Structural*glyphBits/4 + st.Unexplained*glyphBits/2
		return scored{
			region: m.region, word: m.word, dist: float64(raw) / float64(n*glyphBits), raw: raw, bits: n * glyphBits,
			glyphs: n, refined: true, penalized: st.Structural + st.Unexplained, word_: words[m.word],
			path: path, target: t, obs: obs,
		}, true
	}
	// refine re-scores a coarse pair glyph by glyph. With fixed set, the
	// region is framed exactly as in fixed (a competitor must be judged
	// under the winner's geometry, not one that flatters it); otherwise
	// the framing is estimated from the ink heights and a small
	// neighbourhood of scales and baselines is searched.
	refine := func(m match, fixed *scored) scored {
		s := scored{region: m.region, word: m.word, dist: float64(m.d) / float64(lineBits), raw: m.d, bits: lineBits, word_: words[m.word]}
		reg := regions[m.region]
		if len(reg.comps) == 0 {
			return s
		}
		t, top, bottom, err := sp.Target(words[m.word].Text)
		if err != nil || bottom <= top || reg.ink.Dy() == 0 {
			return s
		}
		n := 0
		for _, code := range t.Codes {
			if code != nil {
				n++
			}
		}
		if fixed != nil {
			if f, ok := score(m, fixed.obs, t, n); ok {
				return f
			}
			return s
		}
		xh0 := sp.A.XHeight * float64(reg.ink.Dy()) / float64(bottom-top)
		found := false
		for _, scale := range []float64{1, 0.96, 1.04} {
			for _, dy := range []int{0, -1, 1} {
				obs := alphabet.NewObserved(pre.Bin, sp.GlyphEnc, reg.comps, reg.baseline+dy, xh0*scale)
				if f, ok := score(m, obs, t, n); ok && (!found || f.dist < s.dist) {
					s, found = f, true
				}
			}
		}
		return s
	}
	best := refine(top[0], nil)
	var all []scored
	if debugScored != nil {
		all = append(all, best)
	}
	for _, m := range top[1:] {
		f := refine(m, nil)
		if debugScored != nil {
			all = append(all, f)
		}
		if f.dist < best.dist {
			best = f
		}
	}
	if debugScored != nil {
		debugScored(c.Name, regions, all)
	}
	if digitProbe != nil && best.refined {
		for _, st := range best.path {
			if st.Kind != alphabet.Match || st.Char >= len(best.target.Text) {
				continue
			}
			r := best.target.Text[st.Char]
			if r < '0' || r > '9' {
				continue
			}
			line := fmt.Sprintf("%s glyph %d as %q:", c.Name, st.Glyph, r)
			for d := '0'; d <= '9'; d++ {
				t, _, _, err := sp.Target(string(d))
				if err != nil {
					continue
				}
				line += fmt.Sprintf(" %c=%d", d, bitcode.Distance(best.obs.Codes[st.Glyph], t.Codes[0]))
			}
			digitProbe(line)
		}
	}

	// Competitor: nearest different value on the winning region, refined
	// among the ten nearest by coarse distance.
	var others []match
	for wi, w := range words {
		if w.Value == best.word_.Value {
			continue
		}
		others = append(others, match{best.region, wi, bitcode.Distance(regions[best.region].code, w.Code)})
	}
	sort.Slice(others, func(a, b int) bool { return others[a].d < others[b].d })
	comp := scored{region: -1, dist: math.Inf(1)}
	var fixed *scored
	if best.refined {
		fixed = &best
	}
	for i, m := range others {
		if i >= 10 {
			break
		}
		if f := refine(m, fixed); f.dist < comp.dist {
			comp = f
		}
	}

	reg := regions[best.region]
	ev := &Evidence{
		Region: reg.box, Crop: reg.patch.Gray, Codeword: best.word_.Patch,
		Text: best.word_.Text, Params: best.word_.Params,
		D1: best.raw, D2: -1, Radius: int(math.Round(radius * float64(best.bits))), Bits: best.bits,
		Refined: best.refined, Glyphs: best.glyphs, Penalized: best.penalized,
		LowConfidence: reg.lowConfidence,
	}
	if comp.region >= 0 {
		ev.D2 = comp.raw
		ev.Competitor = comp.word_.Value
	}
	v.Evidence = ev
	if best.dist > radius {
		v.Status = NotFound
		if !c.Required {
			v.Status = Skipped
		}
		return v
	}
	// Margin: glyph-wise, the two candidates only differ where their
	// characters differ, so the margin is counted per differing glyph;
	// line-wise it is the doc's fraction of the code length.
	if comp.region >= 0 {
		var margin float64
		if best.refined && comp.refined {
			// Per differing glyph: the tie fraction, or one and a half times
			// the alphabet's own within-character spread when the image is
			// noisier than that. The spread is the noise of one glyph
			// comparison; a difference under that is not evidence.
			k := differing([]rune(best.word_.Text), []rune(comp.word_.Text))
			per := math.Max(e.opt.TieMargin, 1.5*sp.A.Spread)
			margin = per * float64(k) / float64(max(1, best.glyphs))
		} else {
			margin = e.opt.LineTieMargin
		}
		if comp.dist-best.dist < margin {
			v.Status = Review
			v.Candidates = []string{best.word_.Value, comp.word_.Value}
			return v
		}
	}
	v.Observed = best.word_.Value
	if best.word_.Value == c.Expected {
		v.Status = Verified
	} else {
		v.Status = Mismatch
	}
	return v
}

// differing counts the non-space positions where two texts differ, plus the
// length difference; it is at least one.
func differing(a, b []rune) int {
	n := 0
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] && (a[i] != ' ' || b[i] != ' ') {
			n++
		}
	}
	if len(a) > len(b) {
		n += len(a) - len(b)
	} else {
		n += len(b) - len(a)
	}
	return max(1, n)
}
