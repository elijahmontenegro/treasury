package verify

import (
	"fmt"
	"image"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"

	"treasury/internal/digits"
	"treasury/internal/preprocess"
	"treasury/internal/spell"
)

// NumericFormat is one way a numeric claim is printed: a template with {n}
// where the number goes, and the factor that converts the printed number
// into the claim's unit (0.5 for proof to percent, 1000 for litres to
// millilitres).
type NumericFormat struct {
	Template string
	Scale    float64
}

// Numeric describes a claim that is read as a number rather than decoded
// against an enumeration: the printed formats, the values the field may
// legally take in the claim's unit, and the tolerance within which two
// values are the same. The valid set is a validity check on the reading,
// not a codebook.
type Numeric struct {
	Formats   []NumericFormat
	Valid     []float64 // empty accepts any value
	Tolerance float64
}

// reading is a run of digit-class glyphs in a region as the classifier read
// it: the text, the confidence of its least certain glyph, and the text
// with that glyph's runner-up class instead.
type reading struct {
	region     int
	text       string
	confidence float64
	alt        string // the text with the least certain digit's runner-up class
	percent    string // the text with a percent sign, when a wide unrecognized glyph follows it
}

// glyphClass is the classifier's verdict on one component: its class and
// probability, and the runner-up.
type glyphClass struct {
	class      int
	prob       float64
	second     int
	secondProb float64
}

// callCache holds work shared by the claims of one verification: a
// component is classified once however many word runs contain it.
type callCache struct {
	classes map[image.Rectangle]glyphClass
}

func newCallCache() *callCache { return &callCache{classes: map[image.Rectangle]glyphClass{}} }

// numericTrace, when set, receives the readings and candidates of numeric
// claims; a test hook.
var numericTrace func(format string, args ...any)

// decideClaim decides a claim by its kind.
func (e *Engine) decideClaim(c Claim, sp *spell.Speller, pre *preprocess.Result, regions []encodedRegion, cache *callCache) (Verdict, *scored) {
	if c.Numeric != nil {
		return e.decideNumeric(c, sp, pre, regions, cache)
	}
	return e.decide(c, sp, pre, regions)
}

