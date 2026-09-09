package verify

import (
	"image"
	"sort"
	"strings"
	"unicode"
)

// The header of the statutory warning must be printed in capitals and
// distinct from the rest of it — on every label this build was measured
// on, in a heavier weight. Step 30a verifies both, and the two are
// answered from different evidence.
//
// CAPITALS ARE IN THE TEXT. The recogniser returns the case it read, so
// the question needs no pixels and no separate detection: the header's
// characters are found inside the reference's own match and their case
// is read off. That matters, because on four labels in five the
// detector returns the header and the first words of the body as one
// box, and an earlier version of this step asked for a separate
// detection and so answered nothing on those.
//
// WEIGHT IS NOT IN THE TEXT. It is measured from the pixels, and the
// measurement is a RATIO between the header and the body of the same
// warning on the same label. That is the only form the question can
// honestly take: a stroke five pixels wide is heavy on a small label and
// hairline on a large one, and the same face at the same weight measures
// differently through a photograph, a scan and a JPEG. What does not
// vary is that a bold header is thicker than the body beside it, set at
// the same size, in the same ink, in the same photograph.

// emphasisContrast is how much thicker the header's strokes must be than
// the body's for the header to be called heavier. Fitted at step 30a on
// half A of the corpus, which prints a body-weight header on purpose.
const emphasisContrast = 1.15

// verifyEmphasis judges one span of a reference: that it is printed in
// capitals, and that its strokes are heavier than the body's.
func (e *Engine) verifyEmphasis(ref Reference, span Span, run *refRun, kept []Region, img image.Image) Verdict {
	rs := []rune(ref.Text)
	if span.Start < 0 || span.End > len(rs) || span.Start >= span.End {
		return Verdict{Status: Skipped, Reason: "no_span"}
	}
	want := string(rs[span.Start:span.End])
	v := Verdict{Status: NotFound, Reason: "no_reference", Expected: want}
	if run == nil || len(run.members) == 0 {
		return v
	}
	// The header is the front of the reference, so it is the front of
	// the match: as many characters of the reading as the header has,
	// counted after normalisation and taken back to the text as printed.
	n := len(normalize(want))
	from := run.from
	to := min(from+n, len(run.norm))
	read := run.quote(from, to)
	// The header may not be inside the reference's own chain at all: a
	// detector often returns "GOVERNMENT" and "WARNING:" as two boxes,
	// set apart across the top of the block, and a chain built along the
	// body's own lines does not pick them up. They are fixed words, so
	// they can be looked for directly - which is what step 30a's fix
	// asks for, and what it needed: without it the heading of a warning
	// whose body was found reported as unread.
	if head := findHeader(want, kept, run, e.opt.confusionHalfCost()); head != nil {
		return e.judgeHeader(v, want, head, img)
	}
	if read == "" {
		return notRead(v, run)
	}
	v.Observed = read
	// The span is taken from where the reference's own match puts the
	// statute's opening. Where the header itself was not read, that
	// lands on whatever text was nearest, and judging its case judges a
	// different piece of the label: it called two of the fifty's
	// warnings title-case on readings of "1) According to the wo" and
	// "According Surgeon C". So the span has to look like the header
	// before anything is said about it, case aside.
	if infix(normalize(want), normalize(read), e.opt.confusionHalfCost()) > 0.35 {
		return notRead(v, run)
	}
	if !isUpper(read) {
		v.Status, v.Reason = Mismatch, "header_not_capitals"
		v.Evidence = &Evidence{Region: boxOf(run.members), Read: read, Matched: want}
		return v
	}
	ratio, ok := e.headerContrast(run, n, img)
	if !ok {
		// Capitals were confirmed and weight could not be measured, so
		// the verdict is a review rather than a pass or a failure: the
		// engine says what it could not see rather than assuming it.
		v.Status, v.Reason = Review, "weight_not_measurable"
		v.Evidence = &Evidence{Region: boxOf(run.members), Read: read, Matched: want}
		return v
	}
	v.Evidence = &Evidence{
		Region: boxOf(run.members), Read: read, Matched: want,
		Distance: ratio, Radius: emphasisContrast,
	}
	if ratio >= emphasisContrast {
		v.Status, v.Reason = Verified, ""
	} else {
		// Not a mismatch, and step 30a measured why. On the fifty -
		// approved labels, whose headers the regulation requires to be
		// bold - the ratio runs from 0.69 to 1.79, and seventeen sit
		// under the threshold. Either those labels' headers are not
		// bold, which the approvals say they are, or the measure cannot
		// tell bold from regular through a photograph at warning type,
		// which is eight to fourteen pixels tall. The second is far more
		// likely and the engine may not assert on the strength of the
		// first. So a header that does not measure heavier is reported
		// with its ratio for a person to look at, and is not called a
		// violation.
		v.Status, v.Reason = Review, "header_weight_uncertain"
	}
	return v
}

