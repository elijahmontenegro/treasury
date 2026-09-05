package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"treasury/internal/fontset"
)

// TestEvaluationRefusesTrainedFaces requires the evaluation to refuse a set
// drawn from faces the models trained on, rather than warn about it: a
// number measured on such a set is the models' training data reported back.
func TestEvaluationRefusesTrainedFaces(t *testing.T) {
	p := fontset.Recorded()
	dir := t.TempDir()
	write := func(families []string) {
		b, err := json.Marshal(map[string]any{"families": families})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "manifest.json"), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write([]string{p.Evaluation[0], p.Training[0]})
	err := run(dir, []string{"dual"}, 1, false, 0, "")
	if err == nil {
		t.Fatal("a set drawn from a trained face was evaluated")
	}
	if !strings.Contains(err.Error(), p.Training[0]) {
		t.Errorf("the refusal does not name the family: %v", err)
	}

	// A set of evaluation families gets past the check and fails later for
	// want of labels, which is the check passing.
	write([]string{p.Evaluation[0]})
	if err := run(dir, []string{"dual"}, 1, false, 0, ""); err != nil && strings.Contains(err.Error(), "trained on") {
		t.Errorf("an evaluation-only set was refused: %v", err)
	}
}
