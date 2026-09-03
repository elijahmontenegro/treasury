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
		claims = append(claims, verify.Claim{
			Name: name, Expected: text, Required: required, Radius: 0.22,
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

// ABVClaim enumerates alcohol content from 0.5 to 95 percent in half-percent
// steps, in the forms labels print it, including proof.
func ABVClaim(expected float64, required bool) verify.Claim {
	c := verify.Claim{Name: "abv", Expected: Num(expected), Required: required, Radius: 0.15}
	for v := 0.5; v <= 95.0+1e-9; v += 0.5 {
		n := Num(v)
		for _, text := range []string{
			n + "% Alc./Vol.", n + "% ALC./VOL.", n + "% ABV", "ALC. " + n + "% BY VOL.", n + "% alc/vol", Num(2*v) + " Proof",
		} {
			c.Candidates = append(c.Candidates, verify.Candidate{Text: text, Value: n})
		}
	}
	return c
}

// NetClaim enumerates the standard net-contents sizes in millilitre, litre,
// and fluid-ounce forms with and without periods.
func NetClaim(expectedML float64) verify.Claim {
	c := verify.Claim{Name: "net", Expected: Num(expectedML), Required: true, Radius: 0.15}
	for _, ml := range []float64{50, 100, 187, 200, 375, 500, 700, 750, 1000, 1750} {
		value := Num(ml)
		var texts []string
		if ml >= 1000 {
			l := Num(ml / 1000)
			texts = append(texts, l+" L", l+"L", l+" l", l+" Liter", l+" Litre")
		} else {
			m := Num(ml)
			texts = append(texts, m+" mL", m+" ml", m+" ML", m+"mL", m+"ml")
		}
		oz := Num(roundTo(ml/29.5735, 0.1))
		texts = append(texts, oz+" FL OZ", oz+" FL. OZ.", oz+" fl oz", oz+" fl. oz.")
		for _, t := range texts {
			c.Candidates = append(c.Candidates, verify.Candidate{Text: t, Value: value})
		}
	}
	return c
}

func roundTo(v, step float64) float64 {
	return float64(int64(v/step+0.5)) * step
}
