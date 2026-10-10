package main

// Makes a menu-bar template icon from the art: the bag as a black
// silhouette, its eyes, mouth and folds cut out, the background gone,
// cropped to the bag and scaled down with area averaging.

import (
	"image"
	"image/color"
	"image/png"
	"os"
)

func makeTray(srcPath, outPath string, size, grow int) {
	in, _ := os.Open(srcPath)
	src, err := png.Decode(in)
	if err != nil {
		panic(err)
	}
	b := src.Bounds()
	// Bag pixels: the orange (the dark lines inside it are cut out).
	bag := func(x, y int) bool {
		r, g, bl, _ := src.At(x, y).RGBA()
		r, g, bl = r>>8, g>>8, bl>>8
		return r > 150 && g > 90 && bl < 140
	}
	minX, minY, maxX, maxY := b.Max.X, b.Max.Y, 0, 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if bag(x, y) {
				minX, minY, maxX, maxY = min(minX, x), min(minY, y), max(maxX, x), max(maxY, y)
			}
		}
	}
	// The dark marks inside the bag (eyes, mouth, folds) grow by grow
	// pixels, so the cutouts still read at 16 points.
	dark := func(x, y int) bool {
		r, g, bl, _ := src.At(x, y).RGBA()
		return r>>8 < 110 && g>>8 < 110 && bl>>8 < 110
	}
	W, H := b.Dx(), b.Dy()
	hole := make([]bool, W*H)
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			if !dark(x, y) {
				continue
			}
			for dy := -grow; dy <= grow; dy++ {
				for dx := -grow; dx <= grow; dx++ {
					if dx*dx+dy*dy <= grow*grow {
						if xx, yy := x+dx, y+dy; xx >= 0 && yy >= 0 && xx < W && yy < H {
							hole[yy*W+xx] = true
						}
					}
				}
			}
		}
	}
	solid := func(x, y int) bool {
		return (bag(x, y) || dark(x, y) && x >= minX && x <= maxX && y >= minY && y <= maxY) && !hole[y*W+x]
	}
	// A square around the bag, a little room on each side.
	w, h := maxX-minX+1, maxY-minY+1
	side := max(w, h) * 106 / 100
	ox, oy := minX-(side-w)/2, minY-(side-h)/2
	out := image.NewNRGBA(image.Rect(0, 0, size, size))
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			x0, x1 := ox+px*side/size, ox+(px+1)*side/size
			y0, y1 := oy+py*side/size, oy+(py+1)*side/size
			n, in := 0, 0
			for y := y0; y < y1; y++ {
				for x := x0; x < x1; x++ {
					n++
					if x >= b.Min.X && x < b.Max.X && y >= b.Min.Y && y < b.Max.Y && solid(x, y) {
						in++
					}
				}
			}
			if n > 0 && in > 0 {
				out.SetNRGBA(px, py, color.NRGBA{0, 0, 0, uint8(255 * in / n)})
			}
		}
	}
	f, _ := os.Create(outPath)
	png.Encode(f, out)
	f.Close()
}
