// Package spell composes codewords for expected text out of the learned
// alphabet, synthesizing from the nearest bundled face what the reference
// did not teach.
package spell

import (
	"fmt"
	"image"
	"math"
	"unicode"

	"treasury/internal/alphabet"
	"treasury/internal/bitcode"
	"treasury/internal/bitmap"
	"treasury/internal/encoder"
	"treasury/internal/render"
)

// Params records how a codeword was spelled, for evidence.
type Params struct {
	Casing      string `json:"casing,omitempty"`
	Synthesized string `json:"synthesized,omitempty"` // characters synthesized from Face
	Emphasis    string `json:"emphasis,omitempty"`    // characters taken from an emphasis pool
	Face        string `json:"face,omitempty"`
}

// Codeword is spelled text with its code.
type Codeword struct {
	Text   string
	Value  string
	Code   bitcode.Code
	Params Params
	Patch  encoder.Patch
}

// piece is one glyph ready to place: Box relative to the pen origin on the
// baseline, and its frame code for glyph-wise decoding.
type piece struct {
	bin    *bitmap.Bitmap
	gray   *image.Gray
	box    image.Rectangle
	code   bitcode.Code
	source byte // 'l' learned, 'e' emphasis, 's' synthesized
}

// Speller spells with one alphabet.
type Speller struct {
	A        *alphabet.Alphabet
	Face     *render.Face // nearest bundled face
	Enc      encoder.Encoder
	GlyphEnc encoder.Encoder

	letterGap, wordGap int
	medoids            map[alphabet.Key]alphabet.Glyph
	synth              map[rune]piece
}

// New picks the nearest face and prepares the medoid sample per character.
func New(a *alphabet.Alphabet, faces []*render.Face, enc encoder.Encoder) *Speller {
	glyphEnc := a.Block.Encoder()
	face, _ := NearestFace(a, faces, glyphEnc)
	s := &Speller{A: a, Face: face, Enc: enc, GlyphEnc: glyphEnc, medoids: map[alphabet.Key]alphabet.Glyph{}, synth: map[rune]piece{}}
	s.letterGap = int(math.Round(a.LetterGap))
	s.wordGap = int(math.Round(a.WordGap))
	if s.wordGap <= s.letterGap {
		s.wordGap = s.letterGap + int(math.Round(0.4*a.XHeight))
	}
	for r, samples := range a.Samples {
		s.medoids[alphabet.Key{R: r, Span: -1}] = medoid(samples)
	}
	for span, m := range a.Emphasis {
		for r, samples := range m {
			s.medoids[alphabet.Key{R: r, Span: span}] = medoid(samples)
		}
	}
	return s
}

// medoid is the sample nearest all the others by frame code; samples that
// are components of their own are preferred over ones cut from merged pairs.
func medoid(samples []alphabet.Glyph) alphabet.Glyph {
	pool := samples[:0:0]
	for _, g := range samples {
		if !g.Derived {
			pool = append(pool, g)
		}
	}
	if len(pool) == 0 {
		pool = samples
	}
	best, bestSum := pool[0], math.MaxInt
	for i, g := range pool {
		sum := 0
		for j, h := range pool {
			if i != j {
				sum += bitcode.Distance(g.Code, h.Code)
			}
		}
		if sum < bestSum {
			best, bestSum = g, sum
		}
	}
	return best
}

// NearestFace scores each face by synthesizing the learned lowercase letters
// at the alphabet's x-height and measuring their frame codes against the
// learned centroids.
func NearestFace(a *alphabet.Alphabet, faces []*render.Face, glyphEnc encoder.Encoder) (*render.Face, float64) {
	var best *render.Face
	bestD := math.Inf(1)
	for _, f := range faces {
		sum, n := 0.0, 0
		for r := range a.Samples {
			if !unicode.IsLower(r) {
				continue
			}
			cen, ok := a.Centroid[alphabet.Key{R: r, Span: -1}]
			if !ok {
				continue
			}
			sg, err := f.Glyph(r, a.XHeight, false)
			if err != nil {
				continue
			}
			sum += encoder.NormalizedDistance(frameCode(sg, a.XHeight, glyphEnc), cen, glyphEnc.Bits())
			n++
		}
		if n > 0 && sum/float64(n) < bestD {
			best, bestD = f, sum/float64(n)
		}
	}
	if best == nil && len(faces) > 0 {
		best = faces[0]
	}
	return best, bestD
}

