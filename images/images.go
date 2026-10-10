// Package images makes an attached picture one a model takes: PNG, JPEG,
// GIF or WebP, turned upright by its EXIF orientation, and at most
// MaxSide pixels on a side and MaxBytes long. A HEIC converts on a Mac
// (sips).
package images

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	MaxSide  = 2000
	MaxBytes = 5 << 20
)

// ErrNotImage is returned for bytes no image format reads.
var ErrNotImage = errors.New("not an image")

// Is reports whether data looks like an image Fit can make one of.
func Is(data []byte, name string) bool { return typeOf(data, name) != "" }

func typeOf(data []byte, name string) string {
	switch mt := http.DetectContentType(data); mt {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return mt
	}
	if isHEIC(data, name) {
		return "image/heic"
	}
	return ""
}

func isHEIC(data []byte, name string) bool {
	if len(data) >= 12 && string(data[4:8]) == "ftyp" {
		switch string(data[8:12]) {
		case "heic", "heix", "hevc", "heim", "heis", "mif1", "msf1":
			return true
		}
	}
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".heic" || ext == ".heif"
}

// Fit is data as an image a model takes, with its media type and a note
// saying what was changed ("" when nothing was).
func Fit(data []byte, name string) (out []byte, mediaType, note string, err error) {
	mt := typeOf(data, name)
	if mt == "" {
		return nil, "", "", ErrNotImage
	}
	var notes []string
	if mt == "image/heic" {
		if data, err = fromHEIC(data); err != nil {
			return nil, "", "", err
		}
		mt = "image/jpeg"
		notes = append(notes, "converted from HEIC")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, "", "", fmt.Errorf("%w: %v", ErrNotImage, err)
	}
	orient := 1
	if mt == "image/jpeg" {
		orient = Orientation(data)
	}
	if orient == 1 && cfg.Width <= MaxSide && cfg.Height <= MaxSide && len(data) <= MaxBytes {
		return data, mt, strings.Join(notes, ", "), nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", "", fmt.Errorf("%w: %v", ErrNotImage, err)
	}
	if orient != 1 {
		img = upright(img, orient)
		notes = append(notes, "turned upright")
	}
	b := img.Bounds()
	if w, h := b.Dx(), b.Dy(); w > MaxSide || h > MaxSide {
		nw, nh := MaxSide, h*MaxSide/w
		if h > w {
			nw, nh = w*MaxSide/h, MaxSide
		}
		dst := image.NewRGBA(image.Rect(0, 0, max(nw, 1), max(nh, 1)))
		draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
		img = dst
		notes = append(notes, fmt.Sprintf("scaled down from %d×%d to %d×%d", w, h, dst.Bounds().Dx(), dst.Bounds().Dy()))
	}
	if mt == "image/gif" {
		if g, err := gif.DecodeAll(bytes.NewReader(data)); err == nil && len(g.Image) > 1 {
			notes = append(notes, "first frame only")
		}
	}
	var buf bytes.Buffer
	if mt == "image/png" || mt == "image/gif" { // keep transparency
		if err := png.Encode(&buf, img); err == nil && buf.Len() <= MaxBytes {
			return buf.Bytes(), "image/png", strings.Join(notes, ", "), nil
		}
		buf.Reset()
	}
	for _, q := range []int{85, 70, 50} {
		buf.Reset()
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: q}); err != nil {
			return nil, "", "", err
		}
		if buf.Len() <= MaxBytes {
			break
		}
	}
	if buf.Len() > MaxBytes {
		return nil, "", "", fmt.Errorf("the image is still larger than %d MB scaled down", MaxBytes>>20)
	}
	return buf.Bytes(), "image/jpeg", strings.Join(notes, ", "), nil
}

