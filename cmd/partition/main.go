// Command partition writes the one split of typefaces into an evaluation
// partition and a training partition.
//
//	partition -fontdir "C:\Windows\Fonts" -out internal/fontset/partition.json
//
// A family that can set a label goes to one side or the other by an even
// split on a hash of its name; every other family, and every bundled
// synthesis face, goes to training, since it cannot appear in a label
// anyway. The result is committed and embedded: the label generator draws
// only from the evaluation partition, the model data generators learn only
// from the training partition, and the evaluation refuses a set that names
// any other family.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"sort"

	"treasury/internal/fontset"
	"treasury/internal/render"
	"treasury/ttb"
)

func main() {
	fontdir := flag.String("fontdir", "", "directory of installed TTF/OTF faces")
	out := flag.String("out", filepath.Join("internal", "fontset", "partition.json"), "where to write the partition")
	frac := flag.Float64("evaluation", 0.5, "fraction of label-capable families that may appear in evaluated images")
	flag.Parse()

	faces, skipped, err := render.LoadDir(*fontdir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(skipped) > 0 {
		fmt.Fprintf(os.Stderr, "skipped %d files\n", len(skipped))
	}
	bundled, err := render.Bundled()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// Label-capable families: those the label generator can draw a body or a
	// brand line from. Asking the generator's own pool keeps the two
	// definitions from drifting apart.
	pool := ttb.NewFacePool(faces)
	capable := map[string]bool{}
	for _, f := range pool.Body {
		capable[f.Family] = true
	}
	for _, f := range pool.Display {
		capable[f.Family] = true
	}
	all := map[string]bool{}
	for _, f := range faces {
		all[f.Family] = true
	}
	// A bundled face synthesizes the characters a label never taught; it
	// must never set one.
	for _, f := range bundled {
		all[f.Family] = true
		capable[f.Family] = false
	}

	var evaluation, training []string
	for family := range all {
		if capable[family] && share(family) < *frac {
			evaluation = append(evaluation, family)
			continue
		}
		training = append(training, family)
	}
	sort.Strings(evaluation)
	sort.Strings(training)

	p := fontset.Partition{
		Note: fmt.Sprintf("Written by cmd/partition from %s and the bundled faces on %d families, %d of them able to set a label.",
			*fontdir, len(all), count(capable)),
		Rule:       fmt.Sprintf("A family that can set a label goes to the evaluation partition when the low 24 bits of fnv-1a(name) fall in the first %.0f%% of the space, and to training otherwise; a family that cannot set a label, and every bundled synthesis face, goes to training. Nothing else may appear in an evaluated image.", *frac*100),
		Evaluation: evaluation,
		Training:   training,
	}
	b, err := json.MarshalIndent(p, "", " ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, append(b, '\n'), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	bodies, displays := 0, 0
	for _, f := range pool.Body {
		if share(f.Family) < *frac && capable[f.Family] {
			bodies++
		}
	}
	for _, f := range pool.Display {
		if share(f.Family) < *frac && capable[f.Family] {
			displays++
		}
	}
	fmt.Printf("%s: %d families evaluation (%d body faces, %d display faces), %d families training\n",
		*out, len(evaluation), bodies, displays, len(training))
}

// share places a family in [0,1) by a hash of its name.
func share(family string) float64 {
	h := fnv.New32a()
	h.Write([]byte(family))
	return float64(h.Sum32()&0xffffff) / float64(0x1000000)
}

func count(m map[string]bool) int {
	n := 0
	for _, v := range m {
		if v {
			n++
		}
	}
	return n
}
