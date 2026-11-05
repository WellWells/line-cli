package line

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rc4"
	"encoding/binary"
)

var padding = []byte{
	0x28, 0xBF, 0x4E, 0x5E, 0x4E, 0x75, 0x8A, 0x41, 0x64, 0x00, 0x4E, 0x56, 0xFF, 0xFA, 0x01, 0x08,
	0x2E, 0x2E, 0x00, 0xB6, 0xD0, 0x68, 0x3E, 0x80, 0x2F, 0x0C, 0xA9, 0xFE, 0x64, 0x53, 0x69, 0x7A,
}

var sqliteMagic = []byte("SQLite format 3\x00")

type key [16]byte

func padPassword(p []byte) []byte {
	b := make([]byte, 0, 32)
	if len(p) > 32 {
		p = p[:32]
	}
	b = append(b, p...)
	b = append(b, padding[:32-len(b)]...)
	return b
}

func deriveKey(pw []byte) key {
	userPad := padPassword(pw)
	ownerPad := padPassword(nil)
	d := md5.Sum(ownerPad)
	for k := 0; k < 50; k++ {
		d = md5.Sum(d[:])
	}
	ownerKey := append([]byte(nil), userPad...)
	for i := 0; i < 20; i++ {
		var mk [16]byte
		for j := range mk {
			mk[j] = d[j] ^ byte(i)
		}
		c, _ := rc4.NewCipher(mk[:])
		c.XORKeyStream(ownerKey, ownerKey)
	}
	buf := append(append([]byte{}, userPad...), ownerKey...)
	d = md5.Sum(buf)
	for k := 0; k < 50; k++ {
		d = md5.Sum(d[:])
	}
	return key(d)
}

func pageKey(master key, pgno uint32) key {
	nkey := make([]byte, 0, 24)
	nkey = append(nkey, master[:]...)
	var le [4]byte
	binary.LittleEndian.PutUint32(le[:], pgno)
	nkey = append(nkey, le[:]...)
	nkey = append(nkey, 's', 'A', 'l', 'T')
	return key(md5.Sum(nkey))
}

func pageIV(pg uint32) [16]byte {
	z := int64(pg) + 1
	var b [16]byte
	for j := 0; j < 4; j++ {
		q := z / 52774
		z = 40692*(z-52774*q) - 3791*q
		if z < 0 {
			z += 2147483399
		}
		binary.LittleEndian.PutUint32(b[4*j:], uint32(z))
	}
	return md5.Sum(b[:])
}

func cbcDecrypt(buf []byte, k key, iv [16]byte) {
	blk, _ := aes.NewCipher(k[:])
	cipher.NewCBCDecrypter(blk, iv[:]).CryptBlocks(buf, buf)
}

func validHdr(h []byte) bool {
	dbPageSize := (int(h[0]) << 8) | (int(h[1]) << 16)
	return dbPageSize >= 512 && dbPageSize <= 65536 &&
		(dbPageSize-1)&dbPageSize == 0 &&
		h[5] == 0x40 && h[6] == 0x20 && h[7] == 0x20
}

func decryptPage1(buf []byte, master key) (matched bool) {
	hdr := append([]byte(nil), buf[16:24]...)
	k := pageKey(master, 1)
	iv := pageIV(1)
	if validHdr(hdr) {
		copy(buf[16:24], buf[8:16])
		cbcDecrypt(buf[16:], k, iv)
		matched = bytes.Equal(buf[16:24], hdr)
		if matched {
			copy(buf[0:16], sqliteMagic)
		}
		return matched
	}
	cbcDecrypt(buf, k, iv)
	return bytes.HasPrefix(buf, sqliteMagic)
}

func decryptPageN(buf []byte, master key, pgno uint32) {
	cbcDecrypt(buf, pageKey(master, pgno), pageIV(pgno))
}

func decryptPage(buf []byte, master key, pgno uint32) {
	if pgno == 1 {
		decryptPage1(buf, master)
		return
	}
	decryptPageN(buf, master, pgno)
}
