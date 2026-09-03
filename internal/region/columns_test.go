package region

import (
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"sort"
	"strings"
	"testing"

	"treasury/internal/bitmap"
	"treasury/internal/preprocess"
)

// TestDumpColumns prints the per-column cut cues of the component under a
// point of an image: a diagnostic, skipped unless REGION_DUMP=path,x,y.
func TestDumpColumns(t *testing.T) {
	spec := os.Getenv("REGION_DUMP")
	if spec == "" {
		t.Skip("set REGION_DUMP=path,x,y")
	}
	var path string
	var px, py int
	if _, err := parseSpec(spec, &path, &px, &py); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	pre, err := preprocess.Run(img, preprocess.Default())
	if err != nil {
		t.Fatal(err)
	}
	p := Default()
	cs := MergeDots(Filter(Components(pre.Bin), pre.Bin.H, p))
	lines, _ := Lines(cs, p)
	for _, ln := range lines {
		for _, c := range ln.Comps {
			if !image.Pt(px, py).In(c.Box) {
				continue
			}
			typical := min(float64(ln.MedW), 0.9*float64(ln.MedH))
			t.Logf("component %v line medW=%d medH=%d typical=%.1f wide=%v", c.Box, ln.MedW, ln.MedH, typical, float64(c.Box.Dx()) > 2.5*typical)
			dumpColumns(t, pre.Bin, c)
			return
		}
	}
	t.Fatalf("no component at %d,%d", px, py)
}

func parseSpec(spec string, path *string, x, y *int) (int, error) {
	parts := strings.Split(spec, ",")
	if len(parts) != 3 {
		return 0, fmt.Errorf("want path,x,y")
	}
	*path = parts[0]
	return fmt.Sscanf(parts[1]+" "+parts[2], "%d %d", x, y)
}

func dumpColumns(t *testing.T, b *bitmap.Bitmap, c Component) {
	crop := b.Crop(c.Box)
	cu := measureColumns(crop)
	t.Logf("maxProfile=%.0f halfStroke=%.1f", cu.maxProfile, cu.halfStroke)
	for x := range crop.W {
		marks := ""
		if cu.neck[x] <= 0.75*cu.halfStroke && cu.profile[x] <= 0.5*cu.maxProfile {
			marks += " NECK"
		}
		if cu.runs[x] == 1 && cu.profile[x] <= 0.4*cu.maxProfile {
			marks += " THIN1"
		}
		col := make([]byte, crop.H)
		for y := range crop.H {
			col[y] = '.'
			if crop.Pix[y*crop.W+x] != 0 {
				col[y] = '#'
			}
		}
		t.Logf("x=%3d ink=%2.0f runs=%d neck=%4.1f %s%s", x, cu.profile[x], cu.runs[x], cu.neck[x], string(col), marks)
	}
	_ = sort.Ints
	_ = math.Abs
}