// notRead is the verdict where the body of the statute was found and the
// header was not read from it.
//
// It is a REVIEW and never a NOT_FOUND, and the difference is the whole
// point: NOT_FOUND asserts that the label does not carry the heading,
// and a failed read beside a found body establishes no such thing. The
// statute is there; something was printed at the front of it; the reader
// could not make it out. That is a thing for a person to look at, not a
// finding against the label.
func notRead(v Verdict, run *refRun) Verdict {
	v.Status, v.Reason = Review, "heading not read"
	if run != nil && len(run.members) > 0 {
		v.Evidence = &Evidence{Region: boxOf(run.members), Matched: v.Expected}
	}
	return v
}

// headerContrast is how much thicker the header's strokes are than the
// body's, on this label.
//
// Where the header has detections of its own, the two sides are those
// detections and the rest. Where the detector returned the header and
// the first words of the body as one box - four labels in five - the box
// is cut at the header's share of the line's characters. That cut is an
// approximation and it errs in the safe direction: a bold header takes
// more width than its character count suggests, so the cut leaves some
// body text in the header's sample and makes the header measure lighter
// than it is. It can therefore refuse a compliant label; it cannot pass
// a label whose header is not heavier.
func (e *Engine) headerContrast(run *refRun, n int, img image.Image) (float64, bool) {
	if img == nil {
		return 0, false
	}
	var head, body []image.Rectangle
	got := 0
	for _, m := range run.members {
		l := len(normalize(m.Text))
		switch {
		case got >= n:
			body = append(body, m.Box)
		case got+l <= n+2:
			head = append(head, m.Box)
		default:
			// This detection holds the end of the header and the start
			// of the body. Cut it where the header's characters end.
			share := float64(n-got) / float64(l)
			w := m.Box.Dx()
			cut := m.Box.Min.X + int(float64(w)*share)
			if cut <= m.Box.Min.X+2 || cut >= m.Box.Max.X-2 {
				return 0, false
			}
			head = append(head, image.Rect(m.Box.Min.X, m.Box.Min.Y, cut, m.Box.Max.Y))
			body = append(body, image.Rect(cut, m.Box.Min.Y, m.Box.Max.X, m.Box.Max.Y))
		}
		got += l
	}
	hs, hok := strokeWidth(img, head)
	bs, bok := strokeWidth(img, body)
	if !hok || !bok || bs <= 0 {
		return 0, false
	}
	return hs / bs, true
}

func boxOf(rs []Region) image.Rectangle {
	if len(rs) == 0 {
		return image.Rectangle{}
	}
	b := rs[0].Box
	for _, r := range rs[1:] {
		b = b.Union(r.Box)
	}
	return b
}

// isUpper says the text carries letters and none of them is lower case.
// A header read as "Government Warning:" fails here, which is the
// title-case error the corpus prints on purpose.
func isUpper(s string) bool {
	letters := false
	for _, r := range s {
		if unicode.IsLetter(r) {
			letters = true
			if unicode.IsLower(r) {
				return false
			}
		}
	}
	return letters
}

// strokeWidth estimates how thick the ink is over some boxes, in units of
// the text's own height so that a header set larger is not called heavier
// for its size alone.
//
// The estimate is twice the ink's area over its perimeter. A stroke of
// width w and length L has an area of about wL and a perimeter of about
// 2L, so twice one over the other is w. It is the classic estimate and
// it is used here because it needs no distance transform and no
// binarisation of the whole page - the machinery step 19a retired - only
// a threshold inside the crop, where the text and its own background are
// the only things present. An earlier version of this step took the mean
// length of a horizontal run of ink instead, which the crossbars of E, T
// and the statute's many capitals dragged upwards, and it measured
// compliant headers as LIGHTER than their bodies on half the corpus.
func strokeWidth(img image.Image, boxes []image.Rectangle) (float64, bool) {
	var area, perim, height float64
	for _, b := range boxes {
		a, p, h := inkOf(img, b)
		area += a
		perim += p
		height += h
	}
	if perim == 0 || height == 0 || area == 0 {
		return 0, false
	}
	return (2 * area / perim) / (height / float64(len(boxes))), true
}

// inkOf thresholds a crop and measures the ink's area and perimeter.
func inkOf(img image.Image, box image.Rectangle) (area, perim, height float64) {
	b := box.Intersect(img.Bounds())
	if b.Empty() {
		return 0, 0, 0
	}
	w, h := b.Dx(), b.Dy()
	if w < 4 || h < 4 {
		return 0, 0, 0
	}
	grey := make([]uint8, w*h)
	var sum int
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, bl, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			v := uint8((299*int(r>>8) + 587*int(g>>8) + 114*int(bl>>8)) / 1000)
			grey[y*w+x] = v
			sum += int(v)
		}
	}
	mean := sum / (w * h)
	// Ink is whichever side of the mean is the minority, so light text on
	// a dark panel is measured as readily as dark on light: a third of a
	// real label's marks are light on dark (step 10b).
	var below int
	for _, v := range grey {
		if int(v) < mean {
			below++
		}
	}
	inkBelow := below*2 <= w*h
	ink := make([]bool, w*h)
	for i, v := range grey {
		if (int(v) < mean) == inkBelow {
			ink[i] = true
			area++
		}
	}
	// Perimeter: an ink pixel with a neighbour that is not ink, counting
	// the edge of the crop as not ink.
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if !ink[y*w+x] {
				continue
			}
			if x == 0 || x == w-1 || y == 0 || y == h-1 ||
				!ink[y*w+x-1] || !ink[y*w+x+1] || !ink[(y-1)*w+x] || !ink[(y+1)*w+x] {
				perim++
			}
		}
	}
	return area, perim, float64(h)
}

