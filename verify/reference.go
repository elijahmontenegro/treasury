package verify

import (
	"fmt"
	"image"

	"treasury/internal/alphabet"
	"treasury/internal/bitcode"
	"treasury/internal/encoder"
	"treasury/internal/preprocess"
)

// referenceVerdicts judges each row of the reference block by its own
// alignment. Anomalies are glyphs the reference does not account for:
// unexplained insertions and deletions, and matched or split glyphs that
// look more like another character than their own. Unexplained glyphs and
// strong outliers weigh one, weak outliers half. A row with no anomalies
// verifies; a weight under the fail threshold goes to review with the
// glyphs as evidence, since one alignment slip or a blurred letter looks
// the same as one substituted letter; the threshold, a share of glyphs
// contradicting their shape class, or a high mean distance fail the row.
// Altered wording, a missing word, or a title-case header fail on their
// row.
func (e *Engine) referenceVerdicts(a *alphabet.Alphabet) []Verdict {
	out := make([]Verdict, 0, len(a.Rows))
	for i, rq := range a.Rows {
		v := Verdict{Claim: fmt.Sprintf("reference_row_%d", i+1)}
		ev := &Evidence{
			Region: a.Block.Rows[i].Box, Matched: rq.Matched, Unexplained: rq.Unexplained,
			Outliers: rq.Outliers, PriorViolations: rq.PriorViolations, MeanDistance: rq.MeanDistance,
			Anomalies: rq.Anomalies,
		}
		v.Evidence = ev
		weight := float64(rq.Unexplained+rq.StrongOutliers) + 0.5*float64(rq.Outliers-rq.StrongOutliers)
		switch {
		case rq.Matched == 0:
			v.Status = NotFound
		case float64(rq.PriorViolations) > e.opt.ViolationFraction*float64(rq.Matched):
			v.Status, v.Reason = Mismatch, "shape_class"
		case rq.MeanDistance > e.opt.LineThreshold:
			v.Status, v.Reason = Mismatch, "distance"
		case weight >= e.opt.RowFailAnomalies:
			v.Status, v.Reason = Mismatch, "anomalies"
		case weight > 0:
			v.Status, v.Reason = Review, "anomalies"
		default:
			v.Status = Verified
		}
		out = append(out, v)
	}
	return out
}

// emphasisVerdict decides whether an emphasis span is printed heavier than
// the body. Both hypotheses are the span's own glyphs with their stroke
// brought to a target width: the body's for regular, HeavyFactor times the
// body's for heavy. The span is heavy when its code is nearer the heavy
// hypothesis. The stroke-width ratio is reported as a second signal.
func (e *Engine) emphasisVerdict(a *alphabet.Alphabet, pre *preprocess.Result, ref Reference, span int) Verdict {
	runes := []rune(ref.Text)
	s := a.Spans[span]
	v := Verdict{Claim: fmt.Sprintf("emphasis_%d", span+1), Expected: string(runes[s.Start:min(s.End, len(runes))])}
	glyphs := a.SpanGlyphs(span)
	if len(glyphs) < 3 {
		v.Status, v.Reason = NotFound, "span_not_aligned"
		return v
	}
	box := a.Block.Glyphs[glyphs[0]].Box
	for _, g := range glyphs[1:] {
		box = box.Union(a.Block.Glyphs[g].Box)
	}
	crop := pre.Bin.Crop(box)
	body := a.BodyStroke()
	spanStroke := a.SpanStroke(span)
	if body == 0 || spanStroke == 0 {
		v.Status, v.Reason = NotFound, "no_stroke"
		return v
	}
	// Strokes differ by fractions of a pixel, so the hypotheses are built
	// and encoded at four times the resolution, where the coverage grid
	// sees the change.
	const k = 4
	up := crop.Upsample(k)
	enc := encoder.Hash{W: 64, H: 16, Levels: 4}
	code := enc.Encode(encoder.Patch{Bin: up})
	hypothesis := func(target float64) bitcode.Code {
		delta := (crop.StrokeWidth() - target) / 2 * k
		switch {
		case delta > 0.5:
			return enc.Encode(encoder.Patch{Bin: up.Erode(delta)})
		case delta < -0.5:
			return enc.Encode(encoder.Patch{Bin: up.Dilate(-delta)})
		}
		return code
	}
	dRegular := bitcode.Distance(code, hypothesis(body))
	dHeavy := bitcode.Distance(code, hypothesis(e.opt.HeavyFactor*body))
	ev := &Evidence{
		Region: box, Crop: grayCrop(pre, box), D1: dHeavy, D2: dRegular, Bits: enc.Bits(),
		Radius: int(e.opt.EmphasisGate * float64(enc.Bits())), StrokeRatio: spanStroke / body,
	}
	v.Evidence = ev
	if min(dHeavy, dRegular) > ev.Radius {
		v.Status, v.Reason = NotFound, "outside_gate"
		return v
	}
	if dHeavy < dRegular {
		v.Status, v.Observed = Verified, "heavy"
	} else {
		v.Status, v.Observed = Mismatch, "regular"
	}
	return v
}

func grayCrop(pre *preprocess.Result, r image.Rectangle) *image.Gray {
	r = r.Intersect(pre.Gray.Rect)
	out := image.NewGray(image.Rect(0, 0, r.Dx(), r.Dy()))
	for y := range r.Dy() {
		copy(out.Pix[y*out.Stride:y*out.Stride+r.Dx()], pre.Gray.Pix[pre.Gray.PixOffset(r.Min.X, r.Min.Y+y):pre.Gray.PixOffset(r.Max.X, r.Min.Y+y)])
	}
	return out
}
