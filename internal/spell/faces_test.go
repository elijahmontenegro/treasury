package spell_test

import (
	"sort"
	"testing"

	"treasury/internal/alphabet"
	"treasury/internal/encoder"
	"treasury/internal/preprocess"
	"treasury/internal/region"
	"treasury/internal/render"
	"treasury/internal/spell"
	"treasury/internal/synth"
	"treasury/ttb"
)

// TestFaceScores renders the sample label in a few faces and reports how
// every bundled face scores against the learned body; the label's own face
// must come first.
func TestFaceScores(t *testing.T) {
	faces, err := render.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{"Go Regular", "PTSerif Regular", "Lato Regular", "PTSans Regular"} {
		heavy := "Go Bold"
		if b, ok := render.Sibling(faces, mustFind(t, faces, body), "bold"); ok {
			heavy = b.Name
		}
		img, _, err := synth.Render(ttb.LabelDocumentVariant(ttb.Sample(), ttb.Variant{BodyFace: body, HeavyFace: heavy}), faces)
		if err != nil {
			t.Fatal(err)
		}
		pre, err := preprocess.Run(img, preprocess.Default())
		if err != nil {
			t.Fatal(err)
		}
		lines, _ := region.Propose(pre.Bin, pre.Glare, region.Default())
		a, err := alphabet.Find(lines, pre.Bin, pre.Gray, ttb.Statute, []alphabet.Span{{Start: 0, End: ttb.HeaderLen}}, alphabet.DefaultOptions())
		if err != nil {
			t.Fatal(err)
		}
		scores := spell.FaceScores(a, faces, encoder.Glyph(), -1)
		sort.Slice(scores, func(i, j int) bool { return scores[i].Score < scores[j].Score })
		for i, s := range scores {
			if i >= 5 {
				break
			}
			t.Logf("body %-16s #%d %-20s score=%.3f", body, i+1, s.Face.Name, s.Score)
		}
		// Strokes are normalized before scoring, so a family's weights tie
		// on letterforms; the family must be right.
		if scores[0].Face.Family != mustFind(t, faces, body).Family {
			t.Errorf("body %s: nearest face is %s", body, scores[0].Face.Name)
		}
	}
}

func mustFind(t *testing.T, faces []*render.Face, name string) *render.Face {
	t.Helper()
	f, ok := render.Find(faces, name)
	if !ok {
		t.Fatalf("no face %q", name)
	}
	return f
}
