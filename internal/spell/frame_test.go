package spell

import (
	"image"
	"testing"

	"treasury/internal/alphabet"
	"treasury/internal/bitcode"
	"treasury/internal/bitmap"
	"treasury/internal/encoder"
	"treasury/internal/render"
)

// TestFrameNoiseFloor reports, per encoder configuration, how far the same
// glyph drifts between sizes against how far different glyphs sit apart,
// all through the glyph frame. It guides the choice of glyph encoder.
func TestFrameNoiseFloor(t *testing.T) {
	faces, err := render.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	f, _ := render.Find(faces, "Go Regular")
	pos := encoder.Hash{W: 16, H: 16, Levels: 4, Smooth: 1}
	configs := []encoder.Encoder{
		encoder.Hash{W: 24, H: 24, Levels: 2},
		pos,
		encoder.Hash{W: 12, H: 12, Levels: 8, Smooth: 1},
		encoder.Dual{Pos: pos, Tight: encoder.Hash{W: 12, H: 12, Levels: 4, Smooth: 1}},
		encoder.Dual{Pos: pos, Tight: encoder.Hash{W: 16, H: 16, Levels: 4, Smooth: 1}},
		encoder.Dual{Pos: pos, Tight: encoder.Hash{W: 12, H: 16, Levels: 4, Smooth: 1}},
		encoder.Dual{Pos: pos, Tight: encoder.Hash{W: 10, H: 14, Levels: 8, Smooth: 1}},
		encoder.Dual{Pos: pos, Tight: encoder.Hash{W: 16, H: 20, Levels: 4, Smooth: 1}},
	}
	same := []rune("o5ceAl08")
	pairs := []string{"53", "58", "56", "oc", "oe", "ea", "AV", "li", "30", "08", "06", "35"}
	sizes := []float64{18, 19.1, 24}
	for _, enc := range configs {
		code := func(r rune, xh float64) bitcode.Code {
			sg, err := f.Glyph(r, xh, false)
			if err != nil {
				t.Fatal(err)
			}
			return frameCode(sg, xh, enc)
		}
		noise, n := 0.0, 0
		worst := 0
		for _, r := range same {
			for _, xh := range sizes {
				d := bitcode.Distance(code(r, 14), code(r, xh))
				noise += float64(d)
				worst = max(worst, d)
				n++
			}
		}
		noise /= float64(n)
		signal, m := 0.0, 0
		least := 1 << 30
		for _, p := range pairs {
			a, b := rune(p[0]), rune(p[1])
			d := bitcode.Distance(code(a, 14), code(b, 19.1))
			signal += float64(d)
			least = min(least, d)
			m++
		}
		signal /= float64(m)
		t.Logf("%-18s bits=%5d  same-glyph drift mean %5.1f worst %4d   different-glyph mean %5.1f least %4d   ratio %.2f  worst/least %.2f",
			enc.Name(), enc.Bits(), noise, worst, signal, least, signal/noise, float64(worst)/float64(least))
	}
}

// TestFrameSensitivity reports how a small error in the estimated x-height
// or baseline moves a glyph's code, for the candidate encoders.
func TestFrameSensitivity(t *testing.T) {
	faces, err := render.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	f, _ := render.Find(faces, "Go Regular")
	pos := encoder.Hash{W: 16, H: 16, Levels: 4, Smooth: 1}
	for _, enc := range []encoder.Encoder{pos, encoder.Dual{Pos: pos, Tight: encoder.Hash{W: 12, H: 16, Levels: 4, Smooth: 1}}} {
		for _, r := range "o5A" {
			sg, err := f.Glyph(r, 19.1, false)
			if err != nil {
				t.Fatal(err)
			}
			ref := frameAt(sg, 19.1, 0, enc)
			t.Logf("%-16s %q: xh +4%%: %3d  xh -4%%: %3d  baseline +1: %3d  baseline -1: %3d  both: %3d",
				enc.Name(), r,
				bitcode.Distance(ref, frameAt(sg, 19.1*1.04, 0, enc)),
				bitcode.Distance(ref, frameAt(sg, 19.1*0.96, 0, enc)),
				bitcode.Distance(ref, frameAt(sg, 19.1, 1, enc)),
				bitcode.Distance(ref, frameAt(sg, 19.1, -1, enc)),
				bitcode.Distance(ref, frameAt(sg, 19.1*1.04, 1, enc)))
		}
	}
}

// frameAt frames a synthesized glyph with a possibly wrong x-height and a
// baseline shifted by dy.
func frameAt(sg render.Synth, xh float64, dy int, enc encoder.Encoder) bitcode.Code {
	side := int(2.2*xh) + 1
	canvas := bitmap.New(sg.Box.Dx()+2*side, 4*side)
	baseline := 2 * side
	box := sg.Box.Add(image.Pt(side, baseline))
	blit(canvas, sg.Bin, box.Min)
	return enc.Encode(alphabet.Frame(canvas, box, baseline+dy, xh))
}
