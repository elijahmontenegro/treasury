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
	"encoding/json"
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
	stats := flag.Bool("stats", false, "print one line of JSON per image: what the population is made of")
	flag.Parse()
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, path := range flag.Args() {
		if err := one(path, *out, *list, *stats); err != nil {
			fmt.Fprintln(os.Stderr, path, err)
		}
	}
}

func one(path, out string, list int, stats bool) error {
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
	p.Separate = true // this command exists to show the separation
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
	if stats {
		return report(name, res, img)
	}
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

// report prints what one label is made of, for drawing a corpus from the
// population rather than from assumption.
func report(name string, res *preprocess.Result, src image.Image) error {
	type out struct {
		Label      string    `json:"label"`
		LongSide   int       `json:"long_side"`
		Kept       int       `json:"kept"`
		LightShare float64   `json:"light_share"`  // kept pieces that are light on dark
		Contrast   []float64 `json:"contrast"`     // p10, p50, p90 of ink against its own ring
		Ground     []float64 `json:"ground"`       // p10, p50, p90 of the ring's grey, dark-ink pieces
		LightGnd   []float64 `json:"light_ground"` // the same for light-ink pieces
		Busy       []float64 `json:"busy"`         // p50, p90 of the ring's own variation: text over something structured
		Height     []float64 `json:"height"`       // p10, p50, p90 of kept piece height in pixels
		Stroke     []float64 `json:"stroke"`
		Barcode    int       `json:"barcode_bars"`
		Rules      int       `json:"rules"`
	}
	o := out{Label: name, LongSide: max(src.Bounds().Dx(), src.Bounds().Dy())}
	var contrast, ground, lightGnd, busy, height, strokes []float64
	light := 0
	for _, pc := range res.Pieces {
		switch pc.Reason {
		case "a bar of a barcode":
			o.Barcode++
		case "a rule or a border":
			o.Rules++
		}
		if !pc.Kept {
			continue
		}
		o.Kept++
		if !pc.Dark {
			light++
			lightGnd = append(lightGnd, pc.Ground)
		} else {
			ground = append(ground, pc.Ground)
		}
		contrast = append(contrast, pc.Contrast)
		busy = append(busy, pc.Busy)
		height = append(height, float64(pc.Box.Dy()))
		strokes = append(strokes, pc.Stroke)
	}
	if o.Kept > 0 {
		o.LightShare = float64(light) / float64(o.Kept)
	}
	o.Contrast = quantiles(contrast, 0.1, 0.5, 0.9)
	o.Ground = quantiles(ground, 0.1, 0.5, 0.9)
	o.LightGnd = quantiles(lightGnd, 0.1, 0.5, 0.9)
	o.Busy = quantiles(busy, 0.5, 0.9)
	o.Height = quantiles(height, 0.1, 0.5, 0.9)
	o.Stroke = quantiles(strokes, 0.1, 0.5, 0.9)
	b, err := json.Marshal(o)
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

func quantiles(v []float64, qs ...float64) []float64 {
	if len(v) == 0 {
		return make([]float64, len(qs))
	}
	sort.Float64s(v)
	out := make([]float64, 0, len(qs))
	for _, q := range qs {
		i := int(q * float64(len(v)-1))
		out = append(out, v[i])
	}
	return out
}
