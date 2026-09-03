package alphabet_test

import (
	"image"
	"testing"

	"treasury/internal/alphabet"
	"treasury/internal/preprocess"
	"treasury/internal/region"
	"treasury/internal/render"
	"treasury/internal/synth"
	"treasury/ttb"
)

type scored struct {
	acc            float64
	correct, total int
	a              *alphabet.Alphabet
}

// score runs preprocess → regions → alphabet on img and measures how many
// truth glyphs of the reference were assigned their own character.
func score(t *testing.T, img *image.Gray, truth *synth.Truth, ref string, spans []alphabet.Span) scored {
	t.Helper()
	pre, err := preprocess.Run(img, preprocess.Default())
	if err != nil {
		t.Fatal(err)
	}
	lines, _ := region.Propose(pre.Bin, pre.Glare, region.Default())
	a, err := alphabet.Find(lines, pre.Bin, pre.Gray, ref, spans, alphabet.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	chars := []rune(ref)
	// Decoder glyph boxes mapped back to input coordinates. A truth glyph is
	// correct when a decoder glyph covering at least half of it stands for
	// its character; a merged pair covers both of its truth glyphs.
	boxes := make([]image.Rectangle, len(a.Block.Glyphs))
	for gi, g := range a.Block.Glyphs {
		x0, y0 := pre.ToInput(float64(g.Box.Min.X), float64(g.Box.Min.Y))
		x1, y1 := pre.ToInput(float64(g.Box.Max.X), float64(g.Box.Min.Y))
		x2, y2 := pre.ToInput(float64(g.Box.Max.X), float64(g.Box.Max.Y))
		x3, y3 := pre.ToInput(float64(g.Box.Min.X), float64(g.Box.Max.Y))
		boxes[gi] = image.Rect(
			int(min(x0, x1, x2, x3)), int(min(y0, y1, y2, y3)),
			int(max(x0, x1, x2, x3))+1, int(max(y0, y1, y2, y3))+1)
	}
	s := scored{a: a}
	for _, tg := range truth.Glyphs {
		if tg.Claim != "reference" {
			continue
		}
		s.total++
		area := tg.Box.Dx() * tg.Box.Dy()
		ok := false
		for gi, b := range boxes {
			ov := b.Intersect(tg.Box)
			if ov.Empty() || 2*ov.Dx()*ov.Dy() < area {
				continue
			}
			for _, ci := range a.Assign[gi] {
				if string(chars[ci]) == tg.Char {
					ok = true
				}
			}
		}
		if ok {
			s.correct++
		}
	}
	if s.total > 0 {
		s.acc = float64(s.correct) / float64(s.total)
	}
	t.Logf("%d/%d glyphs correct (%.1f%%); spread %.3f, penalized %d, matched %d, band %d, passes %d, cost %.1f, x-height %.0f, chars %d",
		s.correct, s.total, 100*s.acc, a.Spread, a.Penalized, a.Matched, a.Band, a.Passes, a.Cost, a.XHeight, len(a.Samples))
	for _, r := range a.Rows {
		t.Logf("row %d: %d glyphs, %d matched, %d penalized, mean distance %.3f", r.Row, r.Glyphs, r.Matched, r.Penalized, r.MeanDistance)
	}
	return s
}

func sample(t *testing.T) (*image.Gray, *synth.Truth) {
	t.Helper()
	faces, err := render.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	img, truth, err := synth.Render(ttb.LabelDocument(ttb.Sample()), faces)
	if err != nil {
		t.Fatal(err)
	}
	return img, truth
}

var headerSpan = []alphabet.Span{{Start: 0, End: ttb.HeaderLen}}

func TestSampleClean(t *testing.T) {
	img, truth := sample(t)
	s := score(t, img, truth, ttb.Statute, headerSpan)
	if s.acc < 0.95 {
		t.Errorf("accuracy %.1f%% below the 95%% gate", 100*s.acc)
	}
	if len(s.a.Samples) < 20 {
		t.Errorf("only %d characters learned", len(s.a.Samples))
	}
	if len(s.a.Emphasis[0]['G']) == 0 {
		t.Error("no heavy G learned from the header span")
	}
	if s.a.LetterGap <= 0 || s.a.WordGap <= s.a.LetterGap {
		t.Errorf("gaps: letter %.1f word %.1f", s.a.LetterGap, s.a.WordGap)
	}
}

func TestSampleAugmented(t *testing.T) {
	img, truth := sample(t)
	aug, at, err := synth.Augment(img, truth, synth.Aug{RotateDeg: 7, BlurSigma: 1, JPEGQuality: 50})
	if err != nil {
		t.Fatal(err)
	}
	s := score(t, aug, at, ttb.Statute, headerSpan)
	if s.acc < 0.95 {
		t.Errorf("accuracy %.1f%% below the 95%% gate on the augmented copy", 100*s.acc)
	}
}

// A reference that is not a label proves the core is string-agnostic.
func TestPlainParagraph(t *testing.T) {
	const ref = "Pack my box with five dozen liquor jugs. The quick brown fox jumps over the lazy dog, and then Sphinx of black quartz, judge my vow! Jackdaws love my big sphinx of quartz."
	faces, err := render.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	doc := synth.Document{
		W: 900, H: 520,
		Items: []synth.Item{
			{Text: "Some Heading", Face: "Go Bold", Px: 40, X: 450, Y: 70, Center: true},
			{Text: "a decoy line of unrelated text", Face: "Go Regular", Px: 22, X: 450, Y: 470, Center: true},
		},
		Blocks: []synth.Block{{Text: ref, Face: "Go Regular", Px: 22, X: 60, Y: 160, Width: 780, Leading: 1.35, Claim: "reference"}},
	}
	img, truth, err := synth.Render(doc, faces)
	if err != nil {
		t.Fatal(err)
	}
	s := score(t, img, truth, ref, nil)
	if s.acc < 0.95 {
		t.Errorf("accuracy %.1f%% below the 95%% gate on a plain paragraph", 100*s.acc)
	}
	for _, r := range "jqxz" {
		if len(s.a.Samples[r]) == 0 {
			t.Errorf("no sample for %q", r)
		}
	}
}
