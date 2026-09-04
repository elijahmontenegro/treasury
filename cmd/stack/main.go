// Command stack composes label images into one image, one below the other
// on white with a gap, so a front label and its back label can be verified
// together as the engine sees a photograph of a bottle's labels.
//
//	stack out.png in1.png in2.jpg ...
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"os"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: stack out.png in1 [in2 ...]")
		os.Exit(2)
	}
	var imgs []image.Image
	w, h := 0, 0
	const gap = 40
	for _, p := range os.Args[2:] {
		f, err := os.Open(p)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		img, _, err := image.Decode(f)
		f.Close()
		if err != nil {
			fmt.Fprintln(os.Stderr, p, err)
			os.Exit(1)
		}
		imgs = append(imgs, img)
		w = max(w, img.Bounds().Dx())
		h += img.Bounds().Dy() + gap
	}
	out := image.NewRGBA(image.Rect(0, 0, w+2*gap, h+gap))
	draw.Draw(out, out.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)
	y := gap
	for _, img := range imgs {
		b := img.Bounds()
		x := gap + (w-b.Dx())/2
		draw.Draw(out, image.Rect(x, y, x+b.Dx(), y+b.Dy()), img, b.Min, draw.Over)
		y += b.Dy() + gap
	}
	f, err := os.Create(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	if err := png.Encode(f, out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("%s: %dx%d from %d images\n", os.Args[1], out.Bounds().Dx(), out.Bounds().Dy(), len(imgs))
}
