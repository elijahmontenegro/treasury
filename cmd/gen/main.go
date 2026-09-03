// Command gen writes synthetic documents with ground truth.
//
//	gen sample [-out testdata] [-rotate 7] [-blur 1.0] [-jpeg 50]
//
// It writes NAME.png, NAME.json (the ttb application the decoder consumes),
// and NAME.truth.json (per-glyph boxes) for sample and sample_aug.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"os"
	"path/filepath"

	"treasury/internal/bitmap"
	"treasury/internal/render"
	"treasury/internal/synth"
	"treasury/ttb"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "sample" {
		fmt.Fprintln(os.Stderr, "usage: gen sample [-out dir] [-rotate deg] [-blur sigma] [-jpeg quality]")
		os.Exit(2)
	}
	fs := flag.NewFlagSet("sample", flag.ExitOnError)
	out := fs.String("out", "testdata", "output directory")
	rot := fs.Float64("rotate", 7, "rotation of the augmented copy in degrees")
	blur := fs.Float64("blur", 1.0, "Gaussian blur sigma of the augmented copy")
	jpg := fs.Int("jpeg", 50, "JPEG quality of the augmented copy")
	_ = fs.Parse(os.Args[2:])
	if err := run(*out, synth.Aug{RotateDeg: *rot, BlurSigma: *blur, JPEGQuality: *jpg}); err != nil {
		fmt.Fprintln(os.Stderr, "gen:", err)
		os.Exit(1)
	}
}

func run(dir string, aug synth.Aug) error {
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