// headerFound is where the header's words were read, when they were read
// as detections of their own rather than as part of the body's chain.
type headerFound struct {
	boxes []image.Rectangle
	body  []image.Rectangle // the statute's own lines, for the ratio
	read  string            // as printed, spaces and case intact
}

// findHeader looks for the header's words as separate detections above
// the body of the statute.
//
// GOVERNMENT and WARNING are fixed words - the regulation prescribes
// them - so they are searched for by name rather than reconstructed from
// an alignment. They are joined for the capitals test WHATEVER the
// horizontal gap between them, because a header set across the top of a
// panel may be spaced right out and the gap says nothing about whether
// it is one heading. What does say so is the band: both must sit above
// the body's first line and within a few of its heights of it, so a
// stray "WARNING" elsewhere on the label is not taken for this one.
func findHeader(want string, kept []Region, run *refRun, cc int) *headerFound {
	if run == nil || len(run.members) == 0 || len(kept) == 0 {
		return nil
	}
	body := run.members[0].Box
	for _, m := range run.members {
		if m.Box.Min.Y < body.Min.Y {
			body = m.Box
		}
	}
	// The band above the body's first line: as far up as a few lines of
	// it, and no further.
	h := body.Dy()
	if h <= 0 {
		return nil
	}
	top := body.Min.Y - 6*h
	var found []Region
	for _, word := range headerWords(want) {
		best, bestD := -1, 0.34
		for i, r := range kept {
			b := r.Box
			// Above the body's first line, in the band, and not the body
			// line itself.
			if b.Min.Y >= body.Max.Y || b.Max.Y < top {
				continue
			}
			if b.Min.Y >= body.Min.Y && b.Max.Y <= body.Max.Y && b.Min.X >= body.Min.X {
				continue
			}
			if d := distance(word, normalize(r.Text), cc); d < bestD {
				best, bestD = i, d
			}
		}
		if best < 0 {
			return nil
		}
		found = append(found, kept[best])
	}
	if len(found) == 0 {
		return nil
	}
	// In printed order, which for a heading on one line is left to right.
	sort.SliceStable(found, func(a, b int) bool {
		if found[a].Box.Min.Y != found[b].Box.Min.Y {
			return found[a].Box.Min.Y < found[b].Box.Min.Y
		}
		return found[a].Box.Min.X < found[b].Box.Min.X
	})
	out := &headerFound{}
	for _, m := range run.members {
		out.body = append(out.body, m.Box)
	}
	var parts []string
	for _, r := range found {
		out.boxes = append(out.boxes, r.Box)
		parts = append(parts, strings.TrimSpace(r.Text))
	}
	out.read = strings.Join(parts, " ")
	return out
}

// headerWords is the fixed words of the header, normalized. The colon and
// any spacing are not among them: what the regulation fixes is the words.
func headerWords(want string) []string {
	var out []string
	for _, w := range strings.Fields(want) {
		if n := normalize(w); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// judgeHeader decides on a header read as detections of its own.
func (e *Engine) judgeHeader(v Verdict, want string, head *headerFound, img image.Image) Verdict {
	v.Observed = head.read
	if !isUpper(head.read) {
		v.Status, v.Reason = Mismatch, "header_not_capitals"
		v.Evidence = &Evidence{Region: boxUnion(head.boxes), Read: head.read, Matched: want}
		return v
	}
	// Both boxes carry the header, so both are measured for the weight.
	v.Evidence = &Evidence{Region: boxUnion(head.boxes), Read: head.read, Matched: want}
	if img == nil {
		v.Status, v.Reason = Review, "weight_not_measurable"
		return v
	}
	hs, hok := strokeWidth(img, head.boxes)
	bs, bok := strokeWidth(img, e.bodyBoxes(head))
	if !hok || !bok || bs <= 0 {
		v.Status, v.Reason = Review, "weight_not_measurable"
		return v
	}
	ratio := hs / bs
	v.Evidence.Distance, v.Evidence.Radius = ratio, emphasisContrast
	if ratio >= emphasisContrast {
		v.Status, v.Reason = Verified, ""
	} else {
		v.Status, v.Reason = Review, "header_weight_uncertain"
	}
	return v
}

// bodyBoxes is what the header is compared against: the lines of the
// statute itself, which the caller stashed on the engine's behalf.
func (e *Engine) bodyBoxes(head *headerFound) []image.Rectangle { return head.body }

// boxUnion is the rectangle covering them all.
func boxUnion(bs []image.Rectangle) image.Rectangle {
	if len(bs) == 0 {
		return image.Rectangle{}
	}
	out := bs[0]
	for _, b := range bs[1:] {
		out = out.Union(b)
	}
	return out
}
