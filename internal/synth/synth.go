// Package synth renders documents whose every glyph position is known, and
// pushes them through channel augmentation while carrying the truth along.
package synth

import (
	"fmt"
	"image"
	"image/draw"
	"math"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"

	"treasury/internal/imgops"
	"treasury/internal/render"
)

// Span is a half-open rune range [Start, End) of a block's text.
type Span struct {
	Start, End int
}

// Item is one line of text placed by its baseline.
type Item struct {
	Text   string
	Face   string
	Px     float64
	X, Y   int  // left edge (or centre when Center) and baseline
	Center bool // X is the centre of the text
	Claim  string
}

// Block is wrapped text; Heavy spans are drawn in HeavyFace.
type Block struct {
	Text      string
	Heavy     []Span
	Face      string
	HeavyFace string
	Px        float64
	X, Y      int // left edge and first baseline
	Width     int
	Leading   float64 // line pitch as a multiple of Px; 0 means 1.3
	Claim     string
	Quarters  int // quarter turns clockwise; a block set vertically along a side. X, Y then place the rotated block's top-left corner
}

// Document is what Render draws.
type Document struct {
	W, H   int
	Items  []Item
	Blocks []Block
}

// Glyph is the truth for one drawn character.
type Glyph struct {
	Char  string          `json:"char"`
	Box   image.Rectangle `json:"box"`
	Claim string          `json:"claim"`
	Heavy bool            `json:"heavy"`
	Line  int             `json:"line"`
}

// Truth is what a rendered document knows about itself.
type Truth struct {
	W        int     `json:"w"`
	H        int     `json:"h"`
	AngleDeg float64 `json:"angle_deg"`
	Glyphs   []Glyph `json:"glyphs"`
}

// Render draws doc in black on white.
func Render(doc Document, faces []*render.Face) (*image.Gray, *Truth, error) {
	img := image.NewGray(image.Rect(0, 0, doc.W, doc.H))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	t := &Truth{W: doc.W, H: doc.H}
	for _, it := range doc.Items {
		face, err := faceAt(faces, it.Face, it.Px)
		if err != nil {
			return nil, nil, err
		}
		x := it.X
		if it.Center {
			x -= measure(face, it.Text) / 2
		}
		drawRun(img, face, it.Text, x, it.Y, it.Claim, false, 0, t)
	}
	for _, b := range doc.Blocks {
		if b.Quarters%4 == 0 {
			if err := drawBlock(img, b, faces, t, 0, 0); err != nil {
				return nil, nil, err
			}
			continue
		}
		// Draw the block upright on its own canvas, rotate it, and lay it
		// at (X, Y); its glyph boxes turn with it.
		pad := int(b.Px) + 4
		tmp := image.NewGray(image.Rect(0, 0, b.Width+2*pad, b.Width+2*pad))
		for i := range tmp.Pix {
			tmp.Pix[i] = 255
		}
		var tt Truth
		if err := drawBlock(tmp, b, faces, &tt, -b.X+pad, -b.Y+pad+int(b.Px)); err != nil {
			return nil, nil, err
		}
		if len(tt.Glyphs) == 0 {
			continue
		}
		ink := tt.Glyphs[0].Box
		for _, g := range tt.Glyphs[1:] {
			ink = ink.Union(g.Box)
		}
		ink = ink.Inset(-pad / 2).Intersect(tmp.Rect)
		crop := image.NewGray(image.Rect(0, 0, ink.Dx(), ink.Dy()))
		draw.Draw(crop, crop.Rect, tmp, ink.Min, draw.Src)
		rot := imgops.Rotate90(crop, b.Quarters)
		dst := image.Rect(b.X, b.Y, b.X+rot.Rect.Dx(), b.Y+rot.Rect.Dy())
		draw.Draw(img, dst, rot, image.Point{}, draw.Over)
		w, h := crop.Rect.Dx(), crop.Rect.Dy()
		for _, g := range tt.Glyphs {
			bx := g.Box.Sub(ink.Min)
			var r image.Rectangle
			switch ((b.Quarters % 4) + 4) % 4 {
			case 1:
				r = image.Rect(h-bx.Max.Y, bx.Min.X, h-bx.Min.Y, bx.Max.X)
			case 2:
				r = image.Rect(w-bx.Max.X, h-bx.Max.Y, w-bx.Min.X, h-bx.Min.Y)
			default:
				r = image.Rect(bx.Min.Y, w-bx.Max.X, bx.Max.Y, w-bx.Min.X)
			}
			g.Box = r.Add(image.Pt(b.X, b.Y))
			t.Glyphs = append(t.Glyphs, g)
		}
	}
	return img, t, nil
}