// decideNumeric reads the claim's number off the label and then verifies
// the reading the way any other claim is verified.
//
// Every region's components are classified as digit classes or other at
// the region's own geometry; the longest run of digit classes with at
// least one digit is the region's reading. Each printed format is
// instantiated with the reading (and with its runner-up, so an uncertain
// digit competes as it would in an enumeration) and the instantiated
// texts are decided as candidates: aligned glyph by glyph against the
// region, with the alphabet's letters for the unit and the synthesized
// digits, the same radius and tie rules, the same evidence. The decided
// value is then judged against the claim: equal within tolerance is
// VERIFIED, a valid other value is MISMATCH naming it, and a value the
// field cannot take is REVIEW.
func (e *Engine) decideNumeric(c Claim, sp *spell.Speller, pre *preprocess.Result, regions []encodedRegion, cache *callCache) (Verdict, *scored) {
	v := Verdict{Claim: c.Name, Expected: c.Expected}
	notFound := func(reason string) (Verdict, *scored) {
		v.Status = NotFound
		if !c.Required {
			v.Status = Skipped
		}
		v.Reason = reason
		return v, nil
	}
	if e.digits == nil {
		v.Status = Review
		v.Reason = "no_digit_model"
		return v, nil
	}
	expected, err := strconv.ParseFloat(c.Expected, 64)
	if err != nil {
		v.Status = Review
		v.Reason = "expected_not_numeric"
		return v, nil
	}
	spec := c.Numeric
	snap := func(val float64) (float64, bool) {
		if len(spec.Valid) == 0 {
			return val, true
		}
		best, ok := val, false
		for _, x := range spec.Valid {
			if math.Abs(x-val) <= spec.Tolerance && (!ok || math.Abs(x-val) < math.Abs(best-val)) {
				best, ok = x, true
			}
		}
		return best, ok
	}
	canon := func(val float64) string { return strconv.FormatFloat(val, 'f', -1, 64) }

	// Read every region on a line short enough to be a field. A number
	// inside running text (the warning block is most of a label's glyphs)
	// is not the claim, and classifying it would cost more than the rest of
	// the verification.
	lineComps := map[int]int{}
	for _, reg := range regions {
		if reg.line >= 0 && len(reg.comps) > lineComps[reg.line] {
			lineComps[reg.line] = len(reg.comps)
		}
	}
	const maxLineComps = 48
	var short []encodedRegion
	for _, reg := range regions {
		if reg.line >= 0 && lineComps[reg.line] > maxLineComps {
			continue
		}
		short = append(short, reg)
	}
	regions = short
	var readings []reading
	for ri := range regions {
		if r, ok := e.read(pre, regions[ri], ri, cache); ok {
			readings = append(readings, r)
		}
	}
	if numericTrace != nil {
		numericTrace("%s: %d components classified so far, %d regions read", c.Name, len(cache.classes), len(regions))
		for _, r := range readings {
			numericTrace("%s: region %d %v read %q (confidence %.2f, alt %q)", c.Name, r.region, regions[r.region].box, r.text, r.confidence, r.alt)
		}
	}
	if len(readings) == 0 {
		return notFound("no_number_read")
	}

	// Instantiate the formats with the readings.
	type inst struct {
		reading reading
		alt     bool
		value   float64
		valid   bool
	}
	instances := map[string]inst{}
	inner := c
	inner.Numeric = nil
	inner.Candidates = nil
	inner.Expected = canon(expected)
	add := func(r reading, text string, alt bool) {
		number, pct := splitPercent(text)
		val, ok := parseNumber(number)
		if !ok {
			return
		}
		for _, f := range spec.Formats {
			if pct != strings.Contains(f.Template, "{n}%") {
				continue
			}
			scale := f.Scale
			if scale == 0 {
				scale = 1
			}
			cand := strings.Replace(f.Template, "{n}", number, 1)
			if _, seen := instances[cand]; seen {
				continue
			}
			value := val * scale
			snapped, valid := snap(value)
			if valid {
				value = snapped
			}
			instances[cand] = inst{reading: r, alt: alt, value: value, valid: valid}
			inner.Candidates = append(inner.Candidates, Candidate{Text: cand, Value: canon(value)})
		}
	}
	for _, r := range readings {
		add(r, r.text, false)
		if r.alt != "" && r.alt != r.text {
			add(r, r.alt, true)
		}
		if r.percent != "" {
			add(r, r.percent, false)
		}
	}
	// A value the field cannot take only stands when nothing valid was
	// read: it then asks for review rather than vanishing, but it must
	// not outvote or contradict a valid reading elsewhere on the label.
	anyValid := false
	for _, in := range instances {
		anyValid = anyValid || in.valid
	}
	if anyValid {
		kept := inner.Candidates[:0]
		for _, cand := range inner.Candidates {
			if instances[cand.Text].valid {
				kept = append(kept, cand)
			}
		}
		inner.Candidates = kept
	}
	if len(inner.Candidates) == 0 {
		return notFound("no_number_parsed")
	}
	sort.Slice(inner.Candidates, func(i, j int) bool { return inner.Candidates[i].Text < inner.Candidates[j].Text })
	// A candidate instantiated from a reading can only be decided on a
	// region that holds a digit run: elsewhere it is a string of letters
	// matching a word by chance, as "1 Litre" once did on a real label at
	// the radius's edge.
	holds := make([]bool, len(regions))
	for _, r := range readings {
		holds[r.region] = true
		rb := regions[r.region].box
		for i := range regions {
			if regions[i].box.Overlaps(rb) {
				holds[i] = true
			}
		}
	}
	var withRuns []encodedRegion
	for i, reg := range regions {
		if holds[i] {
			withRuns = append(withRuns, reg)
		}
	}
	regions = withRuns

	if numericTrace != nil {
		var texts []string
		for _, cand := range inner.Candidates {
			texts = append(texts, cand.Text+"="+cand.Value)
		}
		numericTrace("%s: %d candidates: %s", c.Name, len(texts), strings.Join(texts, " | "))
	}
	out, winner := e.decide(inner, sp, pre, regions)
	out.Claim, out.Expected = c.Name, c.Expected
	if numericTrace != nil && out.Evidence != nil {
		numericTrace("%s: decided %s on region %v text %q d1=%d d2=%d bits=%d competitor %q", c.Name, out.Status, out.Evidence.Region, out.Evidence.Text, out.Evidence.D1, out.Evidence.D2, out.Evidence.Bits, out.Evidence.CompetitorText)
	}
	if out.Evidence != nil {
		if in, ok := instances[out.Evidence.Text]; ok {
			out.Evidence.Reading = in.reading.text
			out.Evidence.DigitConfidence = in.reading.confidence
		}
	}
	// Judge the decided value against the claim in its own unit.
	if out.Status == Verified || out.Status == Mismatch {
		val, _ := strconv.ParseFloat(out.Observed, 64)
		in, known := instances[out.Evidence.Text]
		switch {
		case known && !in.valid:
			out.Status = Review
			out.Reason = "invalid_value"
			out.Candidates = []string{out.Observed}
			return out, nil
		case math.Abs(val-expected) <= spec.Tolerance:
			out.Status = Verified
			out.Observed = c.Expected
		default:
			out.Status = Mismatch
		}
	}
	return out, winner
}

