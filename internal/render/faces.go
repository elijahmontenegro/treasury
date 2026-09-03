// Package render holds the bundled typefaces. The engine uses them only to
// synthesize glyphs the reference text lacks; the generator uses them to draw
// documents.
package render

import (
	"fmt"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/gomedium"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
)

// Face is one parsed typeface at one weight.
type Face struct {
	Name   string // "Go Regular"
	Family string // "Go"
	Weight string // regular, medium, bold
	Font   *sfnt.Font

	mu    sync.Mutex
	cache map[float64]font.Face
}

// At returns a rasterizing face at the given pixel size (72 dpi, no hinting).
// The returned font.Face is not safe for concurrent use.
func (f *Face) At(px float64) (font.Face, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ff, ok := f.cache[px]; ok {
		return ff, nil
	}
	ff, err := opentype.NewFace(f.Font, &opentype.FaceOptions{Size: px, DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		return nil, fmt.Errorf("face %s at %.1fpx: %w", f.Name, px, err)
	}
	if f.cache == nil {
		f.cache = map[float64]font.Face{}
	}
	f.cache[px] = ff
	return ff, nil
}

// Bundled parses the embedded Go fonts.
func Bundled() ([]*Face, error) {
	specs := []struct {
		name, family, weight string
		ttf                  []byte
	}{
		{"Go Regular", "Go", "regular", goregular.TTF},
		{"Go Medium", "Go", "medium", gomedium.TTF},
		{"Go Bold", "Go", "bold", gobold.TTF},
		{"Go Italic", "Go", "regular", goitalic.TTF},
		{"Go Mono", "Go Mono", "regular", gomono.TTF},
	}
	out := make([]*Face, 0, len(specs))
	for _, s := range specs {
		f, err := opentype.Parse(s.ttf)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", s.name, err)
		}
		out = append(out, &Face{Name: s.name, Family: s.family, Weight: s.weight, Font: f})
	}
	return out, nil
}

// Find returns the face with the given name.
func Find(faces []*Face, name string) (*Face, bool) {
	for _, f := range faces {
		if f.Name == name {
			return f, true
		}
	}
	return nil, false
}
