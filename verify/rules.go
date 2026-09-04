package verify

import (
	"fmt"
	"image"
	"image/draw"
	"math"
	"sort"
	"strings"
	"unicode"

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
	baselines     []int             // per component, from the fitted baseline
	baseline      int
	line          int // the line the region is a run of; -1 for bands
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
		er := encodedRegion{box: r.Box, ink: ink, code: enc.Encode(p), patch: p, line: r.Line, lowConfidence: r.Glare > glareFrac}
		for _, c := range r.Comps {
			er.comps = append(er.comps, c.Box)
		}
		slope, intercept := region.FitBaseline(er.comps)
		for _, b := range er.comps {
			er.baselines = append(er.baselines, region.BaselineAt(slope, intercept, (b.Min.X+b.Max.X)/2))
		}
		if n := len(er.baselines); n > 0 {
			er.baseline = er.baselines[n/2]
		}
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

// debugScored, when set by a test, receives every refined pair of a claim;
// digitProbe receives each observed digit's distances to all ten digits;
// debugProbe names a region box and candidate text to report on.
var (
	debugScored func(claim string, regions []encodedRegion, pairs []scored)
	digitProbe  func(line string)
	debugProbe  *probe
)

// probe names a region box and a candidate text to report on.
type probe struct {
	box  image.Rectangle
	text string
}

// scored is a (region, candidate) pair after scoring: a normalized
// distance, the raw bits behind it, and how it was measured.
type scored struct {
	region, word int
	dist         float64 // normalized to [0, 1]
	raw, bits    int
	glyphs       int  // candidate glyphs when refined
	refined      bool // glyph-wise alignment scored the pair
	penalized    int
	word_        spell.Codeword
	path         alphabet.Path
	target       alphabet.Target
	obs          alphabet.Observed
}

// decide spells the claim's candidates and applies the rules.
//
// Every structurally possible (region, candidate) pair is scored glyph by
// glyph: the region's components aligned to the candidate's glyph codes by
// the alphabet's decoder, at a geometry estimated from the ink heights. A
// region must have about as many components as the candidate has glyphs,
// allowing for touching and broken ones, and a similar aspect. The nearest
// pairs are then re-scored over a small neighbourhood of scales and
// baselines, and the nearest of those decodes the claim. The competitor is
// the nearest candidate of a different value on the same region under the
// winner's geometry. The spec's line hash is kept only for regions without
// components: as a ranker it puts short spurious pairs ahead of the true
// one whenever the label's face differs from the synthesized one.
//
//	d1 ≤ radius, margin ≥ tie, value == expected → VERIFIED
//	d1 ≤ radius, margin ≥ tie, value != expected → MISMATCH (observed = value)
//	d1 ≤ radius, margin <  tie                   → REVIEW  (both values)
//	d1 >  radius                                  → NOT_FOUND or SKIPPED
func (e *Engine) decide(c Claim, sp *spell.Speller, pre *preprocess.Result, regions []encodedRegion) (Verdict, *scored) {
	v := Verdict{Claim: c.Name, Expected: c.Expected}
	var words []spell.Codeword
	unspellable := 0
	for _, cand := range variants(c, sp.HasEmphasis()) {
		w, err := sp.SpellAs(cand.Text, cand.Value, cand.heavy)
		if err != nil {
			unspellable++
			continue
		}
		w.Params.Casing = cand.casing
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
		return v, nil
	}
	lineBits := e.lineEnc.Bits()
	glyphBits := sp.GlyphEnc.Bits()
	radius := c.Radius
	if radius == 0 {
		radius = e.opt.DefaultRadius
	}

	// Candidate shapes and glyph-wise targets, once per candidate.
	type shape struct {
		glyphs      int
		aspect      float64
		target      alphabet.Target
		top, bottom int
		ok          bool
	}
	shapes := make([]shape, len(words))
	for i, w := range words {
		s := shape{aspect: float64(w.Patch.Bin.W) / float64(max(1, w.Patch.Bin.H))}
		if t, top, bottom, err := sp.Target(w.Text, w.Heavy); err == nil && bottom > top {
			s.target, s.top, s.bottom, s.ok = t, top, bottom, true
			for _, code := range t.Codes {
				if code != nil {
					s.glyphs++
				}
			}
		}
		shapes[i] = s
	}

	// Observed frames per region and geometry, shared across candidates.
	type obsKey struct{ region, xh10, dy int }
	cache := map[obsKey]alphabet.Observed{}
	// Framings are shared to the quarter pixel of x-height: candidates of
	// one claim differ in their extents by less than that, and each
	// framing encodes every component of the region.
	observed := func(ri int, xh float64, dy int) alphabet.Observed {
		xh = math.Round(xh*4) / 4
		k := obsKey{ri, int(math.Round(xh * 4)), dy}
		if o, ok := cache[k]; ok {
			return o
		}
		o := alphabet.NewObserved(pre.Bin, sp.GlyphEnc, regions[ri].comps, regions[ri].baselines, dy, xh)
		cache[k] = o
		return o
	}
	fits := func(ri, wi int) bool {
		r, s := regions[ri], shapes[wi]
		if !s.ok || len(r.comps) == 0 {
			return false
		}
		slack := max(3, s.glyphs/4)
		if len(r.comps) < s.glyphs-slack || len(r.comps) > s.glyphs+slack {
			return false
		}
		ratio := float64(r.ink.Dx()) / float64(max(1, r.ink.Dy())) / s.aspect
		return ratio >= 0.6 && ratio <= 1.6
	}
	nominalXH := func(ri, wi int) float64 {
		return sp.A.XHeight * float64(regions[ri].ink.Dy()) / float64(shapes[wi].bottom-shapes[wi].top)
	}
	lineScore := func(ri, wi int) scored {
		d := bitcode.Distance(regions[ri].code, words[wi].Code)
		return scored{region: ri, word: wi, dist: float64(d) / float64(lineBits), raw: d, bits: lineBits, word_: words[wi]}
	}
	// score aligns one candidate to one framing of a region's components.
	score := func(ri, wi int, obs alphabet.Observed) (scored, bool) {
		s := shapes[wi]
		path, st, ok := alphabet.Decode(obs, s.target, alphabet.DefaultPenalties())
		if !ok {
			return scored{}, false
		}
		// Structural steps are scored by their union or pair shape and carry
		// a quarter-glyph penalty. An unexplained glyph or character costs
		// a whole glyph: ink the candidate does not account for means the
		// region is not that text, however well the rest matches, and at
		// half a glyph "(90 Proof)" passed for "190 Proof".
		raw := st.Hamming + st.Structural*glyphBits/4 + st.Unexplained*glyphBits
		return scored{
			region: ri, word: wi, dist: float64(raw) / float64(s.glyphs*glyphBits), raw: raw, bits: s.glyphs * glyphBits,
			glyphs: s.glyphs, refined: true, penalized: st.Structural + st.Unexplained, word_: words[wi],
			path: path, target: s.target, obs: obs,
		}, true
	}
	// refine re-scores a pair over a neighbourhood of scales and baselines;
	// with fixed set, the region is framed exactly as in fixed.
	refine := func(ri, wi int, fixed *scored) scored {
		if !fits(ri, wi) {
			return lineScore(ri, wi)
		}
		if fixed != nil {
			if f, ok := score(ri, wi, fixed.obs); ok {
				return f
			}
			return lineScore(ri, wi)
		}
		// Coordinate descent over the geometry: the baseline first at the
		// nominal scale (which the nominal pass already framed), then the
		// scale at the better baseline. Five framings where the full grid
		// took nine, and each framing encodes every component.
		best, found := lineScore(ri, wi), false
		xh0 := nominalXH(ri, wi)
		bestDy := 0
		for _, dy := range []int{0, -1, 1} {
			if f, ok := score(ri, wi, observed(ri, xh0, dy)); ok && (!found || f.dist < best.dist) {
				best, found, bestDy = f, true, dy
			}
		}
		for _, scale := range []float64{0.96, 1.04} {
			if f, ok := score(ri, wi, observed(ri, xh0*scale, bestDy)); ok && (!found || f.dist < best.dist) {
				best, found = f, true
			}
		}
		return best
	}

	// Nominal pass over every plausible pair; the line hash stands in for
	// regions without components.
	const topK = 48
	top := make([]scored, 0, topK+1)
	keep := func(s scored) {
		if len(top) == topK && s.dist >= top[topK-1].dist {
			return
		}
		top = append(top, s)
		sort.Slice(top, func(a, b int) bool { return top[a].dist < top[b].dist })
		if len(top) > topK {
			top = top[:topK]
		}
	}
	for ri := range regions {
		for wi := range words {
			if !fits(ri, wi) {
				if len(regions[ri].comps) == 0 {
					keep(lineScore(ri, wi))
				}
				continue
			}
			if s, ok := score(ri, wi, observed(ri, nominalXH(ri, wi), 0)); ok {
				keep(s)
			}
		}
	}
	if len(top) == 0 {
		v.Status = NotFound
		if !c.Required {
			v.Status = Skipped
		}
		v.Reason = "no_region_fits"
		return v, nil
	}
	all := make([]scored, 0, len(top))
	for _, s := range top {
		all = append(all, refine(s.region, s.word, nil))
	}
	sort.Slice(all, func(a, b int) bool { return all[a].dist < all[b].dist })
	best := all[0]
	if debugScored != nil {
		debugScored(c.Name, regions, all)
	}
	if debugProbe != nil && debugScored != nil {
		runProbe(c.Name, words, regions, func(ri, wi int) scored { return refine(ri, wi, nil) }, fits)
	}
	if digitProbe != nil && best.refined {
		runDigitProbe(c.Name, sp, best)
	}

	// competitor is the nearest candidate of a different value on the
	// pair's region under the pair's geometry.
	competitor := func(p scored) scored {
		comp := scored{region: -1, dist: math.Inf(1)}
		for wi, w := range words {
			if w.Value == p.word_.Value {
				continue
			}
			s, ok := scored{}, false
			if p.refined && fits(p.region, wi) {
				s, ok = score(p.region, wi, p.obs)
			}
			if !ok {
				s = lineScore(p.region, wi)
			}
			if s.dist < comp.dist {
				comp = s
			}
		}
		return comp
	}
	// decisive says whether a pair beats its competitor by the margin.
	// Glyph-wise, the two candidates only differ where their characters
	// differ, so the margin is counted per differing glyph: the tie
	// fraction, or one and a half times the alphabet's own within-character
	// spread when the image is noisier than that, since the spread is the
	// noise of one glyph comparison and a difference under it is not
	// evidence. Line-wise it is the doc's fraction of the code length.
	decisive := func(p, comp scored) bool {
		// A competitor outside the radius could not have been decoded
		// as the value; it is no competitor. Without this, a candidate
		// of a different format, differing in most of its glyphs, would
		// demand a margin no real gap could supply.
		if comp.region < 0 || comp.dist > radius {
			return true
		}
		var margin float64
		if p.refined && comp.refined {
			k := differing([]rune(p.word_.Text), []rune(comp.word_.Text))
			per := math.Max(e.opt.TieMargin, 1.5*sp.A.Spread)
			margin = per * float64(k) / float64(max(1, p.glyphs))
		} else {
			margin = e.opt.LineTieMargin
		}
		return comp.dist-p.dist >= margin
	}
	evidence := func(p, comp scored) *Evidence {
		reg := regions[p.region]
		ev := &Evidence{
			Region: reg.box, Crop: reg.patch.Gray, Codeword: p.word_.Patch,
			Text: p.word_.Text, Params: p.word_.Params,
			D1: p.raw, D2: -1, Radius: int(math.Round(radius * float64(p.bits))), Bits: p.bits,
			Refined: p.refined, Glyphs: p.glyphs, Penalized: p.penalized, Spread: sp.A.Spread,
			LowConfidence: reg.lowConfidence,
		}
		if comp.region >= 0 {
			ev.D2 = comp.raw
			ev.Competitor = comp.word_.Value
			ev.CompetitorText = comp.word_.Text
		}
		return ev
	}

	if best.dist > radius {
		v.Evidence = evidence(best, competitor(best))
		v.Status = NotFound
		if !c.Required {
			v.Status = Skipped
		}
		return v, nil
	}

	// Every region near the best reads as some value: its nearest
	// candidate. The claim is decided when the decisive readings agree;
	// conflicting decisive readings, or none, go to review. A blurred
	// "(90 Proof)" that cannot tell its 0 from a 9 does not override a
	// clear "45%" elsewhere on the label. A reading much farther than the
	// best is not a peer observation of the claim, and a reading of fewer
	// than MinGlyphs glyphs is too little evidence to decide anything: a
	// batch number that happens to read as "1 L" can only ask for review.
	const maxReadings = 8
	peer := math.Min(radius, best.dist+0.02)
	var decided []scored
	var firstTie *scored
	var tieComp scored
	read := map[int]bool{}
	for i := range all {
		p := all[i]
		if p.dist > peer || len(read) >= maxReadings {
			break
		}
		if read[p.region] {
			continue
		}
		read[p.region] = true
		comp := competitor(p)
		if numericTrace != nil {
			numericTrace("%s: reading region %d %v text %q dist %.4f glyphs %d refined %v; competitor %q dist %.4f; decisive %v", c.Name, p.region, regions[p.region].box, p.word_.Text, p.dist, p.glyphs, p.refined, comp.word_.Text, comp.dist, decisive(p, comp))
		}
		if p.refined && p.glyphs >= e.opt.MinGlyphs && decisive(p, comp) {
			if len(decided) == 0 {
				v.Evidence = evidence(p, comp)
			}
			decided = append(decided, p)
			continue
		}
		if firstTie == nil {
			firstTie, tieComp = &p, comp
		}
	}
	if len(decided) == 0 {
		v.Evidence = evidence(*firstTie, tieComp)
		v.Status = Review
		v.Candidates = []string{firstTie.word_.Value}
		if tieComp.region >= 0 {
			v.Candidates = append(v.Candidates, tieComp.word_.Value)
		}
		if firstTie.glyphs < e.opt.MinGlyphs {
			v.Reason = "too_few_glyphs"
		}
		return v, nil
	}
	values := []string{decided[0].word_.Value}
	for _, p := range decided[1:] {
		if p.word_.Value != values[0] {
			values = append(values, p.word_.Value)
		}
	}
	if len(values) > 1 {
		v.Status = Review
		v.Reason = "regions_disagree"
		v.Candidates = values
		return v, nil
	}
	v.Observed = values[0]
	if values[0] == c.Expected {
		v.Status = Verified
	} else {
		v.Status = Mismatch
	}
	// A verdict resting on a character the label should have taught but
	// did not (its samples disagreed) is only a review.
	for _, r := range decided[0].word_.Text {
		if sp.A.Unlearned[r] {
			v.Status = Review
			v.Reason = "char_unlearned:" + string(r)
			v.Candidates = []string{values[0]}
			return v, nil
		}
	}
	return v, &decided[0]
}

// runProbe reports every region overlapping the probe box against the
// probed candidate text.
func runProbe(claim string, words []spell.Codeword, regions []encodedRegion, refine func(ri, wi int) scored, fits func(ri, wi int) bool) {
	wi := -1
	for i, w := range words {
		if w.Text == debugProbe.text {
			wi = i
			break
		}
	}
	if wi < 0 {
		return
	}
	var probed []scored
	for ri, r := range regions {
		if !r.ink.Overlaps(debugProbe.box) {
			continue
		}
		f := refine(ri, wi)
		f.word_.Params.Face = fmt.Sprintf("probe comps=%d fits=%v aspect=%.2f line=%d", len(r.comps), fits(ri, wi), float64(r.ink.Dx())/float64(max(1, r.ink.Dy())), bitcode.Distance(r.code, words[wi].Code))
		probed = append(probed, f)
	}
	debugScored(claim+"/probe", regions, probed)
}

// runDigitProbe reports, for every matched digit of the best pair, the
// observed glyph's distance to all ten synthesized digits.
func runDigitProbe(claim string, sp *spell.Speller, best scored) {
	for _, st := range best.path {
		if st.Kind != alphabet.Match || st.Char >= len(best.target.Text) {
			continue
		}
		r := best.target.Text[st.Char]
		if r < '0' || r > '9' {
			continue
		}
		var line strings.Builder
		fmt.Fprintf(&line, "%s glyph %d as %q:", claim, st.Glyph, r)
		for d := '0'; d <= '9'; d++ {
			t, _, _, err := sp.Target(string(d), false)
			if err != nil {
				continue
			}
			fmt.Fprintf(&line, " %c=%d", d, bitcode.Distance(best.obs.Codes[st.Glyph], t.Codes[0]))
		}
		digitProbe(line.String())
	}
}

// variant is a candidate with the spelling choices applied to it.
type variant struct {
	Text, Value string
	casing      string
	heavy       bool
}

// variants expands a claim's candidates. Enumerations are spelled as given;
// free text (Variants set) also in capitals and title case where the text
// admits them, with straight and curly apostrophes and quotes, and in the
// heavy weight when the alphabet learned one. All variants of a candidate
// share its value.
func variants(c Claim, hasHeavy bool) []variant {
	if !c.Variants {
		out := make([]variant, 0, len(c.Candidates))
		for _, cand := range c.Candidates {
			out = append(out, variant{cand.Text, cand.Value, "as_given", false})
		}
		return out
	}
	var out []variant
	for _, cand := range c.Candidates {
		texts := map[string]string{"as_given": cand.Text}
		if u := strings.ToUpper(cand.Text); u != cand.Text {
			texts["upper"] = u
		}
		if t := titleCase(cand.Text); t != cand.Text && strings.ToLower(cand.Text) != cand.Text {
			texts["title"] = t
		}
		for casing, text := range texts {
			for _, q := range quoteForms(text) {
				for _, heavy := range []bool{false, true} {
					if heavy && !hasHeavy {
						continue
					}
					out = append(out, variant{q, cand.Value, casing, heavy})
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].casing != out[j].casing {
			return out[i].casing < out[j].casing
		}
		if out[i].Text != out[j].Text {
			return out[i].Text < out[j].Text
		}
		return !out[i].heavy && out[j].heavy
	})
	return out
}

// titleCase capitalizes the first letter of every word and lowers the rest.
func titleCase(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		r := []rune(strings.ToLower(w))
		r[0] = unicode.ToUpper(r[0])
		words[i] = string(r)
	}
	return strings.Join(words, " ")
}

// quoteForms returns the text with straight and with curly apostrophes and
// quotes, when it has any.
func quoteForms(s string) []string {
	straight := strings.NewReplacer("’", "'", "“", "\"", "”", "\"").Replace(s)
	curly := strings.NewReplacer("'", "’", "\"", "”").Replace(s)
	if straight == curly {
		return []string{s}
	}
	return []string{straight, curly}
}

// differing counts the glyph positions where two texts differ once spaces
// are dropped, plus the difference in glyph count; it is at least one.
func differing(a, b []rune) int {
	strip := func(r []rune) []rune {
		out := r[:0:0]
		for _, c := range r {
			if c != ' ' {
				out = append(out, c)
			}
		}
		return out
	}
	a, b = strip(a), strip(b)
	n := 0
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
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
