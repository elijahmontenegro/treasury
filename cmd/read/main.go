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
)

func main() {
	flag.Parse()
	r, err := ocr.New(ocr.Default())
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
