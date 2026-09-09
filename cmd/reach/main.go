// Command reach asks, for every claim of every label given, whether the
// claim's text can be read at all under each of several ways of reading
// (step 21c). It reports the best distance from an accepted spelling to
// the text, allowing anything either side of it to be free, which is a
// question about the reader and not about any verdict.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"

	"treasury/internal/ocr"
	"treasury/ttb"
	"treasury/verify"
)

var (
	models = flag.String("models", "out/models", "where the alternative models are")
	passes = flag.String("passes", "base,scale,rotate,server,serverrec", "which ways of reading to try")
)

type reader struct {
	name string
	r    *ocr.Reader
	turn bool // read the page turned a quarter, and map the boxes back
}

func main() {
	flag.Parse()
	want := map[string]bool{}
	for _, p := range strings.Split(*passes, ",") {
		want[p] = true
	}
	var rs []reader
	add := func(name string, p ocr.Params, turn bool) {
		r, err := ocr.New(p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
			return
		}
		rs = append(rs, reader{name: name, r: r, turn: turn})
	}
	base := ocr.Default()
	base.MaxSide = 1600
	if want["base"] {
		add("base", base, false)
	}
	if want["scale"] {
		p := base
		p.MaxSide = 3200
		add("scale", p, false)
	}
	if want["rotate"] {
		add("rotate", base, true)
	}
	if want["server"] {
		p := base
		if b, err := os.ReadFile(filepath.Join(*models, "det_server.onnx")); err == nil {
			p.Det, p.DetOutput = b, "sigmoid_11.tmp_0"
			add("server", p, false)
		} else {
			fmt.Fprintln(os.Stderr, "server detector:", err)
		}
	}
	if want["serverrec"] {
		p := base
		if b, err := os.ReadFile(filepath.Join(*models, "rec_server.onnx")); err == nil {
			p.Rec, p.RecOutput = b, "softmax_2.tmp_0"
			add("serverrec", p, false)
		} else {
			fmt.Fprintln(os.Stderr, "server recogniser:", err)
		}
	}
	defer func() {
		for _, x := range rs {
			x.r.Close()
		}
	}()

	enc := json.NewEncoder(os.Stdout)
	for _, path := range flag.Args() {
		if err := one(rs, enc, path); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
		}
	}
}

func one(rs []reader, enc *json.Encoder, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	img, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		return err
	}
	base := strings.TrimSuffix(path, filepath.Ext(path))
	b, err := os.ReadFile(base + ".json")
	if err != nil {
		return err
	}
	var exp ttb.Expected
	if err := json.Unmarshal(b, &exp); err != nil {
		return err
	}
	_, claims := ttb.Inputs(exp)

	out := map[string]map[string]float64{}
	regions := map[string]int{}
	texts := map[string]map[string]string{}
	for _, x := range rs {
		in := img
		if x.turn {
			in = turn90(img)
		}
		read, err := x.r.Read(context.Background(), in)
		if err != nil {
			return err
		}
		regions[x.name] = len(read)
		out[x.name] = map[string]float64{}
		texts[x.name] = map[string]string{}
		for _, c := range claims {
			d, t := verify.Reach(c, read)
			out[x.name][c.Name] = d
			texts[x.name][c.Name] = t
		}
	}
	return enc.Encode(struct {
		Label   string                        `json:"label"`
		Regions map[string]int                `json:"regions"`
		Reach   map[string]map[string]float64 `json:"reach"`
		Text    map[string]map[string]string  `json:"text"`
	}{filepath.Base(base), regions, out, texts})
}

// turn90 rotates the page a quarter turn, so a detector that proposes
// horizontal lines is offered the vertical ones as horizontal.
func turn90(src image.Image) image.Image {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dy(), b.Dx()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dst.Set(b.Max.Y-1-y, x-b.Min.X, src.At(x, y))
		}
	}
	return dst
}
