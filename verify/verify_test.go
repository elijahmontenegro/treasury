package verify_test

import (
	"context"
	"image"
	"strings"
	"testing"

	"treasury/internal/render"
	"treasury/internal/synth"
	"treasury/ttb"
	"treasury/verify"
)

func label(t *testing.T, exp ttb.Expected, aug *synth.Aug) *image.Gray {
	t.Helper()
	faces, err := render.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	img, truth, err := synth.Render(ttb.LabelDocument(exp), faces)
	if err != nil {
		t.Fatal(err)
	}
	if aug != nil {
		if img, _, err = synth.Augment(img, truth, *aug); err != nil {
			t.Fatal(err)
		}
	}
	return img
}

func run(t *testing.T, img image.Image, exp ttb.Expected) map[string]verify.Verdict {
	t.Helper()
	eng, err := verify.New(verify.Options{})
	if err != nil {
		t.Fatal(err)
	}
	refs, claims := ttb.Inputs(exp)
	res, err := eng.Verify(context.Background(), img, refs, claims)
	if err != nil {
		t.Fatal(err)
	}
	if res.Reason != "" {
		t.Fatalf("no alphabet: %s", res.Reason)
	}
	out := map[string]verify.Verdict{}
	for _, v := range res.Claims {
		out[v.Claim] = v
		if v.Evidence != nil {
			ev := v.Evidence
			t.Logf("%-12s %-9s observed=%q d1=%d d2=%d radius=%d bits=%d refined=%v glyphs=%d pen=%d text=%q synth=%q comp=%q",
				v.Claim, v.Status, v.Observed, ev.D1, ev.D2, ev.Radius, ev.Bits, ev.Refined, ev.Glyphs, ev.Penalized, ev.Text, ev.Params.Synthesized, ev.Competitor)
		} else {
			t.Logf("%-12s %-9s reason=%s", v.Claim, v.Status, v.Reason)
		}
	}
	return out
}

func expectStatus(t *testing.T, vs map[string]verify.Verdict, claim string, status verify.Status, observed string) {
	t.Helper()
	v, ok := vs[claim]
	if !ok {
		t.Errorf("no verdict for %s", claim)
		return
	}
	if v.Status != status || (observed != "" && v.Observed != observed) {
		t.Errorf("%s: got %s observed %q, want %s observed %q", claim, v.Status, v.Observed, status, observed)
	}
}

func TestEnumeratedClaimsClean(t *testing.T) {
	exp := ttb.Sample()
	vs := run(t, label(t, exp, nil), exp)
	expectStatus(t, vs, "abv", verify.Verified, "45")
	expectStatus(t, vs, "net", verify.Verified, "750")
}

func TestEnumeratedClaimsAugmented(t *testing.T) {
	exp := ttb.Sample()
	vs := run(t, label(t, exp, &synth.Aug{RotateDeg: 7, BlurSigma: 1, JPEGQuality: 50}), exp)
	expectStatus(t, vs, "abv", verify.Verified, "45")
	expectStatus(t, vs, "net", verify.Verified, "750")
}

func TestWrongABVIsMismatch(t *testing.T) {
	printed := ttb.Sample()
	printed.ABV = 40
	expected := ttb.Sample() // the application says 45
	vs := run(t, label(t, printed, nil), expected)
	expectStatus(t, vs, "abv", verify.Mismatch, "40")
	expectStatus(t, vs, "net", verify.Verified, "750")
}

// TestDumpPairs prints the refined pairs for the abv and net claims on the
// clean sample. It only reports; run it with -v when a verdict looks wrong.
func TestDumpPairs(t *testing.T) {
	verify.SetDebugScored(func(claim string, lines []string) {
		if claim != "abv" && claim != "net" {
			return
		}
		for _, l := range lines {
			t.Logf("%s: %s", claim, l)
		}
	})
	defer verify.SetDebugScored(nil)
	exp := ttb.Sample()
	run(t, label(t, exp, nil), exp)
}

func TestDigitProbe(t *testing.T) {
	verify.DigitDistances(func(line string) { t.Log(line) })
	defer verify.DigitDistances(nil)
	exp := ttb.Sample()
	run(t, label(t, exp, nil), exp)
}

