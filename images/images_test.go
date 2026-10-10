package images

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func pngOf(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

func TestFitKeepsASmallImage(t *testing.T) {
	in := pngOf(40, 30)
	out, mt, note, err := Fit(in, "a.png")
	if err != nil || mt != "image/png" || note != "" || !bytes.Equal(in, out) {
		t.Fatalf("got %s %q %v", mt, note, err)
	}
}

func TestFitScalesALargeImage(t *testing.T) {
	out, mt, note, err := Fit(pngOf(4000, 1000), "wide.png")
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, _ := image.DecodeConfig(bytes.NewReader(out))
	if mt != "image/png" || cfg.Width != 2000 || cfg.Height != 500 || note != "scaled down from 4000×1000 to 2000×500" {
		t.Fatalf("got %s %dx%d %q", mt, cfg.Width, cfg.Height, note)
	}
}

func TestFitRefusesText(t *testing.T) {
	if _, _, _, err := Fit([]byte("hello"), "a.txt"); !errors.Is(err, ErrNotImage) {
		t.Fatal(err)
	}
	if Is([]byte("hello"), "a.txt") {
		t.Fatal("text is an image")
	}
}

// withOrientation is a JPEG of img with an EXIF APP1 naming orient.
func withOrientation(img image.Image, orient byte) []byte {
	var b bytes.Buffer
	jpeg.Encode(&b, img, nil)
	j := b.Bytes()
	tiff := []byte{'M', 'M', 0, 42, 0, 0, 0, 8, 0, 1, 0x01, 0x12, 0, 3, 0, 0, 0, 1, 0, orient, 0, 0, 0, 0, 0, 0, 0, 0}
	seg := append([]byte("Exif\x00\x00"), tiff...)
	app1 := append([]byte{0xFF, 0xE1, byte((len(seg) + 2) >> 8), byte(len(seg) + 2)}, seg...)
	return append(append([]byte{0xFF, 0xD8}, app1...), j[2:]...)
}

func TestFitTurnsAPhotoUpright(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 60, 20)) // wide, taken turned: shows tall
	data := withOrientation(img, 6)
	if Orientation(data) != 6 {
		t.Fatalf("orientation %d", Orientation(data))
	}
	out, mt, note, err := Fit(data, "photo.jpg")
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, _ := image.DecodeConfig(bytes.NewReader(out))
	if mt != "image/jpeg" || cfg.Width != 20 || cfg.Height != 60 || note != "turned upright" {
		t.Fatalf("got %s %dx%d %q", mt, cfg.Width, cfg.Height, note)
	}
}

func TestThumbnail(t *testing.T) {
	img, err := Thumbnail(pngOf(900, 300), "a.png", 240)
	if err != nil || img.Bounds().Dx() != 240 || img.Bounds().Dy() != 80 {
		t.Fatalf("%v %v", img.Bounds(), err)
	}
}
