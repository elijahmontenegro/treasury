// Package ttb is the alcohol-label configuration of the engine: the statutory
// reference text, what an application claims, and a label template for the
// generator. It holds data, not mechanism.
package ttb

import (
	"fmt"
	"math"
	"strconv"

	"treasury/internal/synth"
)

// Statute is the health warning of 27 CFR 16.21, verbatim. Verified against
// the eCFR API, govinfo, and Cornell LII on 2026-09-02; pinned by TestStatuteHash.
const Statute = "GOVERNMENT WARNING: (1) According to the Surgeon General, women should not drink alcoholic beverages during pregnancy because of the risk of birth defects. (2) Consumption of alcoholic beverages impairs your ability to drive a car or operate machinery, and may cause health problems."

// HeaderLen is the length of "GOVERNMENT WARNING:", the span that 27 CFR
// 16.22 requires in capitals and bold type.
const HeaderLen = 19

// Expected is what a label application states.
type Expected struct {
	Beverage string   `json:"beverage"` // spirits, wine, beer
	Brand    string   `json:"brand"`
	Class    string   `json:"class"`
	Producer []string `json:"producer"` // name and address, one line each
	Origin   string   `json:"origin"`
	ABV      float64  `json:"abv"`    // percent alcohol by volume
	NetML    float64  `json:"net_ml"` // net contents in millilitres

	// Other spellings of a claim that the application itself states, by
	// claim name (amendment step 12b): the permittee's operating name
	// beside the name on its permit, where the form gives both. A label
	// carrying either identifies the permittee, so either verifies the
	// claim, and the value reported stays the one filed.
	Aliases map[string][]string `json:"aliases,omitempty"`
}

// Sample is the application behind testdata/sample.png.
func Sample() Expected {
	return Expected{
		Beverage: "spirits",
		Brand:    "OLD TOM DISTILLERY",
		Class:    "Kentucky Straight Bourbon Whiskey",
		Producer: []string{"Distilled and Bottled by Old Tom Distillery", "Louisville, Kentucky 40202"},
		Origin:   "Product of USA",
		ABV:      45,
		NetML:    750,
	}
}

// Num formats a value with the fewest decimals that reproduce it: 45, 45.5, 1.75.
func Num(v float64) string {
	if v == math.Trunc(v) {
		return strconv.Itoa(int(v))
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// ABVText is the common "45% Alc./Vol." form.
func ABVText(v float64) string { return Num(v) + "% Alc./Vol." }

// ProofText is the proof form, two times the percentage.
func ProofText(v float64) string { return Num(2*v) + " Proof" }

// NetText is the common net-contents form: millilitres below one litre, litres above.
func NetText(ml float64) string {
	if ml >= 1000 {
		return Num(ml/1000) + " L"
	}
	return Num(ml) + " mL"
}

// Variant is a deliberate deviation of a generated label from the default
// compliant layout: an error to catch, or a change of type.
type Variant struct {
	HeaderTitleCase bool              // "Government Warning:" instead of capitals
	HeaderRegular   bool              // header in the body weight
	Wording         string            // replaces the statutory text when set
	NoWarning       bool              // omit the warning block entirely
	BrandText       string            // print this instead of the application's brand
	BodyFace        string            // face for body text; default Go Regular
	HeavyFace       string            // face for the header; default Go Bold
	BrandFace       string            // face for the brand line; default the heavy face
	ClaimFaces      map[string]string // face per claim (class, producer, origin, abv, net); default the body face
	WarningCaps     bool              // the warning set in capitals, as half of real labels do
	Vertical        bool              // the warning set vertically along a side
	Crowded         bool              // rows of other text in the warning's size directly above and below it
}

// LabelDocument lays exp out as a compliant spirits label for the generator.
func LabelDocument(exp Expected) synth.Document { return LabelDocumentVariant(exp, Variant{}) }

// LabelDocumentVariant lays exp out with a deliberate deviation.
func LabelDocumentVariant(exp Expected, v Variant) synth.Document {
	const w, h = 1200, 1600
	cx := w / 2
	body, heavy := v.BodyFace, v.HeavyFace
	if body == "" {
		body = "Go Regular"
	}
	if heavy == "" {
		heavy = "Go Bold"
	}
	brandFace := v.BrandFace
	if brandFace == "" {
		brandFace = heavy
	}
	brand := exp.Brand
	if v.BrandText != "" {
		brand = v.BrandText
	}
	doc := synth.Document{W: w, H: h}
	item := func(text, face string, px float64, y int, claim string) {
		doc.Items = append(doc.Items, synth.Item{Text: text, Face: face, Px: px, X: cx, Y: y, Center: true, Claim: claim})
	}
	item(brand, brandFace, 84, 180, "brand")
	item("SMALL BATCH", heavy, 36, 270, "")
	item(exp.Class, body, 44, 350, "class")
	item("AGED 8 YEARS", body, 32, 420, "")
	item("Batch No. 12 - Est. 1887", body, 28, 480, "")
	item("Distilled from grain and aged in new charred oak barrels", body, 26, 560, "")
	doc.Items = append(doc.Items,
		synth.Item{Text: fmt.Sprintf("%s (%s)", ABVText(exp.ABV), ProofText(exp.ABV)), Face: body, Px: 34, X: 120, Y: 640, Claim: "abv"},
		synth.Item{Text: NetText(exp.NetML), Face: body, Px: 34, X: 900, Y: 640, Claim: "net"},
	)
	y := 720
	for _, line := range exp.Producer {
		item(line, body, 28, y, "producer")
		y += 40
	}
	item(exp.Origin, body, 28, y, "origin")
	item("Handcrafted in limited quantities", body, 26, 900, "")
	text := Statute
	if v.Wording != "" {
		text = v.Wording
	}
	if v.HeaderTitleCase {
		text = "Government Warning:" + text[HeaderLen:]
	}
	if v.HeaderRegular {
		heavy = body
	}
	if !v.NoWarning {
		doc.Blocks = append(doc.Blocks, synth.Block{
			Text:      text,
			Heavy:     []synth.Span{{Start: 0, End: HeaderLen}},
			Face:      body,
			HeavyFace: heavy,
			Px:        24,
			X:         120,
			Y:         1020,
			Width:     960,
			Leading:   1.35,
			Claim:     "reference",
		})
	}
	item("Please drink responsibly.", body, 26, 1300, "")
	item("www.oldtomdistillery.example", body, 22, 1380, "")
	return doc
}
