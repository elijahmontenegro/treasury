package verify_test

import (
	"context"
	"image"
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
