package line

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/draw"
	"image/jpeg"
	_ "image/png" // register the PNG decoder
	"os"
)

// previewMaxSide is the longest side of a photo's preview, as the LINE apps
// make them.
const previewMaxSide = 500

// maxPhotoPixels is the pixel count above which an image is handled as a file
// rather than decoded for a preview (decoding would take too much memory). It
// lives here, in the always-built read side, because photo and video reading
// use it too; the send side (sendfile.go) also relies on it.
const maxPhotoPixels = 50_000_000

// photoExt reports whether a file extension is sent as a photo.
func photoExt(ext string) bool { return ext == "jpg" || ext == "jpeg" || ext == "png" }

type photo struct {
	width, height int // as displayed, after the EXIF orientation
	preview       []byte
}

// makePhoto decodes a JPEG or PNG and makes its JPEG preview.
func makePhoto(path, ext string) (photo, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return photo{}, err
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return photo{}, errors.New("not a readable image")
	}
	want := "jpeg"
	if ext == "png" {
		want = "png"
	}
	if format != want {
		return photo{}, errors.New("the content is " + format + ", not " + want)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxPhotoPixels {
		return photo{}, errors.New("the image is too large to make a preview")
	}
	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return photo{}, errors.New("the image could not be decoded: " + err.Error())
	}
	orient := 1
	if format == "jpeg" {
		orient = jpegOrientation(b)
	}
	small := downscale(img, previewMaxSide)
	small = orientImage(small, orient)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, small, &jpeg.Options{Quality: 85}); err != nil {
		return photo{}, err
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	if orient >= 5 {
		w, h = h, w
	}
	return photo{width: w, height: h, preview: out.Bytes()}, nil
}

// downscale box-filters img so its longer side is at most maxSide (it never
// enlarges) and flattens transparency onto white.
func downscale(img image.Image, maxSide int) *image.RGBA {
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	dw, dh := sw, sh
	if sw > maxSide || sh > maxSide {
		if sw >= sh {
			dw, dh = maxSide, max(1, sh*maxSide/sw)
		} else {
			dw, dh = max(1, sw*maxSide/sh), maxSide
		}
	}
	src, ok := img.(*image.RGBA)
	if !ok || src.Bounds().Min != (image.Point{}) {
		src = image.NewRGBA(image.Rect(0, 0, sw, sh))
		draw.Draw(src, src.Bounds(), img, b.Min, draw.Src)
	}
	type acc struct{ r, g, b, a, n uint64 }
	sums := make([]acc, dw*dh)
	col := make([]int, sw)
	for x := range col {
		col[x] = x * dw / sw
	}
	for y := 0; y < sh; y++ {
		row := sums[(y*dh/sh)*dw:]
		p := src.Pix[y*src.Stride:]
		for x := 0; x < sw; x++ {
			s := &row[col[x]]
			s.r += uint64(p[4*x])
			s.g += uint64(p[4*x+1])
			s.b += uint64(p[4*x+2])
			s.a += uint64(p[4*x+3])
			s.n++
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for i, s := range sums {
		if s.n == 0 {
			continue
		}
		a := s.a / s.n
		// Premultiplied colour over white: c + (255 - a).
		dst.Pix[4*i] = uint8(min(255, s.r/s.n+255-a))
		dst.Pix[4*i+1] = uint8(min(255, s.g/s.n+255-a))
		dst.Pix[4*i+2] = uint8(min(255, s.b/s.n+255-a))
		dst.Pix[4*i+3] = 255
	}
	return dst
}

// orientImage applies an EXIF orientation (1-8) so the image displays upright.
func orientImage(src *image.RGBA, orient int) *image.RGBA {
	if orient < 2 || orient > 8 {
		return src
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	ow, oh := w, h
	if orient >= 5 {
		ow, oh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, ow, oh))
	for oy := 0; oy < oh; oy++ {
		for ox := 0; ox < ow; ox++ {
			var sx, sy int
			switch orient {
			case 2:
				sx, sy = w-1-ox, oy
			case 3:
				sx, sy = w-1-ox, h-1-oy
			case 4:
				sx, sy = ox, h-1-oy
			case 5:
				sx, sy = oy, ox
			case 6:
				sx, sy = oy, h-1-ox
			case 7:
				sx, sy = w-1-oy, h-1-ox
			case 8:
				sx, sy = w-1-oy, ox
			}
			copy(dst.Pix[oy*dst.Stride+4*ox:][:4], src.Pix[sy*src.Stride+4*sx:][:4])
		}
	}
	return dst
}

// jpegOrientation returns the EXIF orientation of a JPEG, or 1 when it has
// none or it cannot be read.
func jpegOrientation(b []byte) int {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return 1
	}
	for i := 2; i+4 <= len(b); {
		if b[i] != 0xFF {
			return 1
		}
		marker := b[i+1]
		if marker == 0xD8 || marker == 0x01 || marker >= 0xD0 && marker <= 0xD7 || marker == 0xFF {
			i++
			continue
		}
		if marker == 0xDA || marker == 0xD9 { // image data starts: no EXIF before it
			return 1
		}
		n := int(binary.BigEndian.Uint16(b[i+2:]))
		if n < 2 || i+2+n > len(b) {
			return 1
		}
		seg := b[i+4 : i+2+n]
		if marker == 0xE1 && len(seg) > 6 && string(seg[:6]) == "Exif\x00\x00" {
			return exifOrientation(seg[6:])
		}
		i += 2 + n
	}
	return 1
}

// exifOrientation reads tag 0x0112 from IFD0 of a TIFF-format EXIF block.
func exifOrientation(t []byte) int {
	if len(t) < 8 {
		return 1
	}
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 1
	}
	off := int(bo.Uint32(t[4:]))
	if off < 8 || off+2 > len(t) {
		return 1
	}
	count := int(bo.Uint16(t[off:]))
	for k := 0; k < count; k++ {
		e := off + 2 + 12*k
		if e+12 > len(t) {
			return 1
		}
		if bo.Uint16(t[e:]) == 0x0112 && bo.Uint16(t[e+2:]) == 3 {
			if v := int(bo.Uint16(t[e+8:])); v >= 1 && v <= 8 {
				return v
			}
			return 1
		}
	}
	return 1
}
