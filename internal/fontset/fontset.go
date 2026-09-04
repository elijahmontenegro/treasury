// Package fontset holds the one partition of typefaces into those that may
// appear in an evaluated image and those the engine's models may learn from.
//
// A model trained on a face that later sets an evaluation label reports its
// own training data back as accuracy. The partition is recorded once, in
// partition.json, embedded here so every tool built from this tree reads the
// same one: the label generator draws only from the evaluation partition,
// both model data generators draw their training frames only from the
// training partition and report their held-out accuracy on the evaluation
// partition, and the evaluation refuses a set that names any other family.
package fontset

import (
	_ "embed"
	"encoding/json"
	"sort"
)

//go:embed partition.json
var partitionJSON []byte

// Partition is the recorded split.
type Partition struct {
	Note       string   `json:"note"`
	Rule       string   `json:"rule"`
	Evaluation []string `json:"evaluation"`
	Training   []string `json:"training"`
}

var (
	loaded Partition
	eval   map[string]bool
	train  map[string]bool
)

func init() {
	if err := json.Unmarshal(partitionJSON, &loaded); err != nil {
		panic("fontset: partition.json: " + err.Error())
	}
	eval = make(map[string]bool, len(loaded.Evaluation))
	for _, f := range loaded.Evaluation {
		eval[f] = true
	}
	train = make(map[string]bool, len(loaded.Training))
	for _, f := range loaded.Training {
		train[f] = true
	}
}

// Recorded is the partition as committed.
func Recorded() Partition { return loaded }

// Evaluation reports whether a family may appear in an evaluated image. A
// family the partition does not name may not: an unknown face is treated as
// one the models may have learned from.
func Evaluation(family string) bool { return eval[family] }

// Training reports whether a family may be learned from. Every family the
// partition does not name may be, since it may not appear in an evaluated
// image.
func Training(family string) bool { return !eval[family] }

// Known reports whether the partition names the family at all.
func Known(family string) bool { return eval[family] || train[family] }

// Leaked returns, sorted, the families of a set that may not appear in an
// evaluated image: those the models may have learned from, including any the
// partition does not name.
func Leaked(families []string) []string {
	var bad []string
	for _, f := range families {
		if !eval[f] {
			bad = append(bad, f)
		}
	}
	sort.Strings(bad)
	return bad
}
