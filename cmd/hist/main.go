// Command hist prints the gray-level histogram of a rectangle of an image,
// a diagnostic for choosing threshold parameters.
package main

import (
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"strconv"
)

func main() {
	if len(os.Args) != 6 {
		fmt.Fprintln(os.Stderr, "usage: hist image x0 y0 x1 y1")
		os.Exit(2)
	}
	f, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var v [4]int
	for i := range v {
		v[i], _ = strconv.Atoi(os.Args[i+2])
	}
	var hist [16]int
	n := 0
	for y := v[1]; y < v[3]; y++ {
		for x := v[0]; x < v[2]; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			gray := (299*r + 587*g + 114*b) / 1000 >> 8
			hist[gray/16]++
			n++
		}
	}
	for i, h := range hist {
		fmt.Printf("%3d-%3d %6.2f%%\n", i*16, i*16+15, 100*float64(h)/float64(n))
	}
}
