package verify_test

import (
	"context"
	"strings"
	"testing"

	"treasury/internal/render"
	"treasury/internal/synth"
	"treasury/ttb"
	"treasury/verify"
)

// The two shapes step 16a made a false assertion on: a label that prints a
// different brand and carries the filed brand's words inside its
// producer's name. They are quoted here verbatim rather than named by
// label, which is the point of the test.
var guardCases = []struct {
	brand    string // what the application filed
	producer string // what the label prints
}{
	{"Valley Mill", "Produced and Bottled by Valley Mill Company"},
	{"Heron Black", "Produced and Bottled by Heron Black Company"},
}

// TestTheGuard is the guard the build has claimed since step 21b and has
// not had since some point before step 27b.
//
// Every gate from 21b onward checked that corpus labels 0099 and 0309
// stayed refused, by name. Step 27b found that check passing for the wrong
// reason: their brand claims sit at a distance of 0.60 against a radius of
// 0.14, because the reader no longer reads their producer lines at all -
// the nearest thing to "Valley Mill" anywhere on 0099 is "Li". A refusal
// on text that was never read tests nothing, and would have gone on
// reporting green through any change to the rule it was guarding.
//
// So the case is constructed instead of hoped for. A label is rendered
// printing the responsibility statement verbatim, and two claims are
// asked of it:
//
//   - the filed brand, which must be REFUSED, since the label prints a
//     different brand and these words are part of a company's name;
//   - the permittee, which must be VERIFIED, since that is the same line.
//
// The second assertion is what stops this test failing the way the old
// check did. If the reader stops reading the line, the permittee stops
// verifying and the test fails, instead of the brand's refusal passing
// vacuously.
func TestTheGuard(t *testing.T) {
	if testing.Short() {
		t.Skip("long: renders and reads a label per case")
	}
	faces, err := render.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	pool := ttb.NewFacePool(faces)
	if len(pool.Body) == 0 {
		t.Skip("no bundled body face")
	}
	eng, err := verify.New(verify.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	for _, c := range guardCases {
		// The label prints a brand of its own and the responsibility
		// statement holding the filed brand's words, which is exactly
		// step 16a's shape.
		printed := ttb.Expected{
			Beverage: "spirits",
			Brand:    "SAINT STONE",
			Class:    "VODKA",
			Producer: []string{c.producer, "Portland, Oregon 97209"},
			Origin:   "PRODUCT OF USA",
			ABV:      40,
			NetML:    750,
		}
		doc := ttb.LabelDocument(printed)
		img, _, err := synth.Render(doc, faces)
		if err != nil {
			t.Fatalf("%s: render: %v", c.brand, err)
		}

		// What the application filed: the brand under test, and the
		// permittee, whose name is the same line the brand hides in.
		asked := ttb.Expected{
			Beverage: "spirits",
			Brand:    c.brand,
			Class:    "VODKA",
			Producer: []string{strings.TrimPrefix(c.producer, "Produced and Bottled by "),
				"Portland, Oregon 97209"},
			Origin: "PRODUCT OF USA",
			ABV:    40,
			NetML:  750,
		}
		refs, claims := ttb.Inputs(asked)
		res, err := eng.Verify(context.Background(), img, refs, claims)
		if err != nil {
			t.Fatalf("%s: verify: %v", c.brand, err)
		}

		var brand, producer *verify.Verdict
		for i := range res.Claims {
			switch res.Claims[i].Claim {
			case "brand":
				brand = &res.Claims[i]
			case "producer_1":
				producer = &res.Claims[i]
			}
		}
		if brand == nil || producer == nil {
			t.Fatalf("%s: brand or producer_1 missing from the result", c.brand)
		}
		if brand.Status == verify.Verified {
			read := ""
			if brand.Evidence != nil {
				read = brand.Evidence.Read
			}
			t.Errorf("%q verified against a label printing %q, read %q: "+
				"this is step 16a's false assertion",
				c.brand, c.producer, read)
		}
		// Non-vacuity: the line has to have been read for the refusal to
		// mean anything.
		if producer.Status != verify.Verified {
			t.Errorf("%q: the permittee did not verify (%s), so the line was not read "+
				"and the brand's refusal proves nothing - this is the defect step 27b "+
				"found in the by-name check on labels 0099 and 0309",
				c.brand, producer.Status)
		}
	}
}
