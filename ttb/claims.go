package ttb

import (
	"fmt"
	"math"
	"strings"

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
		// The radius is the engine's, not the domain's: it is a property
		// of how the reader reads, and it was a bit-code quantity until
		// step 19a retired the bit codes. It is fitted in 19c and lives
		// in verify.Options.
		claims = append(claims, verify.Claim{
			Name: name, Expected: text, Required: required, Variants: true,
			Candidates: cands,
		})
	}
	free("brand", exp.Brand, true)
	free("class", exp.Class, true)
	for i, line := range exp.Producer {
		name := fmt.Sprintf("producer_%d", i+1)
		free(name, line, true)
		if i == 0 && line != "" {
			// The permittee's name is printed inside a statement of
			// responsibility, which the application does not file: the
			// prescribed phrase before it and the address after it are
			// accepted spellings of the same claim.
			var addr string
			if len(exp.Producer) > 1 {
				addr = exp.Producer[1]
			}
			c := &claims[len(claims)-1]
			for _, alt := range append([]string{line}, exp.Aliases[name]...) {
				if alt == "" {
					continue
				}
				for _, form := range responsibilityForms(alt, addr) {
					c.Candidates = append(c.Candidates, verify.Candidate{Text: form, Value: line})
				}
			}
		}
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
		Name: "abv", Expected: Num(expected), Required: required,
		Numeric: &verify.Numeric{Formats: formats, Valid: admit(valid, expected, 0.05), Tolerance: 0.05},
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
		Name: "net", Expected: Num(expectedML), Required: true,
		Numeric: &verify.Numeric{
			Formats: []verify.NumericFormat{
				{Template: "{n} mL", Scale: 1}, {Template: "{n} ml", Scale: 1}, {Template: "{n} ML", Scale: 1}, {Template: "{n}mL", Scale: 1}, {Template: "{n}ml", Scale: 1}, {Template: "{n}ML", Scale: 1},
				{Template: "({n} ML)", Scale: 1}, {Template: "({n} mL)", Scale: 1}, {Template: "({n} ml)", Scale: 1},
				{Template: "{n} L", Scale: 1000}, {Template: "{n}L", Scale: 1000}, {Template: "{n} Liter", Scale: 1000}, {Template: "{n} LITER", Scale: 1000}, {Template: "{n} Litre", Scale: 1000},
				{Template: "{n} FL OZ", Scale: flOz}, {Template: "{n} FL. OZ.", Scale: flOz}, {Template: "{n} fl oz", Scale: flOz}, {Template: "{n} fl. oz.", Scale: flOz}, {Template: "{n} Fl. Oz.", Scale: flOz},
			},
			Valid:     admit(valid, expectedML, 5),
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

// admit puts the filed value into the field's own vocabulary when the
// standard list does not already hold it. A field whose valid set cannot
// express what the application filed can never verify the claim and can
// name a neighbouring value instead: label 0007 declares 355 mL, which is
// the twelve-fluid-ounce can, and the spirits list of standards of fill
// does not contain it, so "12 FL.OZ." on the label was read as the 375 mL
// entry's "12.7 FL OZ" and asserted as a mismatch.
func admit(valid []float64, want, tol float64) []float64 {
	for _, v := range valid {
		if math.Abs(v-want) <= tol {
			return valid
		}
	}
	return append(append([]float64{}, valid...), want)
}

// Responsibility are the phrases the regulation prescribes for the
// statement of responsibility (27 CFR 5.36, 4.35 and 7.25): a bottler,
// importer or brewer is named on the label as "Bottled by <name>", and the
// application files the name alone. The phrase is not part of the name, so
// a label printing one of these before the filed name still names the
// filed permittee, and no phrase on this list can let a different entity
// satisfy the claim.
var Responsibility = []string{
	"Bottled by", "Bottled for", "Produced and Bottled by", "Produced by",
	"Distilled by", "Distilled and Bottled by", "Blended and Bottled by",
	"Blended by", "Vinted and Bottled by", "Cellared and Bottled by",
	"Made and Bottled by", "Manufactured by", "Packed by", "Prepared by",
	"Brewed by", "Brewed and Bottled by", "Brewed and Canned by",
	"Imported by", "Imported and Bottled by", "Sole Agent",
}

// responsibilityForms is every way the label may set the permittee's name
// in the statement of responsibility: the name alone, a prescribed phrase
// before it, and the filed address or any tail of it after it, since the
// label commonly prints only the city and the state of an address filed in
// full. Every piece comes from the regulation or from the application
// itself, never from the label.
func responsibilityForms(name string, address string) []string {
	tails := []string{""}
	if w := strings.Fields(address); len(w) > 0 {
		for i := range w {
			if len(w)-i <= 4 {
				tails = append(tails, " "+strings.Join(w[i:], " "))
			}
		}
	}
	out := make([]string, 0, (len(Responsibility)+1)*len(tails))
	for _, tail := range tails {
		out = append(out, name+tail)
		for _, phrase := range Responsibility {
			out = append(out, phrase+" "+name+tail)
		}
	}
	return out
}
