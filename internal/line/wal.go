package line

import (
	"encoding/binary"
	"io"
	"os"
)

func walChecksum(big bool, data []byte, s1, s2 uint32) (uint32, uint32) {
	get := binary.LittleEndian.Uint32
	if big {
		get = binary.BigEndian.Uint32
	}
	for i := 0; i+8 <= len(data); i += 8 {
		s1 += get(data[i:]) + s2
		s2 += get(data[i+4:]) + s1
	}
	return s1, s2
}

type walOverlay struct {
	pages      map[uint32][]byte
	commitSize uint32
}

func readWalFile(walPath string) ([]byte, error) {
	if _, err := os.Stat(walPath); os.IsNotExist(err) {
		return nil, nil
	}
	f, err := openShared(walPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

func readWalOverlay(walPath string, pageSize int, master key) (walOverlay, error) {
	var ov walOverlay
	data, err := readWalFile(walPath)
	if err != nil {
		return ov, err
	}
	if len(data) < 32 {
		return ov, nil
	}
	h := data[:32]
	if wps := int(binary.BigEndian.Uint32(h[8:12])); wps != pageSize {
		return ov, &Error{Code: ErrDBUnreadable, Message: "write-ahead log page size does not match the database"}
	}
	storedC1 := binary.BigEndian.Uint32(h[24:28])
	storedC2 := binary.BigEndian.Uint32(h[28:32])
	big := true
	ok := false
	for _, b := range []bool{true, false} {
		c1, c2 := walChecksum(b, h[:24], 0, 0)
		if c1 == storedC1 && c2 == storedC2 {
			big, ok = b, true
			break
		}
	}
	if !ok {
		return ov, nil
	}
	salt := h[16:24]
	fsz := 24 + pageSize
	nframes := (len(data) - 32) / fsz

	s1, s2 := walChecksum(big, h[:24], 0, 0)
	ov.pages = map[uint32][]byte{}
	pending := map[uint32][]byte{}
	for i := 0; i < nframes; i++ {
		off := 32 + i*fsz
		fh := data[off : off+24]
		pageCT := data[off+24 : off+24+pageSize]
		pgno := binary.BigEndian.Uint32(fh[0:4])
		dbSize := binary.BigEndian.Uint32(fh[4:8])
		if string(fh[8:16]) != string(salt) {
			break
		}
		fc1 := binary.BigEndian.Uint32(fh[16:20])
		fc2 := binary.BigEndian.Uint32(fh[20:24])
		a1, a2 := walChecksum(big, fh[:8], s1, s2)
		a1, a2 = walChecksum(big, pageCT, a1, a2)
		if a1 != fc1 || a2 != fc2 {
			break
		}
		s1, s2 = a1, a2
		dec := append([]byte(nil), pageCT...)
		decryptPage(dec, master, pgno)
		pending[pgno] = dec
		if dbSize != 0 {
			for p, b := range pending {
				ov.pages[p] = b
			}
			clear(pending)
			ov.commitSize = dbSize
		}
	}
	return ov, nil
}

func buildImage(f *os.File, pageSize, npages int, master key, ov walOverlay) []byte {
	total := npages
	if int(ov.commitSize) > total {
		total = int(ov.commitSize)
	}
	img := make([]byte, total*pageSize)
	raw := make([]byte, npages*pageSize)
	f.ReadAt(raw, 0)
	for p := 1; p <= npages; p++ {
		buf := raw[(p-1)*pageSize : p*pageSize]
		decryptPage(buf, master, uint32(p))
		copy(img[(p-1)*pageSize:], buf)
	}
	for pgno, dec := range ov.pages {
		p := int(pgno)
		if p >= 1 && p <= total {
			copy(img[(p-1)*pageSize:p*pageSize], dec)
		}
	}
	if ov.commitSize != 0 {
		img = img[:int(ov.commitSize)*pageSize]
	}
	binary.BigEndian.PutUint32(img[28:32], uint32(len(img)/pageSize))
	return img
}