// drawBlock draws b wrapped to its width at (X+dx, Y+dy), recording glyphs.
func drawBlock(img *image.Gray, b Block, faces []*render.Face, t *Truth, dx, dy int) error {
	regular, err := faceAt(faces, b.Face, b.Px)
	if err != nil {
		return err
	}
	heavy := regular
	if b.HeavyFace != "" {
		if heavy, err = faceAt(faces, b.HeavyFace, b.Px); err != nil {
			return err
		}
	}
	leading := b.Leading
	if leading == 0 {
		leading = 1.3
	}
	space := advance(regular, ' ')
	words := splitWords(b.Text, b.Heavy)
	y := b.Y + dy
	x := b.X + dx
	line := 0
	for i, w := range words {
		f := regular
		if w.heavy {
			f = heavy
		}
		width := measure(f, w.text)
		if i > 0 && x+width > b.X+dx+b.Width {
			y += int(math.Round(b.Px * leading))
			x = b.X + dx
			line++
		}
		drawRun(img, f, w.text, x, y, b.Claim, w.heavy, line, t)
		x += width + space
	}
	return nil
}

type word struct {
	text  string
	heavy bool
}

// splitWords splits on spaces and marks a word heavy when every rune of it
// lies inside a heavy span.
func splitWords(text string, heavy []Span) []word {
	var out []word
	runes := []rune(text)
	start := -1
	flush := func(end int) {
		if start < 0 {
			return
		}
		w := word{text: string(runes[start:end])}
		for _, s := range heavy {
			if start >= s.Start && end <= s.End {
				w.heavy = true
			}
		}
		out = append(out, w)
		start = -1
	}
	for i, r := range runes {
		if r == ' ' {
			flush(i)
			continue
		}
		if start < 0 {
			start = i
		}
	}
	flush(len(runes))
	return out
}

func faceAt(faces []*render.Face, name string, px float64) (font.Face, error) {
	f, ok := render.Find(faces, name)
	if !ok {
		return nil, fmt.Errorf("synth: no face %q", name)
	}
	return f.At(px)
}

func advance(face font.Face, r rune) int {
	a, _ := face.GlyphAdvance(r)
	return a.Ceil()
}

// measure returns the advance width of s in px, with kerning.
func measure(face font.Face, s string) int {
	var x fixed.Int26_6
	prev := rune(-1)
	for _, r := range s {
		if prev >= 0 {
			x += face.Kern(prev, r)
		}
		a, _ := face.GlyphAdvance(r)
		x += a
		prev = r
	}
	return x.Ceil()
}

// drawRun draws s with its left edge at x and baseline at y, recording every
// non-space glyph's ink box.
func drawRun(dst *image.Gray, face font.Face, s string, x, y int, claim string, heavy bool, line int, t *Truth) {
	dot := fixed.P(x, y)
	prev := rune(-1)
	for _, r := range s {
		if prev >= 0 {
			dot.X += face.Kern(prev, r)
		}
		dr, mask, maskp, adv, ok := face.Glyph(dot, r)
		if ok {
			draw.DrawMask(dst, dr, image.Black, image.Point{}, mask, maskp, draw.Over)
			if r != ' ' && !dr.Empty() {
				t.Glyphs = append(t.Glyphs, Glyph{Char: string(r), Box: dr, Claim: claim, Heavy: heavy, Line: line})
			}
		}
		dot.X += adv
		prev = r
	}
}

// CountNonSpace returns how many glyphs Render records for text.
func CountNonSpace(text string) int {
	return len(text) - strings.Count(text, " ")
}
