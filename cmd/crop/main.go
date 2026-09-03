// Command crop cuts a rectangle out of an image and scales it up for eyeballing.
//
//	crop in.png x0 y0 x1 y1 out.png [scale]
package main

import (
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"strconv"

	xdraw "golang.org/x/image/draw"

	"treasury/internal/bitmap"
)

func main() {
	if len(os.Args) < 7 {
		fmt.Fprintln(os.Stderr, "usage: crop in.png x0 y0 x1 y1 out.png [scale]")
		os.Exit(2)
	}
	var n [4]int
	for i := range n {
		v, err := strconv.Atoi(os.Args[2+i])
		if err != nil {
			fmt.Fprintln(os.Stderr, "crop:", err)
			os.Exit(2)
		}
		n[i] = v
	}
	scale := 1
	if len(os.Args) > 7 {
		scale, _ = strconv.Atoi(os.Args[7])
	}
	if err := run(os.Args[1], image.Rect(n[0], n[1], n[2], n[3]), os.Args[6], scale); err != nil {
		fmt.Fprintln(os.Stderr, "crop:", err)
		os.Exit(1)
	}
}

func run(in string, r image.Rectangle, out string, scale int) error {
	f, err := os.Open(in)
	if err != nil {
		return err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return err
	}
	r = r.Intersect(img.Bounds())
	src := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(src, src.Rect, img, r.Min, draw.Src)
	if scale <= 1 {
		return bitmap.WritePNG(out, src)
	}
	dst := image.NewRGBA(image.Rect(0, 0, r.Dx()*scale, r.Dy()*scale))
	xdraw.NearestNeighbor.Scale(dst, dst.Rect, src, src.Rect, xdraw.Src, nil)
	return bitmap.WritePNG(out, dst)
}
