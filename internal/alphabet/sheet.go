package alphabet

import (
	"image"
	"image/color"
	"math"
	"sort"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"

	"treasury/internal/render"
)

// Sheet draws one row per learned character with its glyph samples on their
// baselines; emphasis pools follow, marked with an asterisk. faces may be nil.
func Sheet(a *Alphabet, faces []*render.Face) *image.Gray {
	type row struct {
		label  string
		glyphs []Glyph
	}
	var rows []row
	keys := make([]rune, 0, len(a.Samples))
	for r := range a.Samples {
		keys = append(keys, r)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	for _, r := range keys {
		rows = append(rows, row{string(r), a.Samples[r]})
	}
	spans := make([]int, 0, len(a.Emphasis))
	for s := range a.Emphasis {
		spans = append(spans, s)
	}
	sort.Ints(spans)
	for _, s := range spans {
		var rs []rune
		for r := range a.Emphasis[s] {
			rs = append(rs, r)
		}
		sort.Slice(rs, func(i, j int) bool { return rs[i] < rs[j] })
		for _, r := range rs {
			rows = append(rows, row{string(r) + "*", a.Emphasis[s][r]})
		}
	}

	xh := a.XHeight
	cell := int(math.Ceil(2.6*xh)) + 6
	if cell < 28 {
		cell = 28
	}
	const label = 48
	const maxPer = 40
	width := label + 8
	for _, r := range rows {
		w := label + 8
		for i, g := range r.glyphs {
			if i >= maxPer {
				break
			}
			w += g.Box.Dx() + 6
		}
		width = max(width, w)
	}
	width = min(width, 2000)
	img := image.NewGray(image.Rect(0, 0, width, cell*len(rows)+8))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	var face font.Face
	if f, ok := render.Find(faces, "Go Regular"); ok {
		face, _ = f.At(20)
	}
	for ri, r := range rows {
		top := 4 + ri*cell
		baseline := top + int(math.Round(1.8*xh))
		if face != nil {
			d := font.Drawer{Dst: img, Src: image.Black, Face: face, Dot: fixed.P(6, top+cell*3/4)}
			d.DrawString(r.label)
		}
		x := label + 8
		for i, g := range r.glyphs {
			if i >= maxPer || x+g.Box.Dx() > width {
				break
			}
			gy := baseline - (g.Baseline - g.Box.Min.Y)
			for y := range g.Bin.H {
				for gx := range g.Bin.W {
					if g.Bin.Pix[y*g.Bin.W+gx] != 0 {
						img.SetGray(x+gx, gy+y, color.Gray{})
					}
				}
			}
			if g.Derived { // underline samples cut out of merged pairs
				for gx := range g.Box.Dx() {
					img.SetGray(x+gx, top+cell-3, color.Gray{Y: 128})
				}
			}
			x += g.Box.Dx() + 6
		}
	}
	return img
}
