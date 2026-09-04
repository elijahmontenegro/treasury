package synth

import (
	"image"
	"testing"

	"treasury/internal/render"
)

func testDoc() Document {
	return Document{
		W: 600, H: 300,
		Items: []Item{{Text: "Hello, World 42", Face: "Go Regular", Px: 32, X: 20, Y: 60, Claim: "x"}},
		Blocks: []Block{{
			Text:  "GOVERNMENT WARNING: one two three four five six seven eight nine ten eleven",
			Heavy: []Span{{Start: 0, End: 19}},
			Face:  "Go Regular", HeavyFace: "Go Bold", Px: 20, X: 20, Y: 120, Width: 300, Claim: "ref",
		}},
	}
}

func renderTest(t *testing.T) (*image.Gray, *Truth) {
	t.Helper()
	faces, err := render.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	img, truth, err := Render(testDoc(), faces)
	if err != nil {
		t.Fatal(err)
	}
	return img, truth
}

func darkIn(g *image.Gray, r image.Rectangle, below uint8) bool {
	r = r.Intersect(g.Rect)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if g.GrayAt(x, y).Y < below {
				return true
			}
		}
	}
	return false
}

func TestRenderRecordsEveryGlyph(t *testing.T) {
	img, truth := renderTest(t)
	doc := testDoc()
	want := CountNonSpace(doc.Items[0].Text) + CountNonSpace(doc.Blocks[0].Text)
	if len(truth.Glyphs) != want {
		t.Fatalf("recorded %d glyphs, want %d", len(truth.Glyphs), want)
	}
	heavy, maxLine := 0, 0
	for _, g := range truth.Glyphs {
		if g.Heavy {
			heavy++
		}
		if g.Line > maxLine {
			maxLine = g.Line
		}
		if !g.Box.In(img.Rect) {
			t.Errorf("glyph %q box %v outside the canvas", g.Char, g.Box)
		}
		if !darkIn(img, g.Box, 128) {
			t.Errorf("glyph %q box %v has no ink", g.Char, g.Box)
		}
	}
	if heavy != 18 {
		t.Errorf("heavy glyphs = %d, want 18 for GOVERNMENT WARNING:", heavy)
	}
	if maxLine < 2 {
		t.Errorf("block wrapped into %d lines, expected at least 3", maxLine+1)
	}
}

func TestAugmentTransformsBoxes(t *testing.T) {
	img, truth := renderTest(t)
	aug, at, err := Augment(img, truth, Aug{RotateDeg: 7, BlurSigma: 1, JPEGQuality: 50})
	if err != nil {
		t.Fatal(err)
	}
	if at.AngleDeg != 7 || len(at.Glyphs) != len(truth.Glyphs) {
		t.Fatalf("angle %.1f glyphs %d", at.AngleDeg, len(at.Glyphs))
	}
	if truth.Glyphs[0].Box == at.Glyphs[0].Box {
		t.Error("boxes were not transformed")
	}
	hit := 0
	for _, g := range at.Glyphs {
		if darkIn(aug, g.Box, 170) {
			hit++
		}
	}
	if hit < len(at.Glyphs)*95/100 {
		t.Errorf("only %d of %d transformed boxes contain ink", hit, len(at.Glyphs))
	}
}

func TestSplitWords(t *testing.T) {
	words := splitWords("AB CD  EF", []Span{{Start: 0, End: 5}})
	if len(words) != 3 || !words[0].heavy || !words[1].heavy || words[2].heavy {
		t.Errorf("got %+v", words)
	}
}
