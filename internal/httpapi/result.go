package httpapi

import (
	"image"
	"time"

	"treasury/api"
	"treasury/verify"
)

// resultOf turns an engine result into what the specification promises.
//
// It is a translation with one addition: each verdict's evidence carries
// a PNG of the region it rests on. That is the point of the whole
// response — a reviewer should not have to take the engine's word for
// what a label says when the engine can show them.
func resultOf(res verify.Result, took time.Duration, img image.Image) api.Result {
	id := identity()
	ms := int(took.Milliseconds())
	out := api.Result{
		Engine:    id,
		LatencyMs: ms,
		Stages:    stagesOf(res.Stages),
		Claims:    *verdictsOf(res.Claims, img),
	}
	if v := verdictsOf(res.Reference, img); len(*v) > 0 {
		out.Reference = v
	}
	if v := verdictsOf(res.Emphasis, img); len(*v) > 0 {
		out.Emphasis = v
	}
	if res.Reason != "" {
		r := res.Reason
		out.Reason = &r
	}
	return out
}

func stagesOf(s verify.Stages) *api.Stages {
	ms := func(d time.Duration) *int {
		v := int(d.Milliseconds())
		return &v
	}
	n := func(v int) *int { return &v }
	return &api.Stages{
		Detect: ms(s.Detect), Recognise: ms(s.Recognise), TurnedPass: ms(s.TurnedPass),
		Runs: ms(s.Runs), Decide: ms(s.Decide),
		Boxes: n(s.Boxes), RunCount: n(s.RunCount),
	}
}

func verdictsOf(vs []verify.Verdict, img image.Image) *[]api.Verdict {
	out := make([]api.Verdict, 0, len(vs))
	for _, v := range vs {
		w := api.Verdict{
			Claim:    v.Claim,
			Status:   api.VerdictStatus(v.Status.String()),
			Expected: v.Expected,
		}
		if v.Reason != "" {
			r := v.Reason
			w.Reason = &r
		}
		if v.Observed != "" {
			o := v.Observed
			w.Observed = &o
		}
		if v.Evidence != nil {
			w.Evidence = evidenceOf(v.Evidence, img)
		}
		out = append(out, w)
	}
	return &out
}

func evidenceOf(e *verify.Evidence, img image.Image) *api.Evidence {
	str := func(s string) *string {
		if s == "" {
			return nil
		}
		return &s
	}
	// A number that is present is sent, including zero. Treating zero as
	// absent dropped the distance from a hundred and forty-one of the
	// hundred and seventy-three claims the fifty carry evidence for -
	// every exact match, which is to say every claim whose evidence is
	// strongest. The specification promises a distance on every verdict
	// and a reviewer reading one has no way to tell "they matched
	// exactly" from "the service did not say".
	f64 := func(v float64) *float64 { return &v }
	out := &api.Evidence{
		Read: str(e.Read), Matched: str(e.Matched),
		Distance: f64(e.Distance), Radius: f64(e.Radius),
		Confidence: f64(e.Confidence),
	}
	// The competitor is the one place absence is the truth: where no
	// other value came near, there is no competitor and no distance to
	// it, and sending a zero there would say the opposite - that
	// something else matched perfectly.
	if e.Competitor != "" {
		out.Competitor = str(e.Competitor)
		out.CompetitorDistance = f64(e.CompDist)
	}
	if e.Parts > 0 {
		p := e.Parts
		out.Parts = &p
	}
	if b := e.Region; !b.Empty() {
		x, y, w, h := b.Min.X, b.Min.Y, b.Dx(), b.Dy()
		out.Region = &api.Region{X: &x, Y: &y, W: &w, H: &h}
		if img != nil {
			if crop := cropPNG(img, b); crop != nil {
				out.Crop = &crop
			}
		}
	}
	return out
}
