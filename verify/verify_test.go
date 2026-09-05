package verify_test

import (
	"context"
	"encoding/json"
	"image"
	_ "image/png"
	"os"
	"strings"
	"testing"
	"time"

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

// off is the pipeline as it stood through step 9, for the assertions
// written against it.
func off() *bool { b := false; return &b }

func run(t *testing.T, img image.Image, exp ttb.Expected) map[string]verify.Verdict {
	return runWith(t, verify.Options{}, img, exp)
}

func runWith(t *testing.T, o verify.Options, img image.Image, exp ttb.Expected) map[string]verify.Verdict {
	t.Helper()
	eng, err := verify.New(o)
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
			t.Logf("%-12s %-9s reason=%q observed=%q d1=%d d2=%d radius=%d bits=%d refined=%v glyphs=%d pen=%d text=%q casing=%s weight=%s synth=%q comp=%q",
				v.Claim, v.Status, v.Reason, v.Observed, ev.D1, ev.D2, ev.Radius, ev.Bits, ev.Refined, ev.Glyphs, ev.Penalized, ev.Text, ev.Params.Casing, ev.Params.Weight, ev.Params.Synthesized, ev.Competitor)
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

// TestEnumeratedClaimsAugmented is a clean-corpus assertion: the sample
// label is type on white paper, which is what the engine was built against
// through step 9. It is pinned to the pipeline it was written for, where
// ink is whatever is dark, because separating text from artwork costs this
// label's net contents once blur and JPEG have been through it. The
// shipped default is measured on the corpus that models the population.
func TestEnumeratedClaimsAugmented(t *testing.T) {
	exp := ttb.Sample()
	vs := runWith(t, verify.Options{Separate: off()}, label(t, exp, &synth.Aug{RotateDeg: 7, BlurSigma: 1, JPEGQuality: 50}), exp)
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
	return fullWith(t, verify.Options{}, img, exp)
}

func fullWith(t *testing.T, o verify.Options, img image.Image, exp ttb.Expected) verify.Result {
	t.Helper()
	eng, err := verify.New(o)
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
	for _, v := range res.Claims {
		if v.Evidence != nil {
			t.Logf("%-12s %-9s d1=%d bits=%d text=%q casing=%s weight=%s synth=%q", v.Claim, v.Status, v.Evidence.D1, v.Evidence.Bits, v.Evidence.Text, v.Evidence.Params.Casing, v.Evidence.Params.Weight, v.Evidence.Params.Synthesized)
		} else {
			t.Logf("%-12s %-9s reason=%s", v.Claim, v.Status, v.Reason)
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

func claimMap(res verify.Result) map[string]verify.Verdict {
	out := map[string]verify.Verdict{}
	for _, v := range res.Claims {
		out[v.Claim] = v
	}
	return out
}

// unique is the sample with a brand that appears nowhere else on the label,
// so a brand verdict can only come from the brand line.
func unique() ttb.Expected {
	exp := ttb.Sample()
	exp.Brand = "SILVER FOX RESERVE"
	return exp
}

func variantOf(t *testing.T, exp ttb.Expected, v ttb.Variant) *image.Gray {
	t.Helper()
	faces, err := render.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	img, _, err := synth.Render(ttb.LabelDocumentVariant(exp, v), faces)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// TestFreeTextCasingVariant: the label prints the brand in title case while
// the application states it in capitals; the casing variant carries it.
// TestFreeTextCasingVariant is the other clean-corpus assertion, pinned for
// the same reason: a brand set in a display face in a casing variant is
// found on paper and not through the separation.
func TestFreeTextCasingVariant(t *testing.T) {
	exp := unique()
	res := fullWith(t, verify.Options{Separate: off()}, variantOf(t, exp, ttb.Variant{BrandText: "Silver Fox Reserve"}), exp)
	v := claimMap(res)["brand"]
	if v.Status != verify.Verified || v.Evidence == nil || v.Evidence.Params.Casing != "title" {
		t.Errorf("brand: %s casing=%q", v.Status, v.Evidence.Params.Casing)
	}
}

// TestDisplayFaceBrandNotFound: a brand set in a script face the alphabet
// cannot reach decodes as NOT_FOUND, the documented limit; the body claims
// are unaffected.
func TestDisplayFaceBrandNotFound(t *testing.T) {
	exp := unique()
	res := full(t, variantOf(t, exp, ttb.Variant{BrandFace: "Lobster Regular"}), exp)
	vs := claimMap(res)
	if vs["brand"].Status != verify.NotFound {
		t.Errorf("brand in Lobster: %s", vs["brand"].Status)
	}
	for _, c := range []string{"class", "producer_1", "abv", "net"} {
		if vs[c].Status != verify.Verified {
			t.Errorf("%s: %s", c, vs[c].Status)
		}
	}
}

// TestOtherBodyFace: the whole label in PT Serif. The alphabet must learn
// it, the nearest face must be PT Serif, and every claim must verify.
func TestOtherBodyFace(t *testing.T) {
	res := full(t, variant(t, ttb.Variant{BodyFace: "PTSerif Regular", HeavyFace: "PTSerif Bold"}, nil), ttb.Sample())
	if res.Alphabet == nil || res.Alphabet.Face != "PTSerif Regular" {
		t.Errorf("nearest face: %+v", res.Alphabet.Face)
	}
	for name, v := range claimMap(res) {
		if v.Status != verify.Verified {
			t.Errorf("%s: %s", name, v.Status)
		}
	}
	for i, s := range rowStatuses(res) {
		if s == verify.Mismatch || s == verify.NotFound {
			t.Errorf("row %d: %s", i+1, s)
		}
	}
	if len(res.Emphasis) != 1 || res.Emphasis[0].Status != verify.Verified {
		t.Errorf("emphasis: %+v", res.Emphasis)
	}
}

// TestDumpPairsSerif prints the refined abv pairs on the PT Serif label and
// probes the regions over the true ABV line.
func TestDumpPairsSerif(t *testing.T) {
	verify.SetDebugScored(func(claim string, lines []string) {
		if claim != "abv" && claim != "abv/probe" {
			return
		}
		for _, l := range lines {
			t.Logf("%s: %s", claim, l)
		}
	})
	defer verify.SetDebugScored(nil)
	faces, err := render.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	img, truth, err := synth.Render(ttb.LabelDocumentVariant(ttb.Sample(), ttb.Variant{BodyFace: "PTSerif Regular", HeavyFace: "PTSerif Bold"}), faces)
	if err != nil {
		t.Fatal(err)
	}
	var box image.Rectangle
	for _, g := range truth.Glyphs {
		if g.Claim == "abv" {
			if box.Empty() {
				box = g.Box
			} else {
				box = box.Union(g.Box)
			}
		}
	}
	t.Logf("true abv box %v", box)
	verify.SetProbe(box, "45% Alc./Vol.")
	full(t, img, ttb.Sample())
}

// TestSerifClaims prints claim evidence with competitors on the PT Serif label.
func TestSerifClaims(t *testing.T) {
	run(t, variant(t, ttb.Variant{BodyFace: "PTSerif Regular", HeavyFace: "PTSerif Bold"}, nil), ttb.Sample())
}

// TestOtherBodyFaceTrace prints the numeric readings on the PT Serif label.
func TestOtherBodyFaceTrace(t *testing.T) {
	verify.SetNumericTrace(func(format string, args ...any) { t.Logf(format, args...) })
	defer verify.SetNumericTrace(nil)
	run(t, variant(t, ttb.Variant{BodyFace: "PTSerif Regular", HeavyFace: "PTSerif Bold"}, nil), ttb.Sample())
}

// TestSynthTrace runs one generated label with the numeric trace on:
// SYNTH_LABEL names the label's path without extension.
func TestSynthTrace(t *testing.T) {
	base := os.Getenv("SYNTH_LABEL")
	if base == "" {
		t.Skip("set SYNTH_LABEL=path/to/label (without extension)")
	}
	var exp ttb.Expected
	b, err := os.ReadFile(base + ".json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &exp); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(base + ".png")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	verify.SetNumericTrace(func(format string, args ...any) { t.Logf(format, args...) })
	defer verify.SetNumericTrace(nil)
	run(t, img, exp)
}

// TestLearnedClaims runs the sample label with claims decoded by the
// learned encoder; a timing and behaviour check.
func TestLearnedClaims(t *testing.T) {
	eng, err := verify.New(verify.Options{ClaimEncoder: "learned"})
	if err != nil {
		t.Skip(err)
	}
	exp := ttb.Sample()
	img := label(t, exp, nil)
	refs, claims := ttb.Inputs(exp)
	start := time.Now()
	res, err := eng.Verify(context.Background(), img, refs, claims)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("learned: %s", time.Since(start))
	for _, v := range res.Claims {
		t.Logf("%-12s %-9s observed=%q", v.Claim, v.Status, v.Observed)
	}
}

// TestSynthLearned runs one generated or real label through the learned
// claim encoder, for timing: SYNTH_LABEL names the label's path without
// extension.
func TestSynthLearned(t *testing.T) {
	base := os.Getenv("SYNTH_LABEL")
	if base == "" {
		t.Skip("set SYNTH_LABEL=path/to/label (without extension)")
	}
	verify.SetNumericTrace(func(format string, args ...any) { t.Logf(format, args...) })
	defer verify.SetNumericTrace(nil)
	eng, err := verify.New(verify.Options{ClaimEncoder: "learned"})
	if err != nil {
		t.Skip(err)
	}
	var exp ttb.Expected
	b, err := os.ReadFile(base + ".json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &exp); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(base + ".png")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	refs, claims := ttb.Inputs(exp)
	start := time.Now()
	res, err := eng.Verify(context.Background(), img, refs, claims)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("learned %s: %s orientation %s casing %s", base, time.Since(start), res.Orientation, res.ReferenceCasing)
	for _, v := range res.Claims {
		t.Logf("%-12s %-9s observed=%q", v.Claim, v.Status, v.Observed)
	}
}

// TestDeterministic verifies one label three times and requires the results
// to be identical. Go orders map iteration differently on every range, and
// three sums that ran in map order (face scores, the alphabet's spread, the
// nearest centroid to an anomaly) made the same image give a different
// verdict about one label in five.
func TestDeterministic(t *testing.T) {
	exp := ttb.Sample()
	img := label(t, exp, nil)
	eng, err := verify.New(verify.Options{})
	if err != nil {
		t.Fatal(err)
	}
	refs, claims := ttb.Inputs(exp)
	var first string
	for i := range 3 {
		res, err := eng.Verify(context.Background(), img, refs, claims)
		if err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal(res)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = string(b)
			continue
		}
		if string(b) != first {
			t.Fatalf("run %d differs from run 1", i+1)
		}
	}
}