// frameCode encodes a synthesized glyph the way the alphabet encodes its own.
func frameCode(sg render.Synth, xh float64, enc encoder.Encoder) bitcode.Code {
	side := int(math.Ceil(2.2 * xh))
	canvas := bitmap.New(sg.Box.Dx()+2*side, 4*side)
	baseline := 2 * side
	box := sg.Box.Add(image.Pt(side, baseline))
	blit(canvas, sg.Bin, box.Min)
	return enc.Encode(alphabet.Frame(canvas, box, baseline, xh))
}

func blit(dst *bitmap.Bitmap, src *bitmap.Bitmap, at image.Point) {
	for y := range src.H {
		for x := range src.W {
			if src.Pix[y*src.W+x] != 0 {
				dst.Set(at.X+x, at.Y+y, 1)
			}
		}
	}
}

// piece returns the glyph to place for r: the learned medoid, else an
// emphasis medoid, else a synthesized glyph.
func (s *Speller) piece(r rune) (piece, error) {
	if g, ok := s.medoids[alphabet.Key{R: r, Span: -1}]; ok {
		return fromGlyph(g, 'l'), nil
	}
	for span := range s.A.Emphasis {
		if g, ok := s.medoids[alphabet.Key{R: r, Span: span}]; ok {
			return fromGlyph(g, 'e'), nil
		}
	}
	if p, ok := s.synth[r]; ok {
		return p, nil
	}
	if s.Face == nil {
		return piece{}, fmt.Errorf("spell: no face to synthesize %q", r)
	}
	byCap := (unicode.IsUpper(r) || unicode.IsDigit(r)) && s.A.CapHeight > 0
	target := s.A.XHeight
	if byCap {
		target = s.A.CapHeight
	}
	sg, err := s.Face.Glyph(r, target, byCap)
	if err != nil {
		return piece{}, err
	}
	p := piece{bin: sg.Bin, gray: sg.Gray, box: sg.Box, code: frameCode(sg, s.A.XHeight, s.GlyphEnc), source: 's'}
	s.synth[r] = p
	return p, nil
}

func fromGlyph(g alphabet.Glyph, source byte) piece {
	return piece{bin: g.Bin, gray: g.Gray, box: g.Box.Sub(image.Pt(g.Box.Min.X, g.Baseline)), code: g.Code, source: source}
}

// Target returns the glyph-wise codeword for text: a frame code per
// non-space character, plus the vertical ink extent of the composite
// relative to its baseline.
func (s *Speller) Target(text string) (t alphabet.Target, top, bottom int, err error) {
	t.Text = []rune(text)
	t.Codes = make([]bitcode.Code, len(t.Text))
	top, bottom = math.MaxInt, math.MinInt
	for i, r := range t.Text {
		if r == ' ' {
			continue
		}
		p, err := s.piece(r)
		if err != nil {
			return alphabet.Target{}, 0, 0, err
		}
		t.Codes[i] = p.code
		top = min(top, p.box.Min.Y)
		bottom = max(bottom, p.box.Max.Y)
	}
	run := func(c, n int) bitcode.Code {
		if c+n > len(t.Text) {
			return nil
		}
		ps := make([]piece, 0, n)
		for i := c; i < c+n; i++ {
			if t.Text[i] == ' ' {
				return nil
			}
			p, err := s.piece(t.Text[i])
			if err != nil {
				return nil
			}
			ps = append(ps, p)
		}
		return s.runCode(ps)
	}
	t.Pair = func(c int) bitcode.Code { return run(c, 2) }
	t.Triple = func(c int) bitcode.Code { return run(c, 3) }
	return t, top, bottom, nil
}

// runCode frames pieces set side by side at the label's letter gap, as a
// touching run in the image would be framed.
func (s *Speller) runCode(ps []piece) bitcode.Code {
	xh := s.A.XHeight
	side := int(math.Ceil(2.2 * xh))
	w := 0
	for i, p := range ps {
		if i > 0 {
			w += s.letterGap
		}
		w += p.box.Dx()
	}
	canvas := bitmap.New(w+2*side, 4*side)
	baseline := 2 * side
	x := side
	box := image.Rectangle{}
	for i, p := range ps {
		at := image.Pt(x, baseline+p.box.Min.Y)
		blit(canvas, p.bin, at)
		r := image.Rect(at.X, at.Y, at.X+p.box.Dx(), at.Y+p.box.Dy())
		if i == 0 {
			box = r
		} else {
			box = box.Union(r)
		}
		x += p.box.Dx() + s.letterGap
	}
	return s.GlyphEnc.Encode(alphabet.Frame(canvas, box, baseline, xh))
}

