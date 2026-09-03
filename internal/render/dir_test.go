package render

import (
	"os"
	"testing"
)

func TestLoadDirSystemFonts(t *testing.T) {
	dir := os.Getenv("WINDIR")
	if dir == "" {
		t.Skip("not Windows")
	}
	faces, skipped, err := LoadDir(dir + `\Fonts`)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d faces, %d skipped", len(faces), len(skipped))
	for i, f := range faces {
		if i < 12 {
			t.Logf("%s (%s / %s)", f.Name, f.Family, f.Weight)
		}
	}
	if len(faces) < 20 {
		t.Errorf("only %d faces", len(faces))
	}
}
