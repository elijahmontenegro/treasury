package region

import (
	"image"
	"testing"

	"treasury/internal/bitmap"
)

func fill(b *bitmap.Bitmap, r image.Rectangle) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			b.Set(x, y, 1)
		}
	}
}

func TestComponents(t *testing.T) {
	b := bitmap.New(40, 20)
	boxes := []image.Rectangle{image.Rect(2, 2, 8, 12), image.Rect(12, 3, 20, 12), image.Rect(30, 5, 36, 15)}
	for _, r := range boxes {
		fill(b, r)
	}
	// Two pixels touching only diagonally must join under 8-connectivity.
	b.Set(25, 2, 1)
	b.Set(26, 3, 1)
	cs := Components(b)
	if len(cs) != 4 {
		t.Fatalf("got %d components, want 4", len(cs))
	}
	found := map[image.Rectangle]int{}
	for _, c := range cs {
		found[c.Box] = c.Area
	}
	for _, r := range boxes {
		if found[r] != r.Dx()*r.Dy() {
			t.Errorf("box %v: area %d, want %d", r, found[r], r.Dx()*r.Dy())
		}
	}
	if found[image.Rect(25, 2, 27, 4)] != 2 {
		t.Errorf("diagonal pair not joined: %v", found)
	}
}

func TestFilter(t *testing.T) {
	cs := []Component{
		{Box: image.Rect(0, 0, 2, 3), Area: 6},    // too small
		{Box: image.Rect(0, 0, 10, 2), Area: 20},  // too short
		{Box: image.Rect(0, 0, 10, 50), Area: 40}, // too tall for a 100 px image
		{Box: image.Rect(0, 0, 3, 3), Area: 9},    // a period: keep
		{Box: image.Rect(0, 0, 10, 12), Area: 40}, // keep
	}
	got := Filter(cs, 100, Default())
	if len(got) != 2 || got[0].Box.Dy() != 3 || got[1].Box.Dy() != 12 {
		t.Fatalf("got %v", got)
	}
}

func TestMergeDots(t *testing.T) {
	cs := []Component{
		{Box: image.Rect(10, 10, 13, 24), Area: 42}, // stem of an i
		{Box: image.Rect(10, 4, 13, 7), Area: 9},    // its dot
		{Box: image.Rect(20, 10, 30, 24), Area: 100},
		{Box: image.Rect(40, 12, 43, 15), Area: 9}, // colon, upper dot
		{Box: image.Rect(40, 20, 43, 23), Area: 9}, // colon, lower dot
		{Box: image.Rect(50, 10, 60, 24), Area: 100},
		{Box: image.Rect(70, 10, 80, 24), Area: 100},
	}
	got := MergeDots(cs)
	if len(got) != 6 {
		t.Fatalf("got %d components, want 6: %v", len(got), got)
	}
	if got[0].Box != image.Rect(10, 4, 13, 24) || got[0].Area != 51 {
		t.Errorf("i not merged: %+v", got[0])
	}
}

// Two rows of glyph-like blobs; the second row has a wide gap that must split
// it into two lines, and the first row a medium gap that yields sub-regions.
func TestLinesAndRegions(t *testing.T) {
	b := bitmap.New(300, 80)
	var row1, row2 []image.Rectangle
	x := 10
	for i := 0; i < 8; i++ {
		row1 = append(row1, image.Rect(x, 10, x+8, 24))
		x += 12
		if i == 3 {
			x += 30 // medium gap: > 2.5×8 = 20, < 1.5×8 = 12? no: it is > 12, so it splits at the line level too
		}
	}
	x = 10
	for i := 0; i < 8; i++ {
		row2 = append(row2, image.Rect(x, 50, x+8, 64))
		x += 12
		if i == 3 {
			x += 120
		}
	}
	for _, r := range append(row1, row2...) {
		fill(b, r)
	}
	lines, regions := Propose(b, nil, Default())
	if len(lines) != 4 {
		t.Fatalf("got %d lines, want 4 (both rows split at their gaps): %+v", len(lines), lines)
	}
	bands, subs := 0, 0
	for _, r := range regions {
		switch r.Kind {
		case KindBand:
			bands++
		case KindSub:
			subs++
		}
	}
	if bands != 2 {
		t.Errorf("got %d band regions, want 2", bands)
	}
	if subs != 0 {
		t.Errorf("got %d word-run regions from lines with evenly spaced blobs, want 0", subs)
	}
}

func TestWords(t *testing.T) {
	// Glyphs 8 wide with 3 px letter gaps, a 12 px word gap after the third.
	var comps []Component
	x := 0
	for i := range 7 {
		comps = append(comps, Component{Box: image.Rect(x, 0, x+8, 14), Area: 100})
		x += 11
		if i == 2 {
			x += 9
		}
	}
	words := Words(newLine(comps, 0))
	if len(words) != 2 || words[0].Max.X != 30 || words[1].Min.X != 42 {
		t.Fatalf("words = %v", words)
	}
	// Drawn as ink, the two-word line yields the line itself plus one
	// region per word; the two-word run is the line and is deduplicated.
	b := bitmap.New(300, 80) // tall enough that 14 px glyphs are under 40% of the height
	for _, c := range comps {
		fill(b, c.Box.Add(image.Pt(5, 5)))
	}
	lines, regions := Propose(b, nil, Default())
	if len(lines) != 1 || len(regions) != 3 {
		t.Errorf("got %d lines and %d regions, want 1 and 3", len(lines), len(regions))
	}
}

func TestMode(t *testing.T) {
	if m := mode([]int{5, 6, 6, 7, 20, 21}); m != 6 {
		t.Errorf("mode = %d, want 6", m)
	}
}