// read classifies a region's components and returns its digit run.
func (e *Engine) read(pre *preprocess.Result, reg encodedRegion, ri int, cache *callCache) (reading, bool) {
	n := len(reg.comps)
	if n == 0 || n > 24 {
		return reading{}, false
	}
	xh := regionXHeight(reg.comps)
	if xh < 4 {
		return reading{}, false
	}
	// Digits stand at cap height; a component clearly shorter than the
	// region's tall glyphs and clearly taller than a period is a
	// lowercase letter and is not classified. That halves the classifier's
	// work on mixed-case lines, and the network is most of a claim's cost.
	heights := make([]float64, 0, n)
	for _, b := range reg.comps {
		heights = append(heights, float64(b.Dy()))
	}
	sort.Float64s(heights)
	tall := heights[len(heights)*9/10]
	glyphs := make([]glyphClass, n)
	for i, box := range reg.comps {
		if g, ok := cache.classes[box]; ok {
			glyphs[i] = g
			continue
		}
		if h := float64(box.Dy()); h < 0.8*tall && h > 0.35*tall {
			glyphs[i] = glyphClass{class: digits.Other, prob: 1, second: -1}
			continue
		}
		logits := e.digits.Logits(digits.Frame(pre.Gray, box, reg.baselines[i], xh))
		g := glyphClass{class: -1, second: -1}
		var sum float64
		best := 0
		for k := range logits {
			if logits[k] > logits[best] {
				best = k
			}
		}
		probs := make([]float64, len(logits))
		for k, l := range logits {
			probs[k] = math.Exp(float64(l - logits[best]))
			sum += probs[k]
		}
		for k := range probs {
			probs[k] /= sum
			if g.class < 0 || probs[k] > g.prob {
				g.second, g.secondProb = g.class, g.prob
				g.class, g.prob = k, probs[k]
			} else if g.second < 0 || probs[k] > g.secondProb {
				g.second, g.secondProb = k, probs[k]
			}
		}
		cache.classes[box] = g
		glyphs[i] = g
	}
	// The longest run of digit classes holding at least one digit, standing
	// as a word of its own: bounded by word gaps or the region's ends, with
	// at most one other glyph before it in its word (an opening paren) and
	// two after it (an attached unit, "750mL"). A digit-looking glyph inside
	// a word of letters, the O of FORK or the l of a batch code, is not a
	// number. A percent sign only ends a run.
	isDigit := func(k int) bool { return k >= 0 && k < 10 }
	numeric := func(g glyphClass) bool { return g.class != digits.Other && g.class >= 0 && g.prob >= 0.5 }
	// A word gap is wide against the x-height and against the region's
	// own letter spacing, which a display face may set wide.
	gaps := make([]float64, 0, n)
	for i := 1; i < n; i++ {
		gaps = append(gaps, float64(reg.comps[i].Min.X-reg.comps[i-1].Max.X))
	}
	wordGap := 0.35 * xh
	if len(gaps) > 0 {
		sorted := append([]float64(nil), gaps...)
		sort.Float64s(sorted)
		wordGap = math.Max(wordGap, 2.2*sorted[len(sorted)/2])
	}
	wordStart := make([]int, n) // index of the first glyph of each glyph's word
	for i := range n {
		wordStart[i] = i
		if i > 0 && gaps[i-1] <= wordGap {
			wordStart[i] = wordStart[i-1]
		}
	}
	wordEnd := make([]int, n) // one past the last glyph of each glyph's word
	for i := n - 1; i >= 0; i-- {
		wordEnd[i] = i + 1
		if i+1 < n && wordStart[i+1] == wordStart[i] {
			wordEnd[i] = wordEnd[i+1]
		}
	}
	// Within a run, a glyph too small to classify (under a third of the
	// tall glyphs) is a decimal point or a thousands comma by its shape
	// alone: a period sits on the baseline, a comma hangs below it. The
	// classifier's own reading is used when it has one.
	small := func(k int) bool { return float64(reg.comps[k].Dy()) <= 0.35*tall }
	mark := func(k int) int {
		g := glyphs[k]
		if p, c := g.prob, g.class; p >= 0.2 && (c == digits.ClassOf('.') || c == digits.ClassOf(',')) {
			return c
		}
		if float64(reg.comps[k].Max.Y-reg.baselines[k]) > 0.12*xh {
			return digits.ClassOf(',')
		}
		return digits.ClassOf('.')
	}
	// A percent sign is often three components, two small circles about
	// a slash; the classifier knows the slash. Small glyphs overlapping a
	// recognized slash's columns are its circles and are absorbed by it.
	absorbed := make([]bool, n)
	for k, g := range glyphs {
		if !numeric(g) || digits.Classes[g.class] != '%' {
			continue
		}
		slash := reg.comps[k]
		for _, m := range []int{k - 2, k - 1, k + 1, k + 2} {
			if m < 0 || m >= n || numeric(glyphs[m]) || wordStart[m] != wordStart[k] {
				continue
			}
			c := reg.comps[m]
			if float64(c.Dy()) <= 0.6*tall && c.Min.X < slash.Max.X && c.Max.X > slash.Min.X {
				absorbed[m] = true
			}
		}
	}
	bestStart, bestEnd := -1, -1
	for i := 0; i < n; {
		if !numeric(glyphs[i]) || !isDigit(glyphs[i].class) {
			i++
			continue
		}
		j := i
		for j < n && wordStart[j] == wordStart[i] {
			if absorbed[j] {
				j++
				continue
			}
			if numeric(glyphs[j]) {
				if digits.Classes[glyphs[j].class] == '%' {
					j++
					for j < n && absorbed[j] {
						j++
					}
					break
				}
				j++
				continue
			}
			// A small unclassified glyph between digits joins the run.
			if small(j) && j+1 < n && wordStart[j+1] == wordStart[i] && numeric(glyphs[j+1]) && isDigit(glyphs[j+1].class) {
				glyphs[j] = glyphClass{class: mark(j), prob: 0.5, second: -1}
				j++
				continue
			}
			break
		}
		// The one glyph allowed before the run must be narrow, a paren,
		// not a fused "787" in front of the "01" of a zip code.
		before, after := i-wordStart[i], wordEnd[i]-j
		if before == 1 && float64(reg.comps[i-1].Dx()) > 0.6*xh {
			before = 2
		}
		if before <= 1 && after <= 2 && j-i > bestEnd-bestStart {
			bestStart, bestEnd = i, j
		}
		i = j
	}
	if numericTrace != nil && os.Getenv("NUMERIC_GLYPHS") != "" {
		var parts []string
		for k, g := range glyphs {
			c := byte('?')
			if g.class >= 0 {
				c = digits.Classes[g.class]
			}
			parts = append(parts, fmt.Sprintf("%c:%.2f(h%d,w%d)", c, g.prob, reg.comps[k].Dy(), reg.comps[k].Dx()))
		}
		numericTrace("region %d %v xh %.1f tall %.0f: %s", ri, reg.box, xh, tall, strings.Join(parts, " "))
	}
	if bestStart < 0 {
		return reading{}, false
	}
	r := reading{region: ri, confidence: 1}
	weakest := -1
	var text []byte
	for k := bestStart; k < bestEnd; k++ {
		if absorbed[k] {
			continue
		}
		g := glyphs[k]
		text = append(text, digits.Classes[g.class])
		if isDigit(g.class) && g.prob < r.confidence {
			r.confidence = g.prob
			weakest = k
		}
	}
	r.text = string(text)
	// A wide glyph right after the run in its word that the classifier
	// did not recognize is most likely a percent sign it did not know in
	// this face; the percent reading is offered too.
	if k := bestEnd; !strings.HasSuffix(r.text, "%") && k < n && wordStart[k] == wordStart[bestStart] && !numeric(glyphs[k]) && float64(reg.comps[k].Dx()) >= 0.8*xh && float64(reg.comps[k].Dy()) >= 0.7*tall {
		r.percent = r.text + "%"
	}
	if weakest >= 0 {
		g := glyphs[weakest]
		if g.second >= 0 && g.second != digits.Other && g.secondProb >= 0.1 {
			pos := 0
			for k := bestStart; k < weakest; k++ {
				if !absorbed[k] {
					pos++
				}
			}
			alt := []byte(r.text)
			alt[pos] = digits.Classes[g.second]
			r.alt = string(alt)
		}
	}
	return r, true
}

