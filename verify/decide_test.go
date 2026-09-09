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

// TestADroppedDigitIsNotADifferentValue pins the rule step 20b needed: a
// detection that clipped the seven off "750 mL" reads a legal 50 mL, and
// naming it would be a false assertion about the label.
func TestADroppedDigitIsNotADifferentValue(t *testing.T) {
	e := &Engine{opt: Options{}.withDefaults()}
	c := Claim{
		Name: "net", Expected: "750", Required: true,
		Numeric: &Numeric{
			Formats:   []NumericFormat{{Template: "{n} mL", Scale: 1}},
			Valid:     []float64{50, 750},
			Tolerance: 5,
		},
	}
	v := e.decide(c, buildRuns(regionsOf("50 mL"), 0.5))
	if v.Status == Mismatch {
		t.Errorf("a clipped figure was named as a different value: %s %q", v.Status, v.Observed)
	}
	// A value that is not the filed one with digits missing is still named.
	c.Numeric.Valid = []float64{375, 750}
	v = e.decide(c, buildRuns(regionsOf("375 mL"), 0.5))
	if v.Status != Mismatch || v.Observed != "375" {
		t.Errorf("a plainly different fill is %s %q, want MISMATCH 375", v.Status, v.Observed)
	}
}

// TestANumberIsTakenFromBesideAnotherStatement pins step 20b's own
// change: labels print the alcohol content and the fill on one line and
// the detector returns them together.
func TestANumberIsTakenFromBesideAnotherStatement(t *testing.T) {
	e := &Engine{opt: Options{}.withDefaults()}
	c := Claim{
		Name: "net", Expected: "750", Required: true,
		Numeric: &Numeric{
			Formats:   []NumericFormat{{Template: "{n} mL", Scale: 1}},
			Valid:     []float64{750},
			Tolerance: 5,
		},
	}
	v := e.decide(c, buildRuns(regionsOf("53%ALC/VOLNET.CONT.750ML"), 0.5))
	if v.Status != Verified {
		t.Errorf("a fill beside an alcohol statement is %s (%s), want VERIFIED", v.Status, v.Reason)
	}
	if v.Evidence == nil || v.Evidence.Read != "750ML" {
		t.Errorf("the evidence quotes %q, want the part that matched", v.Evidence.Read)
	}
	// A fill inside a longer figure is not that fill.
	v = e.decide(c, buildRuns(regionsOf("1750ML"), 0.5))
	if v.Status == Verified {
		t.Errorf("750 mL verified inside 1750 mL: %+v", v.Evidence)
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

// TestANameIsTakenOnlyWherePunctuationDelimitsIt pins step 21b. A name
// printed whole with other matter around it is the claim; a name that runs
// on into more of the same name is not, and step 16a's two false brands
// are of the second kind.
func TestANameIsTakenOnlyWherePunctuationDelimitsIt(t *testing.T) {
	e := &Engine{opt: Options{}.withDefaults()}
	brand := func(text string) Claim {
		return Claim{Name: "brand", Expected: text, Required: true}
	}
	cases := []struct {
		claim, read string
		want        bool
	}{
		{"APONA VINEYARDS", "APONA VINEYARDS, VENETA, OR", true},
		{"PRODUCT OF ITALY", "WHITE WINE - PRODUCT OF ITALY", true},
		{"PRODUCT OF MEXICO", "4 PRODUCTOFMEXICO 750 ML", true},
		{"PASSIONE NATURA", "Bottled by: PASSIONE NATURA, Paglieta (CH), IT", true},
		// The shapes that must stay refused.
		{"Valley Mill", "Produced and Bottled by Valley Mill Company", false},
		{"HERON BLACK", "Produced and Bottled by Heron Black Company", false},
		{"45TH PARALLEL", "Distilled & Bottled by 45th Parallel Spirits, LLC", false},
		{"ALE", "STARGAZE-INDIA PALE ALE", false},
		{"OWL'S BREW", "followus @theowlsbrew", false},
	}
	for _, c := range cases {
		v := e.decide(brand(c.claim), buildRuns(regionsOf(c.read), 0.5))
		if got := v.Status == Verified; got != c.want {
			t.Errorf("%q in %q: verified=%v, want %v", c.claim, c.read, got, c.want)
		}
	}
}

// TestANameTakenFromInsideAReadingIsTheWholeName pins what step 28d
// found the moment the boundary rule was allowed to search a chained
// run. A delimiter says where a statement ends on the label; it does not
// say the claim ends there too.
//
// Label 0028 prints "BOTTLED BY APONA VINEYARDS, VENETA, OR" and the
// application files "Apona Vineyards, LLC". The span ending at the comma
// after VINEYARDS is delimited at both ends and sits at 0.115, inside a
// radius of 0.14, and it is missing the claim's last three characters -
// so verifying it asserts a company form the label does not print, which
// is step 12b's first refusal arriving from the other direction.
func TestANameTakenFromInsideAReadingIsTheWholeName(t *testing.T) {
	e := &Engine{opt: Options{}.withDefaults()}
	producer := func(text string) Claim {
		return Claim{Name: "producer_1", Expected: text, Required: true}
	}
	cases := []struct {
		claim, read string
		want        bool
	}{
		// The claim's tail is not printed: the comma delimits the end of
		// the label's statement, not the end of the name.
		{"Bottled by Apona Vineyards, LLC", "BOTTLED BY APONA VINEYARDS, VENETA, OR", false},
		{"Heron Black Company", "HERON BLACK, PORTLAND OR", false},
		// The whole name is there, with other matter around it. (A
		// leading "BOTTLED BY " would refuse these for a different
		// reason and would not test this rule: a space has never been a
		// delimiter, which step 21b settled and TestANameIsTakenOnly...
		// pins.)
		{"APONA VINEYARDS", "APONA VINEYARDS, VENETA, OR", true},
		{"PASSIONE NATURA", "Bottled by: PASSIONE NATURA, Paglieta (CH), IT", true},
	}
	for _, c := range cases {
		v := e.decide(producer(c.claim), buildRuns(regionsOf(c.read), 0.5))
		if got := v.Status == Verified; got != c.want {
			t.Errorf("%q in %q: verified=%v, want %v", c.claim, c.read, got, c.want)
		}
	}
}
