// Command read prints what the scene-text reader finds in each image, with
// the time it took (amendment step 19b).
//
//	read real2/*.png > out/read.jsonl
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	"treasury/internal/ocr"
	"treasury/verify"
)

var turned bool

// sideOn is the engine's own reading-conditional test for the second
// detection pass (step 24a): a page whose upright pass returned a
// detection taller than it is wide has text the detector saw side-on.
func sideOn(rs []ocr.Region) bool {
	for _, r := range rs {
		if r.Box.Dy() > r.Box.Dx() {
			return true
		}
	}
	return false
}

func main() {
	flag.Parse()
	// Read the way the engine reads. `ocr.Default` is still the 960 px
	// cap and the single upright pass of step 19b, and the engine has
	// detected at 1600 with a second pass on the turned page since 20c
	// and 21d, so this tool had been reporting a weaker reading than the
	// one every verdict rests on - found at step 27b, where it made a
	// brand the engine reads look unread.
	rp := ocr.Default()
	for _, c := range verify.Adopted {
		switch c.Name {
		case "read_max_side":
			rp.MaxSide = int(c.Value)
		case "read_box_thresh":
			rp.BoxThresh = c.Value
		case "read_unclip":
			rp.Unclip = c.Value
		case "read_turned":
			turned = c.Value > 0
		}
	}
	r, err := ocr.New(rp)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read:", err)
		os.Exit(1)
	}
	defer r.Close()
	enc := json.NewEncoder(os.Stdout)
	for _, path := range flag.Args() {
		f, err := os.Open(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, path, err)
			continue
		}
		img, _, err := image.Decode(f)
		f.Close()
		if err != nil {
			fmt.Fprintln(os.Stderr, path, err)
			continue
		}
		start := time.Now()
		regions, err := r.Read(img)
		if err == nil && turned && sideOn(regions) {
			more, terr := r.ReadTurned(img, regions)
			if terr != nil {
				err = terr
			} else {
				regions = append(regions, more...)
			}
		}
		took := time.Since(start)
		if err != nil {
			fmt.Fprintln(os.Stderr, path, err)
			continue
		}
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		if err := enc.Encode(map[string]any{
			"label": name, "seconds": took.Seconds(), "regions": regions,
			"width": img.Bounds().Dx(), "height": img.Bounds().Dy(),
		}); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		fmt.Fprintf(os.Stderr, "%s %d regions %.1fs\n", name, len(regions), took.Seconds())
	}
}
