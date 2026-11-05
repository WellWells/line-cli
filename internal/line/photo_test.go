package line

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

// exifJPEG returns a JPEG whose APP1 EXIF block sets the given orientation.
func exifJPEG(t *testing.T, orient uint16, order binary.ByteOrder) []byte {
	t.Helper()
	var plain bytes.Buffer
	if err := jpeg.Encode(&plain, image.NewRGBA(image.Rect(0, 0, 4, 2)), nil); err != nil {
		t.Fatal(err)
	}
	tiff := make([]byte, 8+2+12+4)
	if order == binary.LittleEndian {
		copy(tiff, "II")
	} else {
		copy(tiff, "MM")
	}
	order.PutUint16(tiff[2:], 42)
	order.PutUint32(tiff[4:], 8)
	order.PutUint16(tiff[8:], 1)       // one IFD entry
	order.PutUint16(tiff[10:], 0x0112) // Orientation
	order.PutUint16(tiff[12:], 3)      // SHORT
	order.PutUint32(tiff[14:], 1)
	order.PutUint16(tiff[18:], orient)
	seg := append([]byte("Exif\x00\x00"), tiff...)
	app1 := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(app1[2:], uint16(len(seg)+2))
	b := plain.Bytes()
	return append(append(append([]byte{}, b[:2]...), append(app1, seg...)...), b[2:]...)
}

func TestJpegOrientation(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for _, o := range []uint16{1, 3, 6, 8} {
			b := exifJPEG(t, o, order)
			if got := jpegOrientation(b); got != int(o) {
				t.Errorf("%v orientation %d: got %d", order, o, got)
			}
			if _, err := jpeg.Decode(bytes.NewReader(b)); err != nil {
				t.Fatalf("test JPEG does not decode: %v", err)
			}
		}
	}
	for name, b := range map[string][]byte{
		"empty":         nil,
		"not jpeg":      []byte("\x89PNG"),
		"bad value":     exifJPEG(t, 9, binary.BigEndian),
		"truncated":     exifJPEG(t, 6, binary.BigEndian)[:12],
		"no exif":       {0xFF, 0xD8, 0xFF, 0xDA, 0, 2},
		"short segment": {0xFF, 0xD8, 0xFF, 0xE1, 0, 1},
	} {
		if got := jpegOrientation(b); got != 1 {
			t.Errorf("%s: got %d, want 1", name, got)
		}
	}
}

// Each EXIF orientation moves the stored top-left pixel to where a viewer
// shows it.
func TestOrientImage(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 3, 2))
	red := color.RGBA{255, 0, 0, 255}
	src.Set(0, 0, red)
	for _, c := range []struct {
		orient int
		w, h   int
		at     image.Point
	}{
		{1, 3, 2, image.Pt(0, 0)},
		{2, 3, 2, image.Pt(2, 0)},
		{3, 3, 2, image.Pt(2, 1)},
		{4, 3, 2, image.Pt(0, 1)},
		{5, 2, 3, image.Pt(0, 0)},
		{6, 2, 3, image.Pt(1, 0)},
		{7, 2, 3, image.Pt(1, 2)},
		{8, 2, 3, image.Pt(0, 2)},
	} {
		got := orientImage(src, c.orient)
		if got.Bounds().Dx() != c.w || got.Bounds().Dy() != c.h {
			t.Errorf("orientation %d: size %v", c.orient, got.Bounds())
			continue
		}
		if got.RGBAAt(c.at.X, c.at.Y) != red {
			t.Errorf("orientation %d: top-left pixel not at %v", c.orient, c.at)
		}
	}
}

func TestDownscale(t *testing.T) {
	for _, c := range []struct{ w, h, dw, dh int }{
		{1000, 400, 500, 200},
		{400, 1000, 200, 500},
		{300, 200, 300, 200},
		{5000, 4, 500, 1},
	} {
		got := downscale(image.NewRGBA(image.Rect(0, 0, c.w, c.h)), previewMaxSide)
		if got.Bounds().Dx() != c.dw || got.Bounds().Dy() != c.dh {
			t.Errorf("%dx%d: got %v, want %dx%d", c.w, c.h, got.Bounds(), c.dw, c.dh)
		}
	}
	// Averaging: half black, half white becomes mid grey.
	src := image.NewRGBA(image.Rect(0, 0, 1000, 2))
	for x := 0; x < 1000; x++ {
		v := uint8(0)
		if x%2 == 1 {
			v = 255
		}
		src.SetRGBA(x, 0, color.RGBA{v, v, v, 255})
		src.SetRGBA(x, 1, color.RGBA{v, v, v, 255})
	}
	if g := downscale(src, previewMaxSide).RGBAAt(10, 0); g.R < 120 || g.R > 135 {
		t.Errorf("box average %v, want mid grey", g)
	}
}
