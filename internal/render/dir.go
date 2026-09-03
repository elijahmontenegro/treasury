package render

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
)

// LoadDir parses every .ttf in dir as a face for local use, such as a
// held-out family set for the generator. Files that fail to parse are
// skipped and reported in the second result. Family and weight are read
// from the font's own name table when present, else from the file name.
func LoadDir(dir string) ([]*Face, []string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	var faces []*Face
	var skipped []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.EqualFold(filepath.Ext(name), ".ttf") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			skipped = append(skipped, name+": "+err.Error())
			continue
		}
		f, err := opentype.Parse(b)
		if err != nil {
			skipped = append(skipped, name+": "+err.Error())
			continue
		}
		family, style := names(f, strings.TrimSuffix(name, filepath.Ext(name)))
		weight := "regular"
		if s := strings.ToLower(style); strings.Contains(s, "bold") || strings.Contains(s, "black") || strings.Contains(s, "heavy") {
			weight = "bold"
		}
		if s := strings.ToLower(style); strings.Contains(s, "italic") || strings.Contains(s, "oblique") {
			continue // italics are not a weight; leave them out
		}
		faces = append(faces, &Face{Name: family + " " + strings.ToUpper(weight[:1]) + weight[1:], Family: family, Weight: weight, Font: f})
	}
	sort.Slice(faces, func(i, j int) bool { return faces[i].Name < faces[j].Name })
	if len(faces) == 0 {
		return nil, skipped, fmt.Errorf("render: no usable .ttf in %s", dir)
	}
	return faces, skipped, nil
}

// names reads the family and subfamily from the font's name table, falling
// back to the file stem.
func names(f *sfnt.Font, stem string) (string, string) {
	var buf sfnt.Buffer
	family, err := f.Name(&buf, sfnt.NameIDFamily)
	if err != nil || family == "" {
		family = stem
	}
	style, _ := f.Name(&buf, sfnt.NameIDSubfamily)
	return family, style
}
