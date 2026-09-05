package verify

import (
	"fmt"
	"image"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"treasury/internal/alphabet"
	"treasury/internal/bitcode"
	"treasury/internal/encoder"
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
	// Enumerable marks a field whose vocabulary is small enough to decode
	// by spelling every value it may take. The engine uses it only when it
	// cannot read digits: such a field is then the label's second known
	// string, and the glyphs its winner explains teach the alphabet the
	// digits the reference never contained.
	Enumerable bool
}

// reading is a run of digit-class glyphs in a region as the classifier read
// it: the text, the confidence of its least certain glyph, and the text
// with that glyph's runner-up class instead.
type reading struct {
	region     int
	text       string
	confidence float64
	alt        string            // the text with the least certain digit's runner-up class
	percent    string            // the text with a percent sign, when a wide unrecognized glyph follows it
	boxes      []image.Rectangle // the run's glyphs, first and last
	run        []image.Rectangle // the digits and marks the number was read from
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
	codes   map[codeKey]bitcode.Code // learned codes, one per box per image
}

type codeKey struct {
	pre *preprocess.Result
	box image.Rectangle
}

func newCallCache() *callCache {
	return &callCache{classes: map[image.Rectangle]glyphClass{}, codes: map[codeKey]bitcode.Code{}}
}

// coder returns the learned encoder's code supplier for regions of pre:
// one code per box, at the first framing asked for. The encoder was
// trained with the engine's own framing jitter, so the framing's error is
// inside what it ignores, and a component is encoded once rather than at
// every scale and baseline the decoder tries.
func (c *callCache) coder(enc encoder.Encoder, pre *preprocess.Result) func(image.Rectangle, int, float64) bitcode.Code {
	return func(box image.Rectangle, baseline int, xh float64) bitcode.Code {
		k := codeKey{pre, box}
		if code, ok := c.codes[k]; ok {
			return code
		}
		code := enc.Encode(alphabet.Frame(pre.Bin, box, baseline, xh))
		c.codes[k] = code
		return code
	}
}

// numericTrace, when set, receives the readings and candidates of numeric
// claims; a test hook.
var numericTrace func(format string, args ...any)

// decideClaim decides a claim by its kind.
func (e *Engine) decideClaim(c Claim, sp *spell.Speller, pre *preprocess.Result, regions []encodedRegion, cache *callCache) (Verdict, *scored) {
	if c.Numeric == nil {
		return e.decide(c, sp, pre, regions, cache)
	}
	enumerable := c.Numeric.Enumerable && !e.without("fill-enumeration")
	if e.opt.Digits == DigitsClassifier || (!enumerable && e.opt.Digits != DigitsImageEnum) {
		return e.decideNumeric(c, sp, pre, regions, cache)
	}
	// A field the engine cannot read is decoded by its own vocabulary:
	// every value it may legally take, in every printed format, spelled
	// with the learned alphabet and aligned as an ordinary claim. The
	// winner's digit glyphs then teach the alphabet through the harvest,
	// and the fields that could not be read for want of digits are read
	// again in the second pass.
	v, w := e.decide(enumerate(c), sp, pre, regions, cache)
	v.Claim, v.Expected = c.Name, c.Expected
	return v, w
}

