// Command gen writes synthetic documents with ground truth.
//
//	gen sample [-out testdata] [-rotate 7] [-blur 1.0] [-jpeg 50]
//	gen set -n 500 -seed 1 -out synth [-fontdir DIR] [-errors 0.2] [-clean 0.2]
//
// sample writes the fixed sample label and its augmented copy with per-glyph
// truth. set writes a randomized labelled set: NNNN.png, NNNN.json (the
// application the decoder consumes), NNNN.truth.json (what was printed,
// the error if any, the augmentation), and manifest.json listing the faces
// used so the evaluation can refuse a set drawn from the bundled ones.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"math/rand"
	"os"
	"path/filepath"
	"sort"

	"treasury/internal/bitmap"
	"treasury/internal/fontset"
	"treasury/internal/imgops"
	"treasury/internal/render"
	"treasury/internal/synth"
	"treasury/ttb"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "sample":
		fs := flag.NewFlagSet("sample", flag.ExitOnError)
		out := fs.String("out", "testdata", "output directory")
		rot := fs.Float64("rotate", 7, "rotation of the augmented copy in degrees")
		blur := fs.Float64("blur", 1.0, "Gaussian blur sigma of the augmented copy")
		jpg := fs.Int("jpeg", 50, "JPEG quality of the augmented copy")
		_ = fs.Parse(os.Args[2:])
		err = sample(*out, synth.Aug{RotateDeg: *rot, BlurSigma: *blur, JPEGQuality: *jpg})
	case "set":
		fs := flag.NewFlagSet("set", flag.ExitOnError)
		n := fs.Int("n", 500, "number of labels")
		seed := fs.Int64("seed", 1, "random seed")
		out := fs.String("out", "synth", "output directory")
		fontdir := fs.String("fontdir", "", "directory of .ttf faces to draw with; default the bundled faces (leaks into the eval)")
		errors := fs.Float64("errors", 0.2, "share of labels carrying a deliberate error")
		clean := fs.Float64("clean", 0.2, "share of labels left un-augmented")
		_ = fs.Parse(os.Args[2:])
		err = set(*out, *n, *seed, *fontdir, *errors, *clean)
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "gen:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: gen sample [-out dir] | gen set -n N -seed S -out dir [-fontdir DIR]")
	os.Exit(2)
}

func sample(dir string, aug synth.Aug) error {
	faces, err := render.Bundled()
	if err != nil {
		return err
	}
	exp := ttb.Sample()
	img, truth, err := synth.Render(ttb.LabelDocument(exp), faces)
	if err != nil {
		return err
	}
	if err := write(dir, "sample", img, exp, truth); err != nil {
		return err
	}
	augImg, augTruth, err := synth.Augment(img, truth, aug)
	if err != nil {
		return err
	}
	return write(dir, "sample_aug", augImg, exp, augTruth)
}

// Manifest describes a generated set.
type Manifest struct {
	Seed     int64    `json:"seed"`
	N        int      `json:"n"`
	FontDir  string   `json:"font_dir"`
	Faces    []string `json:"faces"`
	Families []string `json:"families"`
	Errors   float64  `json:"error_rate"`
	Clean    float64  `json:"clean_rate"`
}

// Truth is what the eval needs per label.
type Truth struct {
	Printed ttb.Printed `json:"printed"`
	Aug     *synth.Aug  `json:"aug,omitempty"`
}

func set(dir string, n int, seed int64, fontdir string, errorRate, cleanRate float64) error {
	var faces []*render.Face
	var err error
	if fontdir == "" {
		faces, err = render.Bundled()
	} else {
		var skipped []string
		faces, skipped, err = render.LoadDir(fontdir)
		if len(skipped) > 0 {
			fmt.Fprintf(os.Stderr, "skipped %d files in %s\n", len(skipped), fontdir)
		}
	}
	if err != nil {
		return err
	}
	if fontdir != "" {
		// Only the evaluation partition may set an evaluated label: a
		// model trained on a face that sets one reports its own training
		// data back as accuracy.
		var kept []*render.Face
		for _, f := range faces {
			if fontset.Evaluation(f.Family) {
				kept = append(kept, f)
			}
		}
		if len(kept) == 0 {
			return fmt.Errorf("no face in %q is in the evaluation partition", fontdir)
		}
		fmt.Printf("%d of %d faces are in the evaluation partition\n", len(kept), len(faces))
		faces = kept
	}
	pool := ttb.NewFacePool(faces)
	if len(pool.Body) == 0 {
		return fmt.Errorf("no family in %q has both a regular and a bold text face", fontdir)
	}
	fmt.Printf("%d body faces, %d display faces\n", len(pool.Body), len(pool.Display))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	rng := rand.New(rand.NewSource(seed))
	used := map[string]bool{}
	families := map[string]bool{}
	for i := 1; i <= n; i++ {
		doc, exp, pr := ttb.Generate(rng, pool, errorRate)
		img, truth, err := synth.Render(doc, faces)
		if err != nil {
			return fmt.Errorf("label %d: %w", i, err)
		}
		if pr.Inverted {
			img = imgops.Invert(img)
		}
		names := []string{pr.BodyFace, pr.HeavyFace, pr.BrandFace}
		for _, f := range pr.ClaimFaces {
			names = append(names, f)
		}
		for _, f := range names {
			used[f] = true
			if face, ok := render.Find(faces, f); ok {
				families[face.Family] = true
			}
		}
		t := Truth{Printed: pr}
		if rng.Float64() >= cleanRate {
			a := synth.Random(rng)
			if img, _, err = synth.Augment(img, truth, a); err != nil {
				return err
			}
			t.Aug = &a
		}
		name := fmt.Sprintf("%04d", i)
		if err := bitmap.WritePNG(filepath.Join(dir, name+".png"), img); err != nil {
			return err
		}
		if err := writeJSON(filepath.Join(dir, name+".json"), exp); err != nil {
			return err
		}
		if err := writeJSON(filepath.Join(dir, name+".truth.json"), t); err != nil {
			return err
		}
		if i%50 == 0 {
			fmt.Printf("%d labels\n", i)
		}
	}
	m := Manifest{Seed: seed, N: n, FontDir: fontdir, Errors: errorRate, Clean: cleanRate}
	for f := range used {
		m.Faces = append(m.Faces, f)
	}
	for f := range families {
		m.Families = append(m.Families, f)
	}
	sort.Strings(m.Faces)
	sort.Strings(m.Families)
	return writeJSON(filepath.Join(dir, "manifest.json"), m)
}

func write(dir, name string, img image.Image, exp ttb.Expected, truth *synth.Truth) error {
	if err := bitmap.WritePNG(filepath.Join(dir, name+".png"), img); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(dir, name+".json"), exp); err != nil {
		return err
	}
	return writeJSON(filepath.Join(dir, name+".truth.json"), truth)
}

func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