// Thumbnail is data as a picture at most side pixels on a side, upright.
func Thumbnail(data []byte, name string, side int) (image.Image, error) {
	mt := typeOf(data, name)
	if mt == "" {
		return nil, ErrNotImage
	}
	var err error
	if mt == "image/heic" {
		if data, err = fromHEIC(data); err != nil {
			return nil, err
		}
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotImage, err)
	}
	if o := Orientation(data); o != 1 {
		img = upright(img, o)
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= side && h <= side {
		return img, nil
	}
	nw, nh := side, max(h*side/w, 1)
	if h > w {
		nw, nh = max(w*side/h, 1), side
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
	return dst, nil
}

// Square is data as a square PNG at most side pixels across, upright and
// cut from the middle of the picture, as an avatar.
func Square(data []byte, name string, side int) ([]byte, error) {
	img, err := Thumbnail(data, name, 1<<30) // decoded and upright
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	short := min(b.Dx(), b.Dy())
	if short <= 0 {
		return nil, ErrNotImage
	}
	crop := image.Rect(0, 0, short, short).Add(b.Min).Add(image.Pt((b.Dx()-short)/2, (b.Dy()-short)/2))
	n := min(side, short)
	dst := image.NewNRGBA(image.Rect(0, 0, n, n))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, crop, draw.Src, nil)
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// fromHEIC converts a HEIC to a JPEG with the Mac's sips.
func fromHEIC(data []byte) ([]byte, error) {
	if runtime.GOOS != "darwin" {
		return nil, errors.New("a HEIC image converts only on a Mac")
	}
	dir, err := os.MkdirTemp("", "gobot-heic")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	in, out := filepath.Join(dir, "in.heic"), filepath.Join(dir, "out.jpg")
	if err := os.WriteFile(in, data, 0o600); err != nil {
		return nil, err
	}
	if b, err := exec.Command("sips", "-s", "format", "jpeg", in, "--out", out).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("converting the HEIC failed: %s", strings.TrimSpace(string(b)))
	}
	return os.ReadFile(out)
}

// upright turns img by an EXIF orientation (2–8) so it shows as taken.
func upright(img image.Image, orient int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if orient >= 5 {
		w, h = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	sw, sh := b.Dx(), b.Dy()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var sx, sy int
			switch orient {
			case 2:
				sx, sy = sw-1-x, y
			case 3:
				sx, sy = sw-1-x, sh-1-y
			case 4:
				sx, sy = x, sh-1-y
			case 5:
				sx, sy = y, x
			case 6:
				sx, sy = y, sh-1-x
			case 7:
				sx, sy = sw-1-y, sh-1-x
			case 8:
				sx, sy = sw-1-y, x
			default:
				sx, sy = x, y
			}
			dst.Set(x, y, img.At(b.Min.X+sx, b.Min.Y+sy))
		}
	}
	return dst
}

// Orientation is a JPEG's EXIF orientation, 1 when it has none.
func Orientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1
	}
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xFF {
			return 1
		}
		marker := data[i+1]
		size := int(data[i+2])<<8 | int(data[i+3])
		if marker == 0xDA || size < 2 || i+2+size > len(data) {
			return 1
		}
		seg := data[i+4 : i+2+size]
		if marker == 0xE1 && len(seg) > 6 && string(seg[:6]) == "Exif\x00\x00" {
			return exifOrientation(seg[6:])
		}
		i += 2 + size
	}
	return 1
}

func exifOrientation(t []byte) int {
	if len(t) < 8 {
		return 1
	}
	u16 := func(b []byte) int { return int(b[0])<<8 | int(b[1]) }
	u32 := func(b []byte) int { return int(b[0])<<24 | int(b[1])<<16 | int(b[2])<<8 | int(b[3]) }
	switch string(t[:2]) {
	case "II":
		u16 = func(b []byte) int { return int(b[1])<<8 | int(b[0]) }
		u32 = func(b []byte) int { return int(b[3])<<24 | int(b[2])<<16 | int(b[1])<<8 | int(b[0]) }
	case "MM":
	default:
		return 1
	}
	off := u32(t[4:8])
	if off < 8 || off+2 > len(t) {
		return 1
	}
	n := u16(t[off:])
	for k := 0; k < n; k++ {
		e := off + 2 + 12*k
		if e+12 > len(t) {
			return 1
		}
		if u16(t[e:]) == 0x0112 {
			if v := u16(t[e+8:]); v >= 1 && v <= 8 {
				return v
			}
			return 1
		}
	}
	return 1
}
