package fontset_test

import (
	"testing"

	"treasury/internal/fontset"
	"treasury/internal/render"
)

// TestPartitionIsDisjoint requires the recorded partition to be a partition:
// no family on both sides, and both sides populated.
func TestPartitionIsDisjoint(t *testing.T) {
	p := fontset.Recorded()
	if len(p.Evaluation) == 0 || len(p.Training) == 0 {
		t.Fatalf("partition is empty: %d evaluation, %d training", len(p.Evaluation), len(p.Training))
	}
	eval := map[string]bool{}
	for _, f := range p.Evaluation {
		if eval[f] {
			t.Errorf("%q listed twice in the evaluation partition", f)
		}
		eval[f] = true
	}
	for _, f := range p.Training {
		if eval[f] {
			t.Errorf("%q is in both partitions", f)
		}
	}
	if p.Rule == "" {
		t.Error("the partition does not record the rule that built it")
	}
}

// TestBundledFacesNeverEvaluate requires every face the engine synthesizes
// from to be on the training side: a label drawn in one would match a
// synthesized glyph by the face that synthesized it.
func TestBundledFacesNeverEvaluate(t *testing.T) {
	faces, err := render.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range faces {
		if fontset.Evaluation(f.Family) {
			t.Errorf("bundled family %q may set an evaluated label", f.Family)
		}
	}
}

// TestLeakedRefusesTrainedFaces requires the check to name any family the
// models may have learned from, including one the partition never heard of.
func TestLeakedRefusesTrainedFaces(t *testing.T) {
	p := fontset.Recorded()
	if got := fontset.Leaked(p.Evaluation); len(got) != 0 {
		t.Errorf("the evaluation partition was refused: %v", got)
	}
	trained := p.Training[0]
	unknown := "A Face No Machine Has"
	got := fontset.Leaked([]string{p.Evaluation[0], trained, unknown})
	if len(got) != 2 {
		t.Fatalf("expected the trained and the unknown family to be refused, got %v", got)
	}
	if fontset.Evaluation(unknown) || !fontset.Training(unknown) {
		t.Error("an unnamed family must be treated as one the models may have learned from")
	}
}