// enumerate turns a numeric claim into the enumeration of its field: every
// valid value in every printed format, with the value each stands for.
func enumerate(c Claim) Claim {
	spec := c.Numeric
	out := c
	out.Numeric = nil
	out.numericInner = true
	out.Candidates = nil
	seen := map[string]bool{}
	for _, v := range spec.Valid {
		for _, f := range spec.Formats {
			scale := f.Scale
			if scale == 0 {
				scale = 1
			}
			n := v / scale
			text := strings.Replace(f.Template, "{n}", strconv.FormatFloat(n, 'f', -1, 64), 1)
			if seen[text] {
				continue
			}
			seen[text] = true
			out.Candidates = append(out.Candidates, Candidate{Text: text, Value: strconv.FormatFloat(v, 'f', -1, 64)})
		}
	}
	sort.Slice(out.Candidates, func(i, j int) bool { return out.Candidates[i].Text < out.Candidates[j].Text })
	return out
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
	// One reading per digit run: the same run is read on every word run
	// that holds it, and each copy would be decided again.
	var readings []reading
	seen := map[[2]image.Rectangle]bool{}
	for ri := range regions {
		for _, r := range e.readAll(regions[ri], ri, sp, cache) {
			key := [2]image.Rectangle{r.boxes[0], r.boxes[1]}
			if seen[key] {
				continue
			}
			seen[key] = true
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
	inner.numericInner = true
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
	// Each reading is decided on its own region and the regions
	// overlapping it, with the candidates it instantiated. A reading from
	// another line cannot claim this region: with a code that tolerates
	// the face, "6% alc/vol" from an age statement fitted a "41% alc/vol"
	// line within the radius, and the region's own reading was never
	// asked.
	type outcome struct {
		v     Verdict
		w     *scored
		value float64
		valid bool
		d1    float64
	}
	var outcomes []outcome
	for _, r := range readings {
		var cands []Candidate
		for _, cand := range inner.Candidates {
			if instances[cand.Text].reading.region == r.region {
				cands = append(cands, cand)
			}
		}
		if len(cands) == 0 {
			continue
		}
		// The regions that hold the run's glyphs: the word runs of its
		// line that include its word, not every region its box touches.
		var sub []encodedRegion
		var back []int
		for i, reg := range regions {
			if !reg.box.Overlaps(r.boxes[0]) {
				continue
			}
			holdsAll := true
			for _, b := range r.boxes {
				held := false
				for _, c := range reg.comps {
					if c == b {
						held = true
						break
					}
				}
				if !held {
					holdsAll = false
					break
				}
			}
			if holdsAll {
				sub = append(sub, reg)
				back = append(back, i)
			}
		}
		if len(sub) == 0 {
			continue
		}
		one := inner
		one.Candidates = cands
		if numericTrace != nil {
			var texts []string
			for _, cand := range cands {
				texts = append(texts, cand.Text+"="+cand.Value)
			}
			numericTrace("%s: reading %q on region %d, %d candidates over %d regions: %s", c.Name, r.text, r.region, len(cands), len(sub), strings.Join(texts, " | "))
		}
		out, w := e.decide(one, sp, pre, sub, cache)
		if w != nil {
			w.region = back[w.region]
			if why := numberAligned(w, r.run, sp.GlyphEnc.Bits()); why != "" {
				if numericTrace != nil {
					numericTrace("%s: reading %q not decided on %q: %s", c.Name, r.text, w.target.Text, why)
				}
				out.Status = NotFound
				out.Reason = why
				out.Observed = ""
				w = nil
			}
		}
		// A tie has no winner to check. Between candidates the field can
		// take it is the digit's own uncertainty ("12" against its
		// runner-up "13") and worth a review; between values the field
		// cannot take it is a zip code fitted as "94558 mL" and "94558 L"
		// on a label whose fill was never read, and is nothing.
		if w == nil && out.Status == Review && out.Reason == "" && !e.without("invalid-tie") {
			valid := false
			for _, cand := range cands {
				valid = valid || instances[cand.Text].valid
			}
			if !valid {
				out.Status = NotFound
				out.Reason = "tie_among_invalid"
				out.Candidates = nil
			}
		}
		o := outcome{v: out, w: w, d1: 1}
		if out.Evidence != nil {
			if in, ok := instances[out.Evidence.Text]; ok {
				out.Evidence.Reading = in.reading.text
				out.Evidence.DigitConfidence = in.reading.confidence
				o.value, o.valid = in.value, in.valid
			}
			if out.Evidence.Bits > 0 {
				o.d1 = float64(out.Evidence.D1) / float64(out.Evidence.Bits)
			}
			// Evidence regions index the full region list.
			if out.Evidence.Region == sub[0].box && w == nil {
				out.Evidence.Region = regions[back[0]].box
			}
		}
		if numericTrace != nil && out.Evidence != nil {
			numericTrace("%s: reading %q decided %s text %q d1=%.3f", c.Name, r.text, out.Status, out.Evidence.Text, o.d1)
			if w != nil && os.Getenv("NUMERIC_GLYPHS") != "" {
				numericTrace("%s: path on region %d (refined %v): %s", c.Name, w.region, w.refined, pathTrace(w, sp.GlyphEnc.Bits()))
			}
		}
		o.v = out
		outcomes = append(outcomes, o)
	}
	if len(outcomes) == 0 {
		return notFound("no_number_parsed")
	}
	// Decisive readings must agree; otherwise the best undecided one stands.
	var decided []outcome
	for _, o := range outcomes {
		if o.v.Status == Verified || o.v.Status == Mismatch {
			decided = append(decided, o)
		}
	}
	best := func(os []outcome) outcome {
		b := os[0]
		for _, o := range os[1:] {
			if o.d1 < b.d1 {
				b = o
			}
		}
		return b
	}
	if len(decided) == 0 {
		var reviews []outcome
		for _, o := range outcomes {
			if o.v.Status == Review {
				reviews = append(reviews, o)
			}
		}
		if len(reviews) > 0 {
			o := best(reviews)
			o.v.Claim, o.v.Expected = c.Name, c.Expected
			return o.v, nil
		}
		o := best(outcomes)
		o.v.Claim, o.v.Expected = c.Name, c.Expected
		return o.v, nil
	}
	first := best(decided)
	for _, o := range decided {
		if math.Abs(o.value-first.value) > spec.Tolerance {
			v.Status = Review
			v.Reason = "regions_disagree"
			v.Evidence = first.v.Evidence
			for _, d := range decided {
				v.Candidates = append(v.Candidates, canon(d.value))
			}
			return v, nil
		}
	}
	out := first.v
	out.Claim, out.Expected = c.Name, c.Expected
	switch {
	case !first.valid:
		out.Status = Review
		out.Reason = "invalid_value"
		out.Candidates = []string{canon(first.value)}
		return out, nil
	case math.Abs(first.value-expected) <= spec.Tolerance:
		out.Status = Verified
		out.Observed = c.Expected
	default:
		out.Status = Mismatch
		out.Observed = canon(first.value)
	}
	return out, first.w
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

// radiusFor is the radius a claim is decided at: its own, or the learned
// code's for the claim's kind.
func (e *Engine) radiusFor(c Claim) float64 {
	radius := c.Radius
	if radius == 0 {
		radius = e.opt.DefaultRadius
	}
	if e.claimEnc != nil {
		if c.numericInner {
			if e.opt.LearnedNumericRadius > 0 {
				radius = e.opt.LearnedNumericRadius
			}
		} else if e.opt.LearnedRadius > 0 {
			radius = e.opt.LearnedRadius
		}
	}
	return radius
}

// numberAligned reports why a decided numeric candidate is not a decision
// about the reading, or "" when it is. A reading decides only what it read:
// every character of the number must sit on one glyph of the run and every
// glyph of the run under the number, and every letter of the unit must be
// measured against a glyph and match it. Without that, "3" read from the B
// of "BY" aligned "ALC. 3% BY VOL." to a 49% line within the radius with a
// digit unexplained, a full glyph being a thirteenth of that line; "PROOF
// 12" matched "No. 12" on two perfect digits and five letters consumed by
// structural steps that compared nothing; and "201ml" matched the bare
// "201" of a zip code with its unit deleted.
func numberAligned(w *scored, run []image.Rectangle, bits int) string {
	if !w.refined {
		return ""
	}
	text := w.target.Text
	numStart, numEnd := -1, -1
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c >= '0' && c <= '9' || (numStart >= 0 && (c == '.' || c == ',')) {
			if numStart < 0 {
				numStart = i
			}
			numEnd = i + 1
			continue
		}
		if numStart >= 0 {
			break
		}
	}
	isNumber := func(c int) bool { return c >= numStart && c < numEnd }
	isLetter := func(c int) bool { return c < len(text) && unicode.IsLetter(text[c]) }
	inRun := map[image.Rectangle]bool{}
	for _, b := range run {
		inRun[b] = true
	}
	covered := map[image.Rectangle]bool{}
	for _, st := range w.path {
		var chars []int
		switch st.Kind {
		case alphabet.Match:
			if isNumber(st.Char) {
				if st.Glyph >= len(w.obs.Boxes) || !inRun[w.obs.Boxes[st.Glyph]] {
					return "number_not_on_reading"
				}
				covered[w.obs.Boxes[st.Glyph]] = true
				continue
			}
			if !isLetter(st.Char) || w.target.Codes[st.Char] == nil {
				continue
			}
			d := encoder.NormalizedDistance(w.obs.Codes[st.Glyph], w.target.Codes[st.Char], bits)
			if d > 0.45 {
				return "unit_letter_unmatched:" + string(text[st.Char])
			}
			continue
		case alphabet.Insert:
			if st.Glyph < len(w.obs.Boxes) && inRun[w.obs.Boxes[st.Glyph]] {
				return "number_glyph_unexplained"
			}
			continue
		case alphabet.Delete:
			if isNumber(st.Char) {
				return "number_char_missing"
			}
			if isLetter(st.Char) {
				return "unit_letter_missing:" + string(text[st.Char])
			}
			continue
		case alphabet.Merge, alphabet.Rejoin:
			chars = []int{st.Char, st.Char + 1}
		case alphabet.Merge3:
			chars = []int{st.Char, st.Char + 1, st.Char + 2}
		case alphabet.Split, alphabet.Split3:
			chars = []int{st.Char}
		default:
			continue
		}
		// A digit is one glyph the classifier read, so the number's
		// characters match one each. A structural step on the unit's
		// letters is measured as the decoder measured it, the composed
		// or union code against the other side; a step that compared
		// nothing (a three-way merge under a code without composed
		// triples, on which "PROOF" rode over "No.") leaves the unit
		// unverified.
		for _, c := range chars {
			if isNumber(c) {
				return "number_not_on_reading"
			}
		}
		letter := -1
		for _, c := range chars {
			if isLetter(c) {
				letter = c
				break
			}
		}
		if letter < 0 {
			continue
		}
		var observed, target bitcode.Code
		ok := st.Glyph < len(w.obs.Codes)
		switch st.Kind {
		case alphabet.Merge:
			observed = w.obs.Codes[st.Glyph]
			if w.target.Pair != nil {
				target = w.target.Pair(st.Char)
			}
		case alphabet.Merge3:
			observed = w.obs.Codes[st.Glyph]
			if w.target.Triple != nil {
				target = w.target.Triple(st.Char)
			}
		case alphabet.Rejoin:
			observed, ok = w.obs.UnionCode(st.Glyph, 2)
			if w.target.Pair != nil {
				target = w.target.Pair(st.Char)
			}
		case alphabet.Split:
			observed, ok = w.obs.UnionCode(st.Glyph, 2)
			target = w.target.Codes[st.Char]
		case alphabet.Split3:
			observed, ok = w.obs.UnionCode(st.Glyph, 3)
			target = w.target.Codes[st.Char]
		}
		if !ok || observed == nil || target == nil {
			return "unit_letter_unverified:" + string(text[letter])
		}
		if encoder.NormalizedDistance(observed, target, bits) > 0.45 {
			return "unit_letter_unmatched:" + string(text[letter])
		}
	}
	for _, b := range run {
		if !covered[b] {
			return "number_glyph_unexplained"
		}
	}
	return ""
}

// pathTrace renders a winning alignment step by step, for the trace.
func pathTrace(w *scored, bits int) string {
	var parts []string
	for _, st := range w.path {
		ch := "-"
		if st.Char < len(w.target.Text) {
			ch = string(w.target.Text[st.Char])
		}
		d := ""
		if st.Kind == alphabet.Match && st.Glyph < len(w.obs.Codes) && st.Char < len(w.target.Codes) && w.target.Codes[st.Char] != nil {
			d = fmt.Sprintf("=%.2f", encoder.NormalizedDistance(w.obs.Codes[st.Glyph], w.target.Codes[st.Char], bits))
		}
		box := ""
		if st.Kind != alphabet.Delete && st.Glyph < len(w.obs.Boxes) {
			box = fmt.Sprintf("@%d", w.obs.Boxes[st.Glyph].Min.X)
		}
		parts = append(parts, fmt.Sprintf("%s:%s%s%s", st.Kind, ch, box, d))
	}
	return strings.Join(parts, " ")
}