// Spell composes text on one baseline with the label's own letter and word
// gaps, tight to the ink like an encoded region, and encodes it.
func (s *Speller) Spell(text, value string) (Codeword, error) {
	return s.spell(text, value, float64(s.letterGap), float64(s.wordGap))
}

// SpellFit composes text so that its aspect matches a region of width×height
// px: the composite lives at the reference's glyph size, so the region's
// width is first brought to that scale through the ratio of ink heights,
// then the label's gap proportions are scaled to fill it.
func (s *Speller) SpellFit(text, value string, width, height int) (Codeword, error) {
	letters, words, glyphs := 0, 0, 0
	top, bottom := math.MaxInt, math.MinInt
	prev := rune(-1)
	for _, r := range text {
		if r == ' ' {
			words++
		} else {
			p, err := s.piece(r)
			if err != nil {
				return Codeword{}, err
			}
			glyphs += p.box.Dx()
			top = min(top, p.box.Min.Y)
			bottom = max(bottom, p.box.Max.Y)
			if prev >= 0 && prev != ' ' {
				letters++
			}
		}
		prev = r
	}
	if glyphs == 0 || height <= 0 || bottom <= top {
		return s.spell(text, value, 0, 0)
	}
	scale := float64(bottom-top) / float64(height)
	budget := float64(width)*scale - float64(glyphs)
	if budget <= 0 || letters+words == 0 {
		return s.spell(text, value, 0, 0)
	}
	ratio := 3.0
	if s.letterGap > 0 {
		ratio = float64(s.wordGap) / float64(s.letterGap)
	}
	unit := budget / (float64(letters) + ratio*float64(words))
	return s.spell(text, value, unit, unit*ratio)
}

func (s *Speller) spell(text, value string, letterGap, wordGap float64) (Codeword, error) {
	type placed struct {
		p piece
		x int
	}
	var ps []placed
	pen := 0.0
	prev := rune(-1)
	var synthesized, emphasis []rune
	for _, r := range text {
		if r == ' ' {
			pen += wordGap
			prev = r
			continue
		}
		p, err := s.piece(r)
		if err != nil {
			return Codeword{}, err
		}
		switch p.source {
		case 's':
			synthesized = append(synthesized, r)
		case 'e':
			emphasis = append(emphasis, r)
		}
		if prev >= 0 && prev != ' ' {
			pen += letterGap
		}
		ps = append(ps, placed{p, int(math.Round(pen))})
		pen += float64(p.box.Dx())
		prev = r
	}
	if len(ps) == 0 {
		return Codeword{}, fmt.Errorf("spell: nothing to spell in %q", text)
	}
	top, bottom := math.MaxInt, math.MinInt
	for _, pl := range ps {
		top = min(top, pl.p.box.Min.Y)
		bottom = max(bottom, pl.p.box.Max.Y)
	}
	last := ps[len(ps)-1]
	w, h := last.x+last.p.box.Dx(), bottom-top
	bin := bitmap.New(w, h)
	gray := image.NewGray(image.Rect(0, 0, w, h))
	for i := range gray.Pix {
		gray.Pix[i] = 255
	}
	baseline := -top
	for _, pl := range ps {
		at := image.Pt(pl.x, baseline+pl.p.box.Min.Y)
		blit(bin, pl.p.bin, at)
		for y := range pl.p.bin.H {
			for x := range pl.p.bin.W {
				gx, gy := at.X+x, at.Y+y
				if gx < 0 || gy < 0 || gx >= w || gy >= h {
					continue
				}
				v := pl.p.gray.Pix[pl.p.gray.PixOffset(pl.p.gray.Rect.Min.X+x, pl.p.gray.Rect.Min.Y+y)]
				if v < gray.Pix[gy*w+gx] {
					gray.Pix[gy*w+gx] = v
				}
			}
		}
	}
	patch := encoder.Patch{Bin: bin, Gray: gray}
	cw := Codeword{Text: text, Value: value, Code: s.Enc.Encode(patch), Patch: patch}
	cw.Params.Synthesized = string(synthesized)
	cw.Params.Emphasis = string(emphasis)
	if len(synthesized) > 0 && s.Face != nil {
		cw.Params.Face = s.Face.Name
	}
	return cw, nil
}
