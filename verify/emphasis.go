package verify

import (
	"image"
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
func (e *Engine) verifyEmphasis(ref Reference, span Span, run *refRun, img image.Image) Verdict {
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
	if read == "" {
		v.Reason = "header_not_read"
		return v
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
		v.Reason = "header_not_read"
		return v
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
