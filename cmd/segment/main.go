// Command segment measures the segmentation stage on its own, against the
// glyphs the generator drew rather than through any verdict (amendment step
// 18a).
//
//	segment -n 40 -seed 21 -aug
//
// For every character drawn, it asks what the pipeline made of it: one
// component of its own, a component shared with its neighbours, several
// components, or no ink kept at all. It reports the rates by x-height, by
// polarity, by how busy the ground under the glyph is, by whether the glyph
// belongs to the brand or to the body, and by whether the label went through
// the channel.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"math"
	"math/rand"
	"os"
	"sort"

	"treasury/internal/preprocess"
	"treasury/internal/region"
	"treasury/internal/render"
	"treasury/internal/synth"
	"treasury/ttb"
)

type outcome struct {
	Label    int     `json:"label"`
	Char     string  `json:"char"`
	Claim    string  `json:"claim"`
	Kind     string  `json:"kind"` // its own, fused, split, dropped
	XHeight  float64 `json:"x_height"`
	Busy     float64 `json:"busy"`
	Inverted bool    `json:"inverted"`
	Brand    bool    `json:"brand"`
	Channel  bool    `json:"channel"`
}

func main() {
	n := flag.Int("n", 40, "labels")
	seed := flag.Int64("seed", 21, "seed; distinct from the corpus so nothing is reused")
	aug := flag.Bool("aug", false, "put the labels through the channel")
	flag.Parse()
	faces, err := render.Bundled()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	pool := ttb.NewFacePool(faces)
	rng := rand.New(rand.NewSource(*seed))
	enc := json.NewEncoder(os.Stdout)
	for i := 0; i < *n; {
		doc, _, printed := ttb.Generate(rng, pool, 0)
		img, truth, err := synth.Render(doc, faces)
		if err != nil {
			continue
		}
		channel := false
		if *aug {
			a := synth.Aug{BlurSigma: 0.4 + rng.Float64(), JPEGQuality: 40 + rng.Intn(50),
				RotateDeg: rng.Float64()*6 - 3, Brightness: rng.Float64()*30 - 15, Seed: rng.Int63()}
			g2, t2, err := synth.Augment(img, truth, a)
			if err != nil {
				continue
			}
			img, truth, channel = g2, t2, true
		}
		i++
		for _, o := range one(i, img, truth, printed, channel) {
			if err := enc.Encode(o); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		}
	}
}

// one measures a single label.
func one(label int, img *image.Gray, truth *synth.Truth, printed ttb.Printed, channel bool) []outcome {
	pre, err := preprocess.Run(img, preprocess.Default())
	if err != nil {
		return nil
	}
	lines, _ := region.Propose(pre.Bin, pre.Glare, region.Default())
	var comps []image.Rectangle
	for _, ln := range lines {
		for _, c := range ln.Comps {
			comps = append(comps, c.Box)
		}
	}
	// Where the ground is busy, from what the separation measured.
	busyAt := func(b image.Rectangle) float64 {
		for _, p := range pre.Pieces {
			if p.Box.Overlaps(b) {
				return p.Busy
			}
		}
		return 0
	}
	// The truth is in the image the pipeline was given; the pipeline
	// resized it and turned it by the skew it estimated.
	w, h := float64(img.Rect.Dx()), float64(img.Rect.Dy())
	cx, cy := w*pre.Scale/2, h*pre.Scale/2
	rad := -pre.AngleDeg * math.Pi / 180
	mapBox := func(b image.Rectangle) image.Rectangle {
		pts := [][2]float64{{float64(b.Min.X), float64(b.Min.Y)}, {float64(b.Max.X), float64(b.Max.Y)},
			{float64(b.Min.X), float64(b.Max.Y)}, {float64(b.Max.X), float64(b.Min.Y)}}
		minX, minY := math.Inf(1), math.Inf(1)
		maxX, maxY := math.Inf(-1), math.Inf(-1)
		for _, p := range pts {
			x, y := p[0]*pre.Scale-cx, p[1]*pre.Scale-cy
			rx := x*math.Cos(rad) - y*math.Sin(rad) + cx
			ry := x*math.Sin(rad) + y*math.Cos(rad) + cy
			minX, minY = math.Min(minX, rx), math.Min(minY, ry)
			maxX, maxY = math.Max(maxX, rx), math.Max(maxY, ry)
		}
		return image.Rect(int(minX), int(minY), int(math.Ceil(maxX)), int(math.Ceil(maxY)))
	}
	type glyph struct {
		box   image.Rectangle
		char  string
		claim string
	}
	var glyphs []glyph
	for _, g := range truth.Glyphs {
		if g.Char == " " {
			continue
		}
		b := mapBox(g.Box)
		if b.Dx() < 1 || b.Dy() < 1 {
			continue
		}
		glyphs = append(glyphs, glyph{b, g.Char, g.Claim})
	}
	// A component covers a glyph when they share a third of the smaller
	// of the two areas: less is a neighbour's overhang.
	covers := func(c, g image.Rectangle) bool {
		in := c.Intersect(g)
		if in.Empty() {
			return false
		}
		a := in.Dx() * in.Dy()
		m := min(c.Dx()*c.Dy(), g.Dx()*g.Dy())
		return m > 0 && float64(a) >= 0.33*float64(m)
	}
	byGlyph := make([][]int, len(glyphs))
	byComp := make([][]int, len(comps))
	for ci, c := range comps {
		for gi, g := range glyphs {
			if covers(c, g.box) {
				byGlyph[gi] = append(byGlyph[gi], ci)
				byComp[ci] = append(byComp[ci], gi)
			}
		}
	}
	var heights []float64
	for _, g := range glyphs {
		heights = append(heights, float64(g.box.Dy()))
	}
	sort.Float64s(heights)
	var out []outcome
	for gi, g := range glyphs {
		kind := "dropped"
		switch len(byGlyph[gi]) {
		case 0:
		case 1:
			if len(byComp[byGlyph[gi][0]]) > 1 {
				kind = "fused"
			} else {
				kind = "its own"
			}
		default:
			kind = "split"
		}
		out = append(out, outcome{
			Label: label, Char: g.char, Claim: g.claim, Kind: kind,
			XHeight: float64(g.box.Dy()), Busy: busyAt(g.box),
			Inverted: printed.Inverted, Brand: g.claim == "brand", Channel: channel,
		})
	}
	return out
}