// regionXHeight estimates a region's x-height from its components: digits
// and capitals stand about 1.42 x-heights tall, so the median height of
// the taller half of the components, divided by that, is the estimate. The
// classifier was trained with the geometry jittered by more than this
// estimate's error.
func regionXHeight(comps []image.Rectangle) float64 {
	hs := make([]float64, 0, len(comps))
	for _, b := range comps {
		hs = append(hs, float64(b.Dy()))
	}
	sort.Float64s(hs)
	tall := hs[len(hs)/2:]
	return tall[len(tall)/2] / 1.42
}

// splitPercent strips a trailing percent sign from a reading.
func splitPercent(text string) (number string, pct bool) {
	if strings.HasSuffix(text, "%") {
		return strings.TrimSuffix(text, "%"), true
	}
	return text, false
}

// parseNumber reads a printed number: a comma followed by exactly three
// digits at the end groups thousands, any other comma is a decimal mark;
// a leading zero only precedes a decimal point.
func parseNumber(s string) (float64, bool) {
	if s == "" || strings.Count(s, ".") > 1 || strings.Count(s, ",") > 1 {
		return 0, false
	}
	if i := strings.Index(s, ","); i >= 0 {
		if d := strings.Index(s, "."); d >= 0 {
			// Thousands only: exactly three digits between comma and point.
			if d-i-1 != 3 {
				return 0, false
			}
			s = s[:i] + s[i+1:]
		} else if len(s)-i-1 == 3 {
			s = s[:i] + s[i+1:]
		} else {
			s = s[:i] + "." + s[i+1:]
		}
	}
	if strings.HasPrefix(s, ".") || strings.HasSuffix(s, ".") {
		return 0, false
	}
	// A printed number has no leading zero except before a decimal point.
	if len(s) > 1 && s[0] == '0' && s[1] != '.' {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 {
		return 0, false
	}
	return v, true
}
