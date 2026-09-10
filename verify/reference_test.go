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
		v := e.verifyEmphasis(ref, ref.Emphasis[0], run, linesOf(c.read, 7), nil)
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
	// The verdict is named, not merely ruled out. Asking only that a
	// fragment is not a MISMATCH let the coverage rule be deleted
	// altogether and still pass, because what a fragment falls through to
	// then is REVIEW, which is not a mismatch either - checked, it did.
	//
	// REVIEW and NOT_FOUND are different answers and step 30a is what the
	// difference is: REVIEW says the warning was read and not exactly,
	// NOT_FOUND says it was not read. The fifty's table divides on it -
	// thirteen exact, twenty-six read imperfectly, eleven not read - so a
	// test that treats them as interchangeable is not testing the rule
	// the table rests on.
	// Both routes to absence are named, because they are different
	// findings and the strict form of this test is what showed there were
	// two: a fragment that aligns somewhere in the statute is found and
	// covers too little of it, and one that aligns nowhere is not found at
	// all. Either is NOT_FOUND, and neither is REVIEW.
	for _, c := range []struct{ read, reason string }{
		{"OF BIRTH", "warning_not_read"}, // aligns; coverage 0.030
		{"KEPREI", "warning_not_read"},   // aligns; coverage 0.026
		{"PROOF 750 ML", "not_found"},    // aligns nowhere at all
	} {
		v, _ := e.verifyReferenceText(ref, linesOf(c.read, 7))
		if v.Status != NotFound || v.Reason != c.reason {
			t.Errorf("%q gave %s/%q, want NOT_FOUND/%s: "+
				"too little of the statute was read to say anything about it",
				c.read, v.Status, v.Reason, c.reason)
		}
	}
	// And the other side of the same rule, so it is not simply refusing
	// everything: a statute read whole but imperfectly is REVIEW, which
	// is the twenty-six.
	damaged := strings.Replace(statute, "GOVERNMENT", "COVERNMENT", 1)
	v, _ := e.verifyReferenceText(ref, linesOf(damaged, 7))
	if v.Status != Review {
		t.Errorf("a statute read whole with one character wrong gave %s/%q, want REVIEW",
			v.Status, v.Reason)
	}
}

// headerApart puts GOVERNMENT and WARNING: in two detections of their
// own, spaced right across the top of the panel, with the body of the
// statute stacked beneath. It is the shape a detector returns on most
// real labels, and the shape the header search in emphasis.go exists for.
func headerApart(head1, head2, body string) []Region {
	out := []Region{
		{Box: image.Rect(10, 0, 130, 16), Text: head1, Confidence: 0.9},
		// Far to the right: the gap between the two words says nothing
		// about whether they are one heading, which is why the search
		// joins them whatever it is.
		{Box: image.Rect(420, 0, 520, 16), Text: head2, Confidence: 0.9},
	}
	for _, r := range linesOf(body, 7) {
		b := r.Box
		out = append(out, Region{
			Box:        image.Rect(b.Min.X, b.Min.Y+30, b.Max.X, b.Max.Y+30),
			Text:       r.Text,
			Confidence: r.Confidence,
		})
	}
	return out
}

// TestTheHeaderIsFoundAsSeparateWords covers the second of step 30a's two
// fixes: GOVERNMENT and WARNING are fixed words, so they are looked for
// by name in the band above the body rather than reconstructed from an
// alignment that never reaches them.
func TestTheHeaderIsFoundAsSeparateWords(t *testing.T) {
	e := &Engine{opt: Options{}.withDefaults()}
	ref := Reference{Text: statute, Emphasis: []Span{{Start: 0, End: 19}}}
	body := statute[19:]
	cases := []struct {
		name, one, two string
		want           Status
		reason         string
	}{
		// Capitals, spaced apart: found, and the weight is what is left
		// to say, which needs an image.
		{"apart and in capitals", "GOVERNMENT", "WARNING:", Review, "weight_not_measurable"},
		// The recogniser's own damage does not stop them being found.
		{"apart and damaged", "COVERNMENT", "WARNINC:", Review, "weight_not_measurable"},
		// Title case is still refused, and this is the case that matters:
		// before the fix the header was not found at all here, so nothing
		// was said about it.
		{"apart and title case", "Government", "Warning:", Mismatch, "header_not_capitals"},
	}
	for _, c := range cases {
		regions := headerApart(c.one, c.two, body)
		_, run := e.verifyReferenceText(ref, regions)
		if run == nil {
			t.Errorf("%s: the body of the statute was not found", c.name)
			continue
		}
		v := e.verifyEmphasis(ref, ref.Emphasis[0], run, regions, nil)
		if v.Status != c.want || v.Reason != c.reason {
			t.Errorf("%s: %s/%s, want %s/%s", c.name, v.Status, v.Reason, c.want, c.reason)
		}
		// What is reported is what was printed, spaces and case intact,
		// so the page can show a reviewer the words rather than a
		// normalized run of letters.
		if v.Observed != c.one+" "+c.two {
			t.Errorf("%s: reported %q, want %q", c.name, v.Observed, c.one+" "+c.two)
		}
	}
}

// TestAHeadingNotReadIsNeverAbsent covers the first of step 30a's two
// fixes. Where the body of the statute is found and the heading is not
// read, the verdict is a review: NOT_FOUND asserts that the label does
// not carry the heading, and a failed read beside a found body
// establishes no such thing.
func TestAHeadingNotReadIsNeverAbsent(t *testing.T) {
	e := &Engine{opt: Options{}.withDefaults()}
	ref := Reference{Text: statute, Emphasis: []Span{{Start: 0, End: 19}}}
	// The body, with nothing above it that reads as the heading.
	regions := linesOf(statute[19:], 7)
	v, run := e.verifyReferenceText(ref, regions)
	if run == nil {
		t.Fatal("the body of the statute was not found")
	}
	if v.Status == NotFound {
		t.Fatalf("the body itself was not found: %s", v.Reason)
	}
	w := e.verifyEmphasis(ref, ref.Emphasis[0], run, regions, nil)
	if w.Status == NotFound {
		t.Errorf("the heading was reported absent (%s) beside a body that was found", w.Reason)
	}
	if w.Status != Review || w.Reason != "heading not read" {
		t.Errorf("%s/%s, want REVIEW/heading not read", w.Status, w.Reason)
	}
}
