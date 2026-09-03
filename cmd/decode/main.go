// Command decode runs the engine on one image.
//
//	decode [-ttb expected.json | -ref "known text"] [-debug dir] image.png
//
// With a reference it learns the image's alphabet and reports the alignment;
// without one it stops after region proposals.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"

	"treasury/internal/alphabet"
	"treasury/internal/bitmap"
	"treasury/internal/preprocess"
	"treasury/internal/region"
	"treasury/internal/render"
	"treasury/ttb"
)

func main() {
	debug := flag.String("debug", "", "write intermediate images to this directory")
	ttbFile := flag.String("ttb", "", "label application JSON; uses the statutory warning as the reference")
	ref := flag.String("ref", "", "reference text known to appear in the image")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: decode [-ttb expected.json | -ref text] [-debug dir] image")
		os.Exit(2)
	}
	var spans []alphabet.Span
	if *ttbFile != "" {
		var exp ttb.Expected
		b, err := os.ReadFile(*ttbFile)
		if err == nil {
			err = json.Unmarshal(b, &exp)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "decode:", err)
			os.Exit(1)
		}
		*ref = ttb.Statute
		spans = []alphabet.Span{{Start: 0, End: ttb.HeaderLen}}
	}
	if err := run(flag.Arg(0), *ref, spans, *debug); err != nil {
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
	Alphabet      *alphaSummary  `json:"alphabet,omitempty"`
	Reason        string         `json:"reason,omitempty"`
}

type alphaSummary struct {
	Block         image.Rectangle       `json:"block"`
	Glyphs        int                   `json:"glyphs"`
	Matched       int                   `json:"matched"`
	Penalized     int                   `json:"penalized"`
	Unexplained   int                   `json:"unexplained"`
	Recovered     int                   `json:"recovered"`
	Spread        float64               `json:"spread"`
	Cost          float64               `json:"cost"`
	Band          int                   `json:"band"`
	Passes        int                   `json:"passes"`
	XHeight       float64               `json:"x_height"`
	CapHeight     float64               `json:"cap_height"`
	LetterGap     float64               `json:"letter_gap"`
	WordGap       float64               `json:"word_gap"`
	Characters    string                `json:"characters"`
	Emphasis      map[int]string        `json:"emphasis"`
	Rows          []alphabet.RowQuality `json:"rows"`
	StepsByKind   map[string]int        `json:"steps_by_kind"`
}

func run(path, ref string, spans []alphabet.Span, debug string) error {
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
	var alpha *alphabet.Alphabet
	if ref != "" {
		alpha, err = alphabet.Find(lines, pre.Bin, pre.Gray, ref, spans, alphabet.DefaultOptions())
		switch {
		case errors.Is(err, alphabet.ErrNoBlock):
			s.Reason = "no_alphabet"
		case err != nil:
			return err
		default:
			s.Alphabet = summarize(alpha)
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
	if err := writeDebug(debug, pre, regions); err != nil {
		return err
	}
	if alpha == nil {
		return nil
	}
	faces, _ := render.Bundled()
	if err := bitmap.WritePNG(filepath.Join(debug, "alphabet.png"), alphabet.Sheet(alpha, faces)); err != nil {
		return err
	}
	return bitmap.WritePNG(filepath.Join(debug, "block.png"), blockOverlay(pre.Gray, alpha))
}

func summarize(a *alphabet.Alphabet) *alphaSummary {
	chars := make([]rune, 0, len(a.Samples))
	for r := range a.Samples {
		chars = append(chars, r)
	}
	sortRunes(chars)
	out := &alphaSummary{
		Block: a.Block.Box, Glyphs: len(a.Block.Glyphs), Matched: a.Matched, Penalized: a.Penalized,
		Unexplained: a.Unexplained, Recovered: a.Recovered,
		Spread: a.Spread, Cost: a.Cost, Band: a.Band, Passes: a.Passes,
		XHeight: a.XHeight, CapHeight: a.CapHeight, LetterGap: a.LetterGap, WordGap: a.WordGap,
		Characters: string(chars), Emphasis: map[int]string{}, Rows: a.Rows, StepsByKind: map[string]int{},
	}
	for span, m := range a.Emphasis {
		rs := make([]rune, 0, len(m))
		for r := range m {
			rs = append(rs, r)
		}
		sortRunes(rs)
		out.Emphasis[span] = string(rs)
	}
	for _, st := range a.Path {
		out.StepsByKind[st.Kind.String()]++
	}
	return out
}

func sortRunes(rs []rune) {
	for i := 1; i < len(rs); i++ {
		for j := i; j > 0 && rs[j] < rs[j-1]; j-- {
			rs[j], rs[j-1] = rs[j-1], rs[j]
		}
	}
}

func writeDebug(debug string, pre *preprocess.Result, regions []region.Region) error {
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
	type box struct {
		Kind string          `json:"kind"`
		Line int             `json:"line"`
		Box  image.Rectangle `json:"box"`
	}
	boxes := make([]box, 0, len(regions))
	for _, r := range regions {
		boxes = append(boxes, box{Kind: r.Kind.String(), Line: r.Line, Box: r.Box})
	}
	b, err := json.MarshalIndent(boxes, "", " ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(debug, "regions.json"), b, 0o644); err != nil {
		return err
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

// blockOverlay draws the block's glyph boxes: matched green, emphasis blue,
// penalized red, and the block box in black.
func blockOverlay(g *image.Gray, a *alphabet.Alphabet) *image.RGBA {
	out := image.NewRGBA(g.Rect)
	draw.Draw(out, out.Rect, g, g.Rect.Min, draw.Src)
	kind := make([]alphabet.Kind, len(a.Block.Glyphs))
	for _, st := range a.Path {
		switch st.Kind {
		case alphabet.Match, alphabet.Merge, alphabet.Insert:
			kind[st.Glyph] = st.Kind
		case alphabet.Split:
			kind[st.Glyph], kind[st.Glyph+1] = st.Kind, st.Kind
		}
	}
	for gi, gl := range a.Block.Glyphs {
		c := color.RGBA{R: 220, A: 255}
		switch kind[gi] {
		case alphabet.Match:
			c = color.RGBA{G: 170, A: 255}
		case alphabet.Merge, alphabet.Split:
			c = color.RGBA{R: 230, G: 140, A: 255}
		}
		rect(out, gl.Box.Inset(-1), c)
	}
	rect(out, a.Block.Box.Inset(-6), color.RGBA{A: 255})
	return out
}

func rect(img *image.RGBA, r image.Rectangle, c color.RGBA) {
	r = r.Intersect(img.Rect)
	if r.Empty() {
		return
	}
	for x := r.Min.X; x < r.Max.X; x++ {
		img.SetRGBA(x, r.Min.Y, c)
		img.SetRGBA(x, r.Max.Y-1, c)
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		img.SetRGBA(r.Min.X, y, c)
		img.SetRGBA(r.Max.X-1, y, c)
	}
}
