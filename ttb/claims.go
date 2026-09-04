package ttb

import (
	"fmt"

	"treasury/verify"
)

// Inputs turns an application into the engine's reference and claims.
func Inputs(exp Expected) ([]verify.Reference, []verify.Claim) {
	refs := []verify.Reference{{Text: Statute, Emphasis: []verify.Span{{Start: 0, End: HeaderLen}}}}
	var claims []verify.Claim
	free := func(name, text string, required bool) {
		if text == "" {
			return
		}
		// The spec's 0.22 is a line-hash radius. Glyph-wise, text in the
		// label's own face lands within 3–8 percent and text in a script
		// face at 15; 0.12 sits between them until the eval tunes it.
		claims = append(claims, verify.Claim{
			Name: name, Expected: text, Required: required, Radius: 0.12, Variants: true,
			Candidates: []verify.Candidate{{Text: text, Value: text}},
		})
	}
	free("brand", exp.Brand, true)
	free("class", exp.Class, true)
	for i, line := range exp.Producer {
		free(fmt.Sprintf("producer_%d", i+1), line, true)
	}
	free("origin", exp.Origin, false)
	if exp.ABV > 0 {
		claims = append(claims, ABVClaim(exp.ABV, exp.Beverage != "beer"))
	}
	if exp.NetML > 0 {
		claims = append(claims, NetClaim(exp.NetML))
	}
	return refs, claims
}

// ABVClaim describes alcohol content as a number read off the label:
// the legal values in half-percent steps are the valid set, and the
// printed forms, including proof at twice the percentage, are the formats.
//
// ABVClaim formerly enumerated alcohol content from 0.5 to 95 percent in half-percent
// steps, in the forms labels print it, including proof.
func ABVClaim(expected float64, required bool) verify.Claim {
	valid := make([]float64, 0, 190)
	for v := 0.5; v <= 95.0+1e-9; v += 0.5 {
		valid = append(valid, v)
	}
	return verify.Claim{
		Name: "abv", Expected: Num(expected), Required: required, Radius: 0.15,
		Numeric: &verify.Numeric{
			Formats: []verify.NumericFormat{
				{Template: "{n}% Alc./Vol.", Scale: 1}, {Template: "{n}% ALC./VOL.", Scale: 1}, {Template: "{n}% ABV", Scale: 1},
				{Template: "ALC. {n}% BY VOL.", Scale: 1}, {Template: "{n}% alc/vol", Scale: 1},
				{Template: "{n} Proof", Scale: 0.5}, {Template: "({n} Proof)", Scale: 0.5}, {Template: "{n} PROOF", Scale: 0.5},
			},
			Valid: valid, Tolerance: 0.05,
		},
	}
}

// NetClaim describes net contents: the standard sizes in millilitres are
// the valid set; millilitre, litre, and fluid-ounce forms with and without
// periods are the printed formats.
func NetClaim(expectedML float64) verify.Claim {
	const flOz = 29.5735
	return verify.Claim{
		Name: "net", Expected: Num(expectedML), Required: true, Radius: 0.15,
		Numeric: &verify.Numeric{
			Formats: []verify.NumericFormat{
				{Template: "{n} mL", Scale: 1}, {Template: "{n} ml", Scale: 1}, {Template: "{n} ML", Scale: 1}, {Template: "{n}mL", Scale: 1}, {Template: "{n}ml", Scale: 1},
				{Template: "{n} L", Scale: 1000}, {Template: "{n}L", Scale: 1000}, {Template: "{n} Liter", Scale: 1000}, {Template: "{n} LITER", Scale: 1000}, {Template: "{n} Litre", Scale: 1000},
				{Template: "{n} FL OZ", Scale: flOz}, {Template: "{n} FL. OZ.", Scale: flOz}, {Template: "{n} fl oz", Scale: flOz}, {Template: "{n} fl. oz.", Scale: flOz},
			},
			Valid:     []float64{50, 100, 187, 200, 375, 500, 700, 750, 1000, 1750},
			Tolerance: 5, // a fluid-ounce figure rounds to a tenth, 3 mL
		},
	}
}
