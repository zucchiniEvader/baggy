package main

// Makes a macOS-shaped app icon: the art in an 824 rounded square, centered
// on a transparent 1024 canvas.

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"

	xdraw "golang.org/x/image/draw"
)

func makeApp(srcPath, outPath string) {
	in, err := os.Open(srcPath)
	if err != nil {
		panic(err)
	}
	src, err := png.Decode(in)
	if err != nil {
		panic(err)
	}
	const canvas, size, radius = 1024, 824, 185.0
	art := image.NewRGBA(image.Rect(0, 0, size, size))
	xdraw.CatmullRom.Scale(art, art.Bounds(), src, src.Bounds(), xdraw.Over, nil)
	out := image.NewRGBA(image.Rect(0, 0, canvas, canvas))
	off := (canvas - size) / 2
	// Rounded corners, antialiased by coverage of 4×4 samples per pixel.
	inside := func(x, y float64) bool {
		cx, cy := math.Min(math.Max(x, radius), size-radius), math.Min(math.Max(y, radius), size-radius)
		dx, dy := x-cx, y-cy
		return dx*dx+dy*dy <= radius*radius
	}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			n := 0
			for sy := 0; sy < 4; sy++ {
				for sx := 0; sx < 4; sx++ {
					if inside(float64(x)+(float64(sx)+0.5)/4, float64(y)+(float64(sy)+0.5)/4) {
						n++
					}
				}
			}
			if n == 0 {
				continue
			}
			c := art.RGBAAt(x, y)
			a := float64(n) / 16
			out.SetRGBA(x+off, y+off, color.RGBA{uint8(float64(c.R) * a), uint8(float64(c.G) * a), uint8(float64(c.B) * a), uint8(255 * a)})
		}
	}
	f, err := os.Create(outPath)
	if err != nil {
		panic(err)
	}
	png.Encode(f, out)
	f.Close()
}
