// Command decode runs the engine on one image.
//
//	decode [-ttb expected.json | -ref "known text"] [-debug dir] image.png
//
// With a reference it learns the image's alphabet and reports the alignment;
// without one it stops after region proposals.
package main

import (
	"context"
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
	"treasury/internal/buildid"
	"treasury/internal/preprocess"
	"treasury/internal/region"
	"treasury/internal/render"
	"treasury/ttb"
	"treasury/verify"
)

func main() {
	version := flag.Bool("version", false, "print what this binary is, and the weights it carries")
	debug := flag.String("debug", "", "write intermediate images to this directory")
	ttbFile := flag.String("ttb", "", "label application JSON; uses the statutory warning as the reference")
	ref := flag.String("ref", "", "reference text known to appear in the image")
	trace := flag.Bool("trace", false, "print shape-class violations found while learning to stderr")
	flag.Parse()
	if *version {
		fmt.Println(buildid.Get())
		return
	}
	if *trace {
		alphabet.Tracef = func(format string, args ...any) { fmt.Fprintln(os.Stderr, fmt.Sprintf(format, args...)) }
	}
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: decode [-ttb expected.json | -ref text] [-debug dir] image")
		os.Exit(2)
	}
	var err error
	if *ttbFile != "" {
		err = runEngine(flag.Arg(0), *ttbFile, *debug)
	} else {
		err = run(flag.Arg(0), *ref, nil, *debug)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "decode:", err)
		os.Exit(1)
	}
}

// runEngine verifies a label application end to end and prints the result.
func runEngine(path, ttbFile, debug string) error {
	var exp ttb.Expected
	b, err := os.ReadFile(ttbFile)
	if err == nil {
		err = json.Unmarshal(b, &exp)
	}
	if err != nil {
		return err
	}
	img, err := load(path)
	if err != nil {
		return err
	}
	eng, err := verify.New(verify.Options{})
	if err != nil {
		return err
	}
	refs, claims := ttb.Inputs(exp)
	res, err := eng.Verify(context.Background(), img, refs, claims)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(res); err != nil {
		return err
	}
	if debug == "" {
		return nil
	}
	for _, v := range res.Claims {
		if v.Evidence == nil {
			continue
		}
		if v.Evidence.Crop != nil {
			if err := bitmap.WritePNG(filepath.Join(debug, "claims", v.Claim+"_region.png"), v.Evidence.Crop); err != nil {
				return err
			}
		}
		if p := v.Evidence.Codeword; p.Bin != nil {
			if err := bitmap.WritePNG(filepath.Join(debug, "claims", v.Claim+"_codeword.png"), p.Bin.ToGray()); err != nil {
				return err
			}
			if p.Gray != nil {
				if err := bitmap.WritePNG(filepath.Join(debug, "claims", v.Claim+"_codeword_gray.png"), p.Gray); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func load(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return img, nil
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
	Clusters      []string       `json:"clusters,omitempty"` // candidate blocks when none aligned
}

type alphaSummary struct {
	Block       image.Rectangle       `json:"block"`
	Glyphs      int                   `json:"glyphs"`
	Matched     int                   `json:"matched"`
	Penalized   int                   `json:"penalized"`
	Unexplained int                   `json:"unexplained"`
	Recovered   int                   `json:"recovered"`
	Spread      float64               `json:"spread"`
	Violations  float64               `json:"violations"`
	Cost        float64               `json:"cost"`
	Band        int                   `json:"band"`
	Passes      int                   `json:"passes"`
	XHeight     float64               `json:"x_height"`
	CapHeight   float64               `json:"cap_height"`
	LetterGap   float64               `json:"letter_gap"`
	WordGap     float64               `json:"word_gap"`
	Characters  string                `json:"characters"`
	Emphasis    map[int]string        `json:"emphasis"`
	Rows        []alphabet.RowQuality `json:"rows"`
	StepsByKind map[string]int        `json:"steps_by_kind"`
}

func run(path, ref string, spans []alphabet.Span, debug string) error {
	img, err := load(path)
	if err != nil {
		return err
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
			for _, cl := range alphabet.Locate(lines, 3) {
				n := 0
				for _, i := range cl {
					n += len(lines[i].Comps)
				}
				s.Clusters = append(s.Clusters, fmt.Sprintf("%d lines, %d glyphs", len(cl), n))
			}
		case err != nil:
			return err
		default:
			s.Alphabet = summarize(alpha)
			if !alpha.OK(0.10, 0.1, 0.30) {
				s.Reason = "alphabet_rejected"
			}
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
		Spread: a.Spread, Violations: a.ViolationFraction(), Cost: a.Cost, Band: a.Band, Passes: a.Passes,
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
		Kind  string            `json:"kind"`
		Line  int               `json:"line"`
		Box   image.Rectangle   `json:"box"`
		Comps []image.Rectangle `json:"comps,omitempty"`
	}
	boxes := make([]box, 0, len(regions))
	for _, r := range regions {
		var comps []image.Rectangle
		for _, c := range r.Comps {
			comps = append(comps, c.Box)
		}
		boxes = append(boxes, box{Kind: r.Kind.String(), Line: r.Line, Box: r.Box, Comps: comps})
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
		case alphabet.Match, alphabet.Merge, alphabet.Merge3, alphabet.Insert:
			kind[st.Glyph] = st.Kind
		case alphabet.Rejoin:
			kind[st.Glyph], kind[st.Glyph+1] = st.Kind, st.Kind
		case alphabet.Split:
			kind[st.Glyph], kind[st.Glyph+1] = st.Kind, st.Kind
		case alphabet.Split3:
			kind[st.Glyph], kind[st.Glyph+1], kind[st.Glyph+2] = st.Kind, st.Kind, st.Kind
		}
	}
	for gi, gl := range a.Block.Glyphs {
		c := color.RGBA{R: 220, A: 255}
		switch kind[gi] {
		case alphabet.Match:
			c = color.RGBA{G: 170, A: 255}
		case alphabet.Merge, alphabet.Merge3, alphabet.Split, alphabet.Split3, alphabet.Rejoin:
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
