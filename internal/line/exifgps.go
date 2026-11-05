package line

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
)

// LINE's apps blank the GPS part of a JPEG's EXIF data before they send it,
// as a photo or as a file, so a photo does not tell where it was taken.
// line-cli does the same, and more thoroughly: LINE blanks the GPS entries
// only, leaving the coordinates they pointed to in the file, while line-cli
// also blanks those values. The bytes are zeroed as the file is uploaded, so
// the size stays the same and the file on disk is not changed.

// byteRange is a span of a file, by offset and length.
type byteRange struct{ off, n int64 }

// maxEXIFHead is how much of a JPEG is read to find its EXIF data, which sits
// in APP1 segments (at most 64 KB each) near the start.
const maxEXIFHead = 256 << 10

// jpegGPSRangesOf returns the ranges to blank in a file: its EXIF GPS data if
// it is a JPEG (whatever its name), else none.
func jpegGPSRangesOf(path string) ([]byteRange, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	head := make([]byte, maxEXIFHead)
	n, err := io.ReadFull(f, head)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return nil, err
	}
	return jpegGPSRanges(head[:n]), nil
}

// jpegGPSRanges finds, in the start of a JPEG, the GPS IFD of each EXIF
// segment: its entry count and entries, and the values stored outside them.
func jpegGPSRanges(b []byte) []byteRange {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return nil
	}
	var out []byteRange
	for p := 2; p+4 <= len(b); {
		if b[p] != 0xFF {
			break
		}
		m := b[p+1]
		switch {
		case m == 0xFF: // fill byte
			p++
			continue
		case m == 0x01 || (m >= 0xD0 && m <= 0xD7): // no length
			p += 2
			continue
		case m == 0xDA || m == 0xD9: // image data or end: no EXIF after this
			return out
		}
		size := int(binary.BigEndian.Uint16(b[p+2:]))
		if size < 2 {
			break
		}
		start, end := p+4, p+2+size
		if m == 0xE1 && end <= len(b) && bytes.HasPrefix(b[start:end], []byte("Exif\x00\x00")) {
			out = append(out, tiffGPSRanges(b[start+6:end], int64(start+6))...)
		}
		p = end
	}
	return out
}

// tiffGPSRanges finds the GPS IFD in EXIF (TIFF) data that starts at base.
func tiffGPSRanges(t []byte, base int64) []byteRange {
	if len(t) < 8 {
		return nil
	}
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return nil
	}
	if bo.Uint16(t[2:]) != 42 {
		return nil
	}
	gps := -1
	ifd0 := int(bo.Uint32(t[4:]))
	if ifd0 < 8 || ifd0+2 > len(t) {
		return nil
	}
	for i, n := 0, int(bo.Uint16(t[ifd0:])); i < n; i++ {
		e := ifd0 + 2 + 12*i
		if e+12 > len(t) {
			break
		}
		if bo.Uint16(t[e:]) == 0x8825 { // GPSInfo: the GPS IFD's offset
			gps = int(bo.Uint32(t[e+8:]))
			break
		}
	}
	if gps < 8 || gps+2 > len(t) {
		return nil
	}
	n := int(bo.Uint16(t[gps:]))
	n = min(n, (len(t)-gps-2)/12)
	out := []byteRange{{base + int64(gps), 2 + 12*int64(n)}}
	for i := 0; i < n; i++ {
		e := gps + 2 + 12*i
		size := tiffTypeSize(bo.Uint16(t[e+2:])) * int64(bo.Uint32(t[e+4:]))
		if size <= 4 {
			continue // the value is inside the entry
		}
		off := int64(bo.Uint32(t[e+8:]))
		if off >= 8 && off+size <= int64(len(t)) {
			out = append(out, byteRange{base + off, size})
		}
	}
	return out
}

// tiffTypeSize is the size of one value of a TIFF field type.
func tiffTypeSize(typ uint16) int64 {
	switch typ {
	case 1, 2, 6, 7: // BYTE, ASCII, SBYTE, UNDEFINED
		return 1
	case 3, 8: // SHORT, SSHORT
		return 2
	case 4, 9, 11: // LONG, SLONG, FLOAT
		return 4
	case 5, 10, 12: // RATIONAL, SRATIONAL, DOUBLE
		return 8
	}
	return 0
}

// blanker zeroes ranges of the stream it reads.
type blanker struct {
	r      io.Reader
	off    int64
	ranges []byteRange
}

func (z *blanker) Read(p []byte) (int, error) {
	n, err := z.r.Read(p)
	for _, rg := range z.ranges {
		from, to := max(rg.off, z.off), min(rg.off+rg.n, z.off+int64(n))
		for i := from; i < to; i++ {
			p[i-z.off] = 0
		}
	}
	z.off += int64(n)
	return n, err
}
