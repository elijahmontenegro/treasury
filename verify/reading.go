package verify

import (
	"fmt"
	"image"
	"math"
	"os"
	"sort"
	"strings"

	"treasury/internal/alphabet"
	"treasury/internal/bitcode"
	"treasury/internal/digits"
	"treasury/internal/encoder"
	"treasury/internal/spell"
)

// readAll classifies a line's components and returns every run of digit
// classes that stands as a number: bounded by word gaps or the line's ends,
// with at most one other glyph before it in its word (an opening paren) and
// two after it (an attached unit, "750mL"). A digit-looking glyph inside a
// word of letters, the O of FORK or the l of a batch code, is not a number.
// A percent sign only ends a run.
//
// Only whole lines are read. A word run cut from a line at a wide gap can
// begin inside a number, and a reading taken from it is complete by
// construction and wrong: "12%" split at the tabular gap after its 1 gave a
// run "2%" that was read, decided, and named as the value on labels that
// were right.
func (e *Engine) readAll(reg encodedRegion, ri int, sp *spell.Speller, cache *callCache) []reading {
	// A number and its unit are three glyphs at the least; a region of
	// one or two is a fragment, and a rotated warning's glyphs in the
	// upright image are hundreds of them.
	n := len(reg.comps)
	if n < 3 {
		return nil
	}
	xh := regionXHeight(reg.comps)
	if xh < 4 {
		return nil
	}
	// Digits stand at cap height; a component clearly shorter than the
	// line's tall glyphs and clearly taller than a period is a lowercase
	// letter and is not classified. That halves the classifier's work on
	// mixed-case lines, and the network is most of a claim's cost.
	heights := make([]float64, 0, n)
	for _, b := range reg.comps {
		heights = append(heights, float64(b.Dy()))
	}
	sort.Float64s(heights)
	tall := heights[len(heights)*9/10]
	// Without the classifier the digits come from the label: each frame is
	// compared with the alphabet's own samples of the digit characters,
	// which a field the engine could decode by its vocabulary has taught
	// it. Where nothing taught them they are synthesized, as any character
	// the reference lacks is.
	byImage := e.opt.Digits != DigitsClassifier
	var samples map[byte]bitcode.Code
	if byImage {
		samples = map[byte]bitcode.Code{}
		for i := range len(digits.Classes) {
			r := digits.Classes[i]
			if r == '?' {
				continue
			}
			t, _, _, err := sp.Target(string(r), false)
			if err != nil || len(t.Codes) == 0 || t.Codes[0] == nil {
				continue
			}
			samples[r] = t.Codes[0]
		}
		if len(samples) == 0 {
			return nil
		}
	}
	glyphs := make([]glyphClass, n)
	for i, box := range reg.comps {
		if g, ok := cache.classes[box]; ok && !byImage {
			glyphs[i] = g
			continue
		}
		if h := float64(box.Dy()); h < 0.8*tall && h > 0.35*tall {
			glyphs[i] = glyphClass{class: digits.Other, prob: 1, second: -1}
			continue
		}
		if byImage {
			glyphs[i] = readByImage(reg, i, xh, sp, cache, samples)
			continue
		}
		logits := e.digits.Logits(digits.Frame(reg.pre.Gray, box, reg.baselines[i], xh))
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
	isDigit := func(k int) bool { return k >= 0 && k < 10 }
	numeric := func(g glyphClass) bool { return g.class != digits.Other && g.class >= 0 && g.prob >= 0.5 }
	digit := func(k int) bool { return numeric(glyphs[k]) && isDigit(glyphs[k].class) }
	// A glyph too small to classify (under a third of the tall glyphs) is
	// a decimal point or a thousands comma by its shape alone: a period
	// sits on the baseline, a comma hangs below it. The classifier's own
	// reading is used when it has one.
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
	// A word gap is wide against the x-height and against the line's own
	// letter spacing, which a display face may set wide.
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
	// Tabular figures give every digit the same advance, so a narrow 1 is
	// followed by a gap as wide as a word space: 8–10 px where the same
	// lines' spaces were 7–15. Two confident digits across a gap no wider
	// than the wider of them are one number, as are a digit and a digit
	// across a point.
	boundary := make([]bool, n)
	for i := 1; i < n; i++ {
		if gaps[i-1] <= wordGap {
			continue
		}
		wider := math.Max(float64(reg.comps[i-1].Dx()), float64(reg.comps[i].Dx()))
		if digit(i) && gaps[i-1] <= wider {
			if digit(i-1) || (i >= 2 && small(i-1) && digit(i-2) && gaps[i-2] <= wordGap) {
				continue
			}
		}
		boundary[i] = true
	}
	wordStart := make([]int, n) // index of the first glyph of each glyph's word
	for i := range n {
		wordStart[i] = i
		if i > 0 && !boundary[i] {
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
	// A percent sign is often three components, two small circles about
	// a slash; the classifier knows the slash. Small glyphs overlapping a
	// recognized slash's columns are its circles, whatever the classifier
	// made of them: one read as a 9 at 0.50 turned 13% into 39%.
	absorbed := make([]bool, n)
	for k, g := range glyphs {
		if !numeric(g) || digits.Classes[g.class] != '%' {
			continue
		}
		slash := reg.comps[k]
		for _, m := range []int{k - 2, k - 1, k + 1, k + 2} {
			if m < 0 || m >= n || wordStart[m] != wordStart[k] {
				continue
			}
			c := reg.comps[m]
			margin := int(math.Max(2, 0.1*xh)) // a circle may sit just clear of the slash's box
			if float64(c.Dy()) <= 0.6*tall && c.Min.X < slash.Max.X+margin && c.Max.X > slash.Min.X-margin {
				absorbed[m] = true
			}
		}
	}
	type span struct{ start, end int }
	var runs []span
	for i := 0; i < n; {
		if absorbed[i] || !digit(i) {
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
			if small(j) && j+1 < n && wordStart[j+1] == wordStart[i] && digit(j+1) {
				glyphs[j] = glyphClass{class: mark(j), prob: 0.5, second: -1}
				j++
				continue
			}
			break
		}
		// The one glyph allowed before the run must be narrow, a paren,
		// not a fused "787" in front of the "01" of a zip code, and a
		// paren is never digit-shaped: a digit the classifier half-saw
		// is a digit the reading would drop.
		before, after := i-wordStart[i], wordEnd[i]-j
		if before == 1 {
			if g := glyphs[i-1]; float64(reg.comps[i-1].Dx()) > 0.6*xh || (isDigit(g.class) && g.prob >= 0.25) {
				before = 2
			}
		}
		if before <= 1 && after <= 2 {
			runs = append(runs, span{i, j})
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
			gap := ""
			if k > 0 {
				gap = fmt.Sprintf("g%d ", int(gaps[k-1]))
				if boundary[k] {
					gap = fmt.Sprintf("|W%d| ", int(gaps[k-1]))
				}
			}
			parts = append(parts, fmt.Sprintf("%s%c:%.2f(h%d,w%d)", gap, c, g.prob, reg.comps[k].Dy(), reg.comps[k].Dx()))
		}
		numericTrace("region %d %v xh %.1f tall %.0f wordgap %.1f: %s", ri, reg.box, xh, tall, wordGap, strings.Join(parts, " "))
	}
	var out []reading
	for _, sp := range runs {
		r := reading{region: ri, confidence: 1, boxes: []image.Rectangle{reg.comps[sp.start], reg.comps[sp.end-1]}}
		weakest := -1
		var text []byte
		for k := sp.start; k < sp.end; k++ {
			if absorbed[k] {
				continue
			}
			g := glyphs[k]
			text = append(text, digits.Classes[g.class])
			if digits.Classes[g.class] != '%' {
				r.run = append(r.run, reg.comps[k])
			}
			if isDigit(g.class) && g.prob < r.confidence {
				r.confidence = g.prob
				weakest = k
			}
		}
		r.text = string(text)
		// A wide glyph right after the run in its word that the
		// classifier did not recognize is most likely a percent sign it
		// did not know in this face; the percent reading is offered too.
		if k := sp.end; !strings.HasSuffix(r.text, "%") && k < n && wordStart[k] == wordStart[sp.start] && !numeric(glyphs[k]) && float64(reg.comps[k].Dx()) >= 0.8*xh && float64(reg.comps[k].Dy()) >= 0.7*tall {
			r.percent = r.text + "%"
		}
		if weakest >= 0 {
			g := glyphs[weakest]
			if g.second >= 0 && g.second != digits.Other && g.secondProb >= 0.1 {
				pos := 0
				for k := sp.start; k < weakest; k++ {
					if !absorbed[k] {
						pos++
					}
				}
				alt := []byte(r.text)
				alt[pos] = digits.Classes[g.second]
				r.alt = string(alt)
			}
		}
		out = append(out, r)
	}
	return out
}

// readByImage names one component by the nearest of the alphabet's digit
// and punctuation samples, in the code claims are decoded in. The margin to
// the runner-up stands in for the classifier's probability: a glyph that
// two characters explain equally is no reading.
func readByImage(reg encodedRegion, i int, xh float64, sp *spell.Speller, cache *callCache, samples map[byte]bitcode.Code) glyphClass {
	box := reg.comps[i]
	var code bitcode.Code
	if coder := cache.coder(sp.GlyphEnc, reg.pre); coder != nil {
		code = coder(box, reg.baselines[i], xh)
	} else {
		code = sp.GlyphEnc.Encode(alphabet.FrameCrop(reg.pre.Bin, box, reg.baselines[i], xh))
	}
	bits := sp.GlyphEnc.Bits()
	best, second := byte('?'), byte('?')
	bestD, secondD := math.Inf(1), math.Inf(1)
	for r, c := range samples {
		d := encoder.NormalizedDistance(code, c, bits)
		if d < bestD {
			second, secondD = best, bestD
			best, bestD = r, d
		} else if d < secondD {
			second, secondD = r, d
		}
	}
	g := glyphClass{class: digits.ClassOf(rune(best)), second: digits.ClassOf(rune(second))}
	// A distance is not a probability. What matters to the run rules is
	// whether the reading is safe to act on, so the margin over the
	// runner-up, relative to the winner's own distance, stands in for one.
	switch {
	case bestD > 0.35:
		return glyphClass{class: digits.Other, prob: 1, second: -1}
	case secondD <= bestD:
		g.prob, g.secondProb = 0.5, 0.5
	default:
		g.prob = math.Min(0.99, (secondD-bestD)/secondD+0.5)
		g.secondProb = 1 - g.prob
	}
	return g
}
