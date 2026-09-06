// Command orient prints the direction each image's text runs, measured from
// the arrangement of its components (amendment step 14b).
//
//	orient real2/*.png
package main

import (
	"flag"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"

	"treasury/internal/preprocess"
	"treasury/internal/region"
)

var verbose bool

func main() {
	flag.BoolVar(&verbose, "v", false, "print the measurements")
	flag.Parse()
	for _, path := range flag.Args() {
		q, err := one(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, path, err)
			continue
		}
		fmt.Printf("%s %s\n", filepath.Base(path), []string{"as_is", "rot90", "rot180", "rot270"}[q])
	}
}

func one(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return 0, err
	}
	pre, err := preprocess.Run(img, preprocess.Default())
	if err != nil {
		return 0, err
	}
	var boxes []image.Rectangle
	for _, c := range region.Components(pre.Bin) {
		boxes = append(boxes, c.Box)
	}
	q, note := region.DirectionDebug(boxes, pre.Bin.W, pre.Bin.H)
	if verbose {
		fmt.Printf("   %s\n", note)
	}
	return q, nil
}
