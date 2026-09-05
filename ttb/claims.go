package ttb

import (
	"fmt"
	"math"

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
		// The application names the permittee twice, by its operating
		// name and by the name on its permit, and a label may print
		// either (amendment step 12b). Both are accepted spellings of
		// the same claim, in the way the alcohol formats are: the
		// alternatives come from the application, never from the label.
		cands := []verify.Candidate{{Text: text, Value: text}}
		for _, alt := range exp.Aliases[name] {
			if alt != "" && alt != text {
				cands = append(cands, verify.Candidate{Text: alt, Value: text})
			}
		}
		// The spec's 0.22 is a line-hash radius. Glyph-wise, text in the
		// label's own face lands within 3–8 percent and text in a script
		// face at 15; 0.12 sits between them until the eval tunes it.
		claims = append(claims, verify.Claim{
			Name: name, Expected: text, Required: required, Radius: 0.12, Variants: true,
			Candidates: cands,
		})
	}
	free("brand", exp.Brand, true)
	free("class", exp.Class, true)
	for i, line := range exp.Producer {
		free(fmt.Sprintf("producer_%d", i+1), line, true)
	}
	free("origin", exp.Origin, false)
	if exp.ABV > 0 {
		if exp.Beverage == "beer" {
			claims = append(claims, abvClaim(exp.ABV, false, 0.1))
		} else {
			claims = append(claims, ABVClaim(exp.ABV, true))
		}
	}
	if exp.NetML > 0 {
		if exp.Beverage == "beer" {
			claims = append(claims, netClaim(exp.NetML, beerNet))
		} else {
			claims = append(claims, NetClaim(exp.NetML))
		}
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
	return abvClaim(expected, required, 0.5)
}

// abvClaim builds the alcohol-content claim with the given step between
// valid values: half a percent for wine and spirits, a tenth for malt
// beverages, which state 3.75 or 4.1.
func abvClaim(expected float64, required bool, step float64) verify.Claim {
	var valid []float64
	for v := step; v <= 95.0+1e-9; v += step {
		valid = append(valid, math.Round(v*100)/100)
	}
	// The printed forms 27 CFR 5.65, 4.36, and 7.71 allow, in the casings
	// labels use, and proof at twice the percentage.
	var formats []verify.NumericFormat
	for _, t := range []string{
		"{n}% Alc./Vol.", "{n}% ALC./VOL.", "{n}% alc./vol.", "{n}% Alc/Vol", "{n}% ALC/VOL", "{n}% alc/vol", "{n} % ALC/VOL",
		"{n}% ABV", "{n}% abv",
		"ALC. {n}% BY VOL.", "Alc. {n}% by Vol.", "ALC {n}% BY VOL", "Alc {n}% by Vol.", "ALC {n}% BY VOL.",
		"{n}% ALC BY VOL", "{n}% Alc by Vol", "{n}% alc. by vol.", "{n}% ALC. BY VOL.",
		"ALC. BY VOL. {n}%", "Alc. by Vol. {n}%", "ALCOHOL {n}% BY VOLUME", "Alcohol {n}% by volume",
	} {
		formats = append(formats, verify.NumericFormat{Template: t, Scale: 1})
	}
	for _, t := range []string{"{n} Proof", "({n} Proof)", "{n} PROOF", "({n} PROOF)", "PROOF {n}"} {
		formats = append(formats, verify.NumericFormat{Template: t, Scale: 0.5})
	}
	return verify.Claim{
		Name: "abv", Expected: Num(expected), Required: required, Radius: 0.15,
		Numeric: &verify.Numeric{Formats: formats, Valid: valid, Tolerance: 0.05},
	}
}

// NetClaim describes net contents: the standard sizes in millilitres are
// the valid set; millilitre, litre, and fluid-ounce forms with and without
// periods are the printed formats.
func NetClaim(expectedML float64) verify.Claim {
	return netClaim(expectedML, []float64{50, 100, 187, 200, 375, 500, 700, 750, 1000, 1750})
}

// netClaim builds the net-contents claim over the given standards of fill.
func netClaim(expectedML float64, valid []float64) verify.Claim {
	const flOz = 29.5735
	return verify.Claim{
		Name: "net", Expected: Num(expectedML), Required: true, Radius: 0.15,
		Numeric: &verify.Numeric{
			Formats: []verify.NumericFormat{
				{Template: "{n} mL", Scale: 1}, {Template: "{n} ml", Scale: 1}, {Template: "{n} ML", Scale: 1}, {Template: "{n}mL", Scale: 1}, {Template: "{n}ml", Scale: 1}, {Template: "{n}ML", Scale: 1},
				{Template: "({n} ML)", Scale: 1}, {Template: "({n} mL)", Scale: 1}, {Template: "({n} ml)", Scale: 1},
				{Template: "{n} L", Scale: 1000}, {Template: "{n}L", Scale: 1000}, {Template: "{n} Liter", Scale: 1000}, {Template: "{n} LITER", Scale: 1000}, {Template: "{n} Litre", Scale: 1000},
				{Template: "{n} FL OZ", Scale: flOz}, {Template: "{n} FL. OZ.", Scale: flOz}, {Template: "{n} fl oz", Scale: flOz}, {Template: "{n} fl. oz.", Scale: flOz}, {Template: "{n} Fl. Oz.", Scale: flOz},
			},
			Valid:     valid,
			Tolerance: 5, // a fluid-ounce figure rounds to a tenth, 3 mL
			// The standards of fill are a closed list in the regulation:
			// a dozen values, where an alcohol content is one of 190.
			Enumerable: true,
		},
	}
}

// beerNet are the fills malt beverages come in, in millilitres: 7, 8, 10,
// 11.2, 12, 16, 19.2, 22, 24, 25.4, and 32 fluid ounces and the metric
// cans and bottles.
var beerNet = []float64{207, 237, 296, 330, 331, 350, 355, 375, 473, 500, 568, 650, 710, 750, 946, 1000}
