// Command decode runs the engine on one image.
//
//	decode [-debug dir] image.png
//
// Step 1 stops after preprocessing and region proposals and prints a summary.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"

	"treasury/internal/bitmap"
	"treasury/internal/preprocess"
	"treasury/internal/region"
)

func main() {
	debug := flag.String("debug", "", "write intermediate images to this directory")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: decode [-debug dir] image")
		os.Exit(2)
	}
	if err := run(flag.Arg(0), *debug); err != nil {
		fmt.Fprintln(os.Stderr, "decode:", err)
		os.Exit(1)
	}
}

type summary struct {
	AngleDeg      float64        `json:"angle_deg"`
	Scale         float64        `json:"scale"`
	Size          [2]int         `json:"size"`
	Lines         int            `json:"lines"`
	Regions       int            `json:"regions"`
	ByKind        map[string]int `json:"by_kind"`
	LowConfidence int            `json:"low_confidence"`
	GlareMask     bool           `json:"glare_mask"`
}

func run(path, debug string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	pre, err := preprocess.Run(img, preprocess.Default())
	if err != nil {
		return err
	}
	lines, regions := region.Propose(pre.Bin, pre.Glare, region.Default())
	s := summary{
		AngleDeg:  pre.AngleDeg,
		Scale:     pre.Scale,
		Size:      [2]int{pre.Gray.Rect.Dx(), pre.Gray.Rect.Dy()},
		Lines:     len(lines),
		Regions:   len(regions),
		ByKind:    map[string]int{},
		GlareMask: pre.Glare != nil,
	}
	for _, r := range regions {
		s.ByKind[r.Kind.String()]++
		if r.Glare > 0.3 {
			s.LowConfidence++
		}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s); err != nil {
		return err
	}
	if debug == "" {
		return nil
	}
	if err := bitmap.WritePNG(filepath.Join(debug, "gray.png"), pre.Gray); err != nil {
		return err
	}
	if err := bitmap.WritePNG(filepath.Join(debug, "binary.png"), pre.Bin.ToGray()); err != nil {
		return err
	}
	if pre.Glare != nil {
		if err := bitmap.WritePNG(filepath.Join(debug, "glare.png"), pre.Glare.ToGray()); err != nil {
			return err
		}
	}
	return bitmap.WritePNG(filepath.Join(debug, "regions.png"), overlay(pre.Gray, regions))
}

// overlay draws region outlines on the gray image: lines red, bands green, subs blue.
func overlay(g *image.Gray, regions []region.Region) *image.RGBA {
	out := image.NewRGBA(g.Rect)
	draw.Draw(out, out.Rect, g, g.Rect.Min, draw.Src)
	colors := map[region.Kind]color.RGBA{
		region.KindLine: {R: 220, A: 255},
		region.KindBand: {G: 160, A: 255},
		region.KindSub:  {B: 220, A: 255},
	}
	for _, r := range regions {
		c := colors[r.Kind]
		if r.Glare > 0.3 {
			c = color.RGBA{R: 255, G: 140, A: 255}
		}
		rect(out, r.Box, c)
	}
	return out
}

func rect(img *image.RGBA, r image.Rectangle, c color.RGBA) {
	for x := r.Min.X; x < r.Max.X; x++ {
		img.SetRGBA(x, r.Min.Y, c)
		img.SetRGBA(x, r.Max.Y-1, c)
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		img.SetRGBA(r.Min.X, y, c)
		img.SetRGBA(r.Max.X-1, y, c)
	}
}
