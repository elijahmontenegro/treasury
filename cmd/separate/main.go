// Command separate shows what text separation keeps and what it rejects.
//
//	separate -out docs/evidence/sep out/… real2/0003.png real2/0007.png
//
// For each image it writes one evidence picture: the label faded behind,
// what separation kept in black, and what it rejected in colour, red for a
// rule or a border, orange for a solid rather than a stroke, blue for a
// stroke of no single width, green for no contrast with its surround, grey
// for a mark with no line of type around it.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"treasury/internal/bitmap"
	"treasury/internal/preprocess"
)

var colours = map[string]color.RGBA{
	"a rule or a border":            {220, 40, 40, 255},
	"solid, not a stroke":           {240, 150, 30, 255},
	"stroke is not one width":       {50, 110, 230, 255},
	"no contrast with its surround": {40, 170, 80, 255},
	"no line of type around it":     {150, 150, 150, 255},
	"a bar of a barcode":            {90, 60, 30, 255},
	"too small":                     {200, 200, 200, 255},
	"taller than a letter":          {170, 60, 200, 255},
}

func main() {
	out := flag.String("out", "out/separation", "directory for the evidence images")
	list := flag.Int("list", 0, "also print this many rejected pieces, tallest first")
	flag.Parse()
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, path := range flag.Args() {
		if err := one(path, *out, *list); err != nil {
			fmt.Fprintln(os.Stderr, path, err)
		}
	}
}

func one(path, out string, list int) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return err
	}
	p := preprocess.Default()
	res, err := preprocess.Run(img, p)
	if err != nil {
		return err
	}
	g := res.Gray
	w, h := g.Rect.Dx(), g.Rect.Dy()
	rgba := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			v := uint8(200 + int(g.Pix[y*w+x])/5) // the label, faded
			rgba.SetRGBA(x, y, color.RGBA{v, v, v, 255})
		}
	}
	kept, rejected := 0, map[string]int{}
	for _, pc := range res.Pieces {
		if pc.Kept {
			kept++
			continue
		}
		rejected[pc.Reason]++
		c, ok := colours[pc.Reason]
		if !ok {
			c = color.RGBA{120, 120, 120, 255}
		}
		outline(rgba, pc.Box, c)
	}
	for y := range h {
		for x := range w {
			if res.Bin.Pix[y*w+x] != 0 {
				rgba.SetRGBA(x, y, color.RGBA{0, 0, 0, 255})
			}
		}
	}
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if err := bitmap.WritePNG(filepath.Join(out, name+".png"), rgba); err != nil {
		return err
	}
	fmt.Printf("%s: %d pieces kept, rejected %v\n", name, kept, rejected)
	if list > 0 {
		var bad []preprocess.Piece
		for _, pc := range res.Pieces {
			if !pc.Kept {
				bad = append(bad, pc)
			}
		}
		sort.Slice(bad, func(a, b int) bool { return bad[a].Box.Dy() > bad[b].Box.Dy() })
		for i, pc := range bad {
			if i >= list {
				break
			}
			fmt.Printf("   %v h%-3d dark=%-5v stroke %.1f ratio %.2f spread %.2f contrast %.2f ground %3.0f  %s\n",
				pc.Box, pc.Box.Dy(), pc.Dark, pc.Stroke, pc.Ratio, pc.Spread, pc.Contrast, pc.Ground, pc.Reason)
		}
	}
	return nil
}

func outline(dst *image.RGBA, r image.Rectangle, c color.RGBA) {
	r = r.Intersect(dst.Rect)
	for x := r.Min.X; x < r.Max.X; x++ {
		dst.SetRGBA(x, r.Min.Y, c)
		dst.SetRGBA(x, r.Max.Y-1, c)
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		dst.SetRGBA(r.Min.X, y, c)
		dst.SetRGBA(r.Max.X-1, y, c)
	}
}