// full runs the engine and returns the whole result.
func full(t *testing.T, img image.Image, exp ttb.Expected) verify.Result {
	t.Helper()
	eng, err := verify.New(verify.Options{})
	if err != nil {
		t.Fatal(err)
	}
	refs, claims := ttb.Inputs(exp)
	res, err := eng.Verify(context.Background(), img, refs, claims)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range res.Reference {
		e := v.Evidence
		t.Logf("%-16s %-9s reason=%-12s matched=%d unexplained=%d outliers=%d violations=%d mean=%.3f", v.Claim, v.Status, v.Reason, e.Matched, e.Unexplained, e.Outliers, e.PriorViolations, e.MeanDistance)
		for _, an := range e.Anomalies {
			t.Logf("    %-7s glyph %3d as %q looks like %q distance %.3f at %v", an.Kind, an.Glyph, an.Char, an.Nearest, an.Distance, an.Box)
		}
	}
	for _, v := range res.Emphasis {
		if v.Evidence != nil {
			t.Logf("%-16s %-9s observed=%-8s d_heavy=%d d_regular=%d ratio=%.2f", v.Claim, v.Status, v.Observed, v.Evidence.D1, v.Evidence.D2, v.Evidence.StrokeRatio)
		} else {
			t.Logf("%-16s %-9s reason=%s", v.Claim, v.Status, v.Reason)
		}
	}
	return res
}

func variant(t *testing.T, v ttb.Variant, aug *synth.Aug) *image.Gray {
	t.Helper()
	faces, err := render.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	img, truth, err := synth.Render(ttb.LabelDocumentVariant(ttb.Sample(), v), faces)
	if err != nil {
		t.Fatal(err)
	}
	if aug != nil {
		if img, _, err = synth.Augment(img, truth, *aug); err != nil {
			t.Fatal(err)
		}
	}
	return img
}

func rowStatuses(res verify.Result) []verify.Status {
	out := make([]verify.Status, len(res.Reference))
	for i, v := range res.Reference {
		out[i] = v.Status
	}
	return out
}

func TestReferenceAndEmphasisCompliant(t *testing.T) {
	for name, aug := range map[string]*synth.Aug{"clean": nil, "augmented": {RotateDeg: 7, BlurSigma: 1, JPEGQuality: 50}} {
		t.Run(name, func(t *testing.T) {
			res := full(t, variant(t, ttb.Variant{}, aug), ttb.Sample())
			for i, s := range rowStatuses(res) {
				// A clean render must verify every row; the degraded copy
				// may send a row to review, never fail one.
				if s != verify.Verified && (aug == nil || s != verify.Review) {
					t.Errorf("row %d: %s", i+1, s)
				}
			}
			if len(res.Emphasis) != 1 || res.Emphasis[0].Status != verify.Verified {
				t.Errorf("emphasis: %+v", res.Emphasis)
			} else if res.Emphasis[0].Evidence.StrokeRatio < 1.15 {
				t.Errorf("stroke ratio %.2f, expected the bold header well above the body", res.Emphasis[0].Evidence.StrokeRatio)
			}
		})
	}
}

func TestTitleCaseHeaderFails(t *testing.T) {
	res := full(t, variant(t, ttb.Variant{HeaderTitleCase: true}, nil), ttb.Sample())
	if s := rowStatuses(res); len(s) == 0 || s[0] != verify.Mismatch {
		t.Errorf("row 1 with a title-case header: %v", s)
	}
}

func TestRegularHeaderFails(t *testing.T) {
	res := full(t, variant(t, ttb.Variant{HeaderRegular: true}, nil), ttb.Sample())
	if len(res.Emphasis) != 1 || res.Emphasis[0].Status != verify.Mismatch || res.Emphasis[0].Observed != "regular" {
		t.Errorf("emphasis with a regular header: %+v", res.Emphasis)
	}
}

func TestAlteredWordingFailsOnItsRow(t *testing.T) {
	altered := strings.Replace(ttb.Statute, "and may cause health problems", "and might cause health problems", 1)
	res := full(t, variant(t, ttb.Variant{Wording: altered}, nil), ttb.Sample())
	s := rowStatuses(res)
	if len(s) != 4 {
		t.Fatalf("rows: %v", s)
	}
	for i := 0; i < 3; i++ {
		if s[i] != verify.Verified {
			t.Errorf("row %d should still verify: %s", i+1, s[i])
		}
	}
	if s[3] != verify.Mismatch {
		t.Errorf("row 4 carries the alteration: %s", s[3])
	}
}

func TestNoReferenceGivesNoAlphabet(t *testing.T) {
	res := full(t, variant(t, ttb.Variant{NoWarning: true}, nil), ttb.Sample())
	if res.Reason != "no_alphabet" {
		t.Fatalf("reason = %q", res.Reason)
	}
	for _, v := range res.Claims {
		if v.Status != verify.NotFound || v.Reason != "no_alphabet" {
			t.Errorf("%s: %s %s", v.Claim, v.Status, v.Reason)
		}
	}
}
