// Package render holds the bundled typefaces. The engine uses them only to
// synthesize glyphs the reference text lacks; the generator uses them to draw
// documents.
package render

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
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

//go:embed fonts/*.ttf
var bundled embed.FS

// Face is one parsed typeface at one weight.
type Face struct {
	Name   string // "Lato Bold"
	Family string // "Lato"
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

// Bundled parses the embedded faces: the Go fonts plus the OFL faces under
// fonts/, whose file names are Family-Weight.ttf.
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
	out := make([]*Face, 0, len(specs)+12)
	for _, s := range specs {
		f, err := opentype.Parse(s.ttf)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", s.name, err)
		}
		out = append(out, &Face{Name: s.name, Family: s.family, Weight: s.weight, Font: f})
	}
	entries, err := fs.ReadDir(bundled, "fonts")
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		stem := strings.TrimSuffix(e.Name(), ".ttf")
		family, weight, ok := strings.Cut(stem, "-")
		if !ok {
			return nil, fmt.Errorf("font file %q is not Family-Weight.ttf", e.Name())
		}
		weight = strings.ToLower(weight)
		b, err := bundled.ReadFile("fonts/" + e.Name())
		if err != nil {
			return nil, err
		}
		f, err := opentype.Parse(b)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", e.Name(), err)
		}
		out = append(out, &Face{Name: family + " " + strings.ToUpper(weight[:1]) + weight[1:], Family: family, Weight: weight, Font: f})
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

// Sibling returns the face of the same family at the given weight.
func Sibling(faces []*Face, of *Face, weight string) (*Face, bool) {
	if of == nil {
		return nil, false
	}
	for _, f := range faces {
		if f.Family == of.Family && f.Weight == weight {
			return f, true
		}
	}
	return nil, false
}
