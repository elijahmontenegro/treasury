package verify

import (
	"image"
	"strings"
	"testing"
)

// The statute of 27 CFR 16.21, which ttb.Statute carries and this file
// keeps a copy of so the test does not depend on the domain package.
const statute = "GOVERNMENT WARNING: (1) According to the Surgeon General, women " +
	"should not drink alcoholic beverages during pregnancy because of the risk " +
	"of birth defects. (2) Consumption of alcoholic beverages impairs your " +
	"ability to drive a car or operate machinery, and may cause health problems."

// linesOf turns text into detections stacked one under another, the way a
// warning block comes back from the detector, so a reference can be
// judged without an image.
func linesOf(text string, per int) []Region {
	var out []Region
	words := strings.Fields(text)
	y := 0
	for i := 0; i < len(words); i += per {
		j := min(i+per, len(words))
		line := strings.Join(words[i:j], " ")
		out = append(out, Region{
			Box:        image.Rect(10, y, 10+8*len(line), y+12),
			Text:       line,
			Confidence: 0.9,
		})
		y += 13
	}
	return out
}

// TestTheWarningIsVerifiedOnlyWhenItIsExact pins what step 30a decided
// after measuring that no radius separates a damaged reading of the true
// statute from a clean reading of an altered one: the corpus alters
// "should not drink" to "should never drink", four characters in
// 241, which measures 0.026, and the recogniser's own damage on the
// fifty's compliant warnings measures up to 0.034.
func TestTheWarningIsVerifiedOnlyWhenItIsExact(t *testing.T) {
	e := &Engine{opt: Options{}.withDefaults()}
	ref := Reference{Text: statute}
	cases := []struct {
		name string
		read string
		want Status
	}{
		{"the statute as printed", statute, Verified},
		{"punctuation and case set aside", strings.ToLower(statute), Verified},
		// The corpus's own alteration, read perfectly. It is not
		// verified, which is what the gate asks; the engine does not go
		// on to assert that the wording differs, because it cannot tell
		// this from a warning it read badly.
		{"altered wording, read cleanly",
			strings.Replace(statute, "should not drink", "should never drink", 1), Review},
		// A fragment says nothing about the wording either way.
		{"a fragment", "of birth defects. (2) Consumption of", NotFound},
	}
	for _, c := range cases {
		v, _ := e.verifyReferenceText(ref, linesOf(c.read, 7))
		if v.Status != c.want {
			t.Errorf("%s: %s (%s), want %s", c.name, v.Status, v.Reason, c.want)
		}
	}
}

// TestATitleCaseHeaderIsRefused is the case the gate names and the
// corpus cannot supply: it prints a title-case header on purpose, and
// the reader does not read its warning at all on 180 of 198 labels, so
// the refusal has to be tested where it can be seen.
//
// Capitals are read from the text, so this needs no image.
func TestATitleCaseHeaderIsRefused(t *testing.T) {
	e := &Engine{opt: Options{}.withDefaults()}
	ref := Reference{Text: statute, Emphasis: []Span{{Start: 0, End: 19}}}
	cases := []struct {
		name, read string
		want       Status
		reason     string
	}{
		{"capitals", statute, Review, "weight_not_measurable"},
		{"title case", "Government Warning:" + statute[19:], Mismatch, "header_not_capitals"},
		{"lower case", "government warning:" + statute[19:], Mismatch, "header_not_capitals"},
	}
	for _, c := range cases {
		_, run := e.verifyReferenceText(ref, linesOf(c.read, 7))
		if run == nil {
			t.Errorf("%s: no reference found", c.name)
			continue
		}
		// No image, so weight cannot be measured: what is under test is
		// the case, and a header whose case is right gets as far as
		// saying the weight could not be seen.
		v := e.verifyEmphasis(ref, ref.Emphasis[0], run, nil)
		if v.Status != c.want || v.Reason != c.reason {
			t.Errorf("%s: %s/%s, want %s/%s", c.name, v.Status, v.Reason, c.want, c.reason)
		}
	}
}

// TestAWarningNeverReadIsNotAWarningAltered pins the first defect step
// 30a found in its own work: the engine called a six-character fragment
// a mismatch on the statute, which asserts something about a label whose
// warning was never read.
func TestAWarningNeverReadIsNotAWarningAltered(t *testing.T) {
	e := &Engine{opt: Options{}.withDefaults()}
	ref := Reference{Text: statute}
	for _, read := range []string{"OF BIRTH", "KEPREI", "PROOF 750 ML"} {
		v, _ := e.verifyReferenceText(ref, linesOf(read, 7))
		if v.Status == Mismatch {
			t.Errorf("%q was called a mismatch on the statute", read)
		}
	}
}
