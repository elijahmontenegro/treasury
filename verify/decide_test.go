package verify

import (
	"image"
	"testing"
)

// TestDecimalIsPartOfTheNumber pins the defect step 19c's first measuring
// run exposed: normalization dropped every punctuation mark, which is
// right for a name and wrong for a number, and made a label printing 4.5
// percent alcohol read as 45 percent.
func TestDecimalIsPartOfTheNumber(t *testing.T) {
	if a, b := normalize("4.5% Alc./Vol."), normalize("45% Alc./Vol."); a == b {
		t.Fatalf("4.5 and 45 normalize alike, to %q", a)
	}
	if got, want := normalize("14,5% vol."), normalize("14.5% VOL"); got != want {
		t.Errorf("a comma decimal is the same number: %q against %q", got, want)
	}
	if got, want := normalize("VIÑEDOS, S.A."), "VINEDOSSA"; got != want {
		t.Errorf("normalize(%q) = %q, want %q", "VIÑEDOS, S.A.", got, want)
	}
}

func numericClaim() Claim {
	return Claim{
		Name: "abv", Expected: "8.5", Required: true,
		Numeric: &Numeric{
			Formats:   []NumericFormat{{Template: "{n}% alc/vol", Scale: 1}},
			Valid:     []float64{8.5, 85},
			Tolerance: 0.05,
		},
	}
}

func regionsOf(text string) []Region {
	return []Region{{Box: image.Rect(0, 0, 100, 20), Text: text, Confidence: 0.9}}
}

// TestAReadingWithCharactersMissingDoesNotContradict pins the two rules
// that hold precision on a number: the margin the application's own value
// is protected by, and step 7a's completeness rule restated for recognised
// text. A recogniser that loses the point in "8.5% alc/vol" reads a
// perfectly legal 85 percent; the engine must not assert it.
func TestAReadingWithCharactersMissingDoesNotContradict(t *testing.T) {
	e := &Engine{opt: Options{}.withDefaults()}
	v := e.decide(numericClaim(), buildRuns(regionsOf("85% alc/vol"), 0.5))
	if v.Status == Mismatch {
		t.Errorf("a reading missing the decimal point contradicted the application: %s (%s)",
			v.Status, v.Reason)
	}
	// The same claim read as printed still verifies.
	v = e.decide(numericClaim(), buildRuns(regionsOf("8.5% alc/vol"), 0.5))
	if v.Status != Verified {
		t.Errorf("the label's own value read back is %s (%s), want VERIFIED", v.Status, v.Reason)
	}
	// One character of difference is not enough to contradict the
	// application, because one character is what a recogniser gets wrong:
	// the engine reviews.
	c := numericClaim()
	c.Numeric.Valid = []float64{8.5, 9.5}
	v = e.decide(c, buildRuns(regionsOf("9.5% alc/vol"), 0.5))
	if v.Status == Verified || v.Status == Mismatch {
		t.Errorf("one character of difference decided %s (%s); it settles nothing", v.Status, v.Reason)
	}
	// A value that is plainly not the filed one is named.
	c = numericClaim()
	c.Numeric.Valid = []float64{8.5, 40}
	v = e.decide(c, buildRuns(regionsOf("40% alc/vol"), 0.5))
	if v.Status != Mismatch || v.Observed != "40" {
		t.Errorf("a plainly different value is %s %q, want MISMATCH 40", v.Status, v.Observed)
	}
	// And the number itself is read exactly: a wrong digit in a long
	// printed form is a twelfth of the string, which the radius would
	// otherwise admit.
	c = numericClaim()
	c.Expected = "5.1"
	c.Numeric.Valid = []float64{5.1}
	c.Numeric.Formats = []NumericFormat{{Template: "ALC. {n}% BY VOL.", Scale: 1}}
	v = e.decide(c, buildRuns(regionsOf("ALC. 4.1% BY VOL."), 0.5))
	if v.Status == Verified {
		t.Errorf("a claim of 5.1 verified against a label printing 4.1: %+v", v.Evidence)
	}
}

// TestAClaimIsNotMatchedInsideALongerLine pins step 16a's lesson in the
// new representation: the filed brand's words appearing inside a
// producer's name is not the brand.
func TestAClaimIsNotMatchedInsideALongerLine(t *testing.T) {
	e := &Engine{opt: Options{}.withDefaults()}
	c := Claim{Name: "brand", Expected: "Valley Mill", Required: true}
	v := e.decide(c, buildRuns(regionsOf("VALLEY MILL DISTILLERY, INC."), 0.5))
	if v.Status == Verified {
		t.Errorf("a brand read inside a longer name verified: %+v", v.Evidence)
	}
}
