package render

import "testing"

func TestBundledFaces(t *testing.T) {
	faces, err := Bundled()
	if err != nil {
		t.Fatal(err)
	}
	if len(faces) < 16 {
		t.Fatalf("only %d faces bundled", len(faces))
	}
	for _, name := range []string{"Go Regular", "Go Bold", "Lato Regular", "Lato Bold", "PTSerif Bold", "Lobster Regular", "Anton Regular"} {
		f, ok := Find(faces, name)
		if !ok {
			t.Errorf("no face %q", name)
			continue
		}
		if _, err := f.Glyph('a', 14, false); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	lato, _ := Find(faces, "Lato Regular")
	if b, ok := Sibling(faces, lato, "bold"); !ok || b.Name != "Lato Bold" {
		t.Errorf("Sibling(Lato Regular, bold) = %v %v", b, ok)
	}
}
