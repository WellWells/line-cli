package line

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

// refEncryptE2EEFile mirrors decryptE2EEFile so the round trip can be tested
// without the live LINE cache. It is test-only.
func refEncryptE2EEFile(t *testing.T, kmB64 string, plain []byte) []byte {
	t.Helper()
	km, err := base64.StdEncoding.DecodeString(kmB64)
	if err != nil {
		t.Fatal(err)
	}
	d, err := hkdf.Key(sha256.New, km, nil, "FileEncryption", 76)
	if err != nil {
		t.Fatal(err)
	}
	encKey, macKey, nonce := d[:32], d[32:64], d[64:76]
	block, err := aes.NewCipher(encKey)
	if err != nil {
		t.Fatal(err)
	}
	iv := make([]byte, aes.BlockSize)
	copy(iv, nonce)
	body := make([]byte, len(plain))
	cipher.NewCTR(block, iv).XORKeyStream(body, plain)
	m := hmac.New(sha256.New, macKey)
	m.Write(body)
	return append(body, m.Sum(nil)...)
}

func TestDecryptE2EEFileRoundTrip(t *testing.T) {
	km := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0xA5}, 32))
	for _, plain := range [][]byte{
		[]byte("hello line"),
		bytes.Repeat([]byte("x"), 1), // shorter than one AES block
		bytes.Repeat([]byte("Z"), 4096),
		{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10}, // JPEG-like header
	} {
		blob := refEncryptE2EEFile(t, km, plain)
		got, err := decryptE2EEFile(km, blob)
		if err != nil {
			t.Fatalf("decrypt len=%d: %v", len(plain), err)
		}
		if !bytes.Equal(got, plain) {
			t.Fatalf("round trip mismatch len=%d: got %x want %x", len(plain), got, plain)
		}
	}
}

func TestDecryptE2EEFileWrongKeyFailsMAC(t *testing.T) {
	right := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x01}, 32))
	wrong := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x02}, 32))
	blob := refEncryptE2EEFile(t, right, []byte("secret"))
	if _, err := decryptE2EEFile(wrong, blob); err == nil {
		t.Fatal("expected MAC mismatch with wrong key, got nil")
	}
}

func TestDecryptE2EEFileTruncated(t *testing.T) {
	if _, err := decryptE2EEFile(base64.StdEncoding.EncodeToString(make([]byte, 32)), []byte{1, 2, 3}); err == nil {
		t.Fatal("expected error on blob shorter than MAC")
	}
}

func TestSniffExt(t *testing.T) {
	cases := map[string][]byte{
		".jpg":  {0xFF, 0xD8, 0xFF, 0xE0, 0, 0},
		".png":  {0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'},
		".gif":  []byte("GIF89a..."),
		".webp": []byte("RIFF\x00\x00\x00\x00WEBP"),
		".bin":  []byte("not an image"),
	}
	for want, b := range cases {
		if got := sniffExt(b); got != want {
			t.Errorf("sniffExt(%q) = %q, want %q", b, got, want)
		}
	}
}

// refChunkedVideo is LINE's video encryption written out from its web client:
// the MAC covers SHA-256 of each 131072-byte chunk of the ciphertext, and
// that list of hashes is what the MAC is computed over.
func refChunkedVideo(t *testing.T, kmB64 string, plain []byte) (blob, hashes []byte) {
	t.Helper()
	whole := refEncryptE2EEFile(t, kmB64, plain)
	body := whole[:len(plain)]
	for i := 0; i < len(body); i += 131072 {
		h := sha256.Sum256(body[i:min(i+131072, len(body))])
		hashes = append(hashes, h[:]...)
	}
	km, _ := base64.StdEncoding.DecodeString(kmB64)
	d, _ := hkdf.Key(sha256.New, km, nil, "FileEncryption", 76)
	m := hmac.New(sha256.New, d[32:64])
	m.Write(hashes)
	return append(append([]byte(nil), body...), m.Sum(nil)...), hashes
}

// A video's MAC covers its chunk hashes, as LINE's apps send videos; download
// must open it, and still refuse a changed chunk.
func TestDecryptE2EEFileChunkedVideo(t *testing.T) {
	km := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x77}, 32))
	for _, n := range []int{1, 131072, 131073, 2*131072 + 99} {
		plain := bytes.Repeat([]byte("video "), n/6+1)[:n]
		blob, _ := refChunkedVideo(t, km, plain)
		got, err := decryptE2EEFile(km, blob)
		if err != nil || !bytes.Equal(got, plain) {
			t.Fatalf("n=%d: %v", n, err)
		}
		blob[n-1] ^= 1
		if _, err := decryptE2EEFile(km, blob); err != errMacMismatch {
			t.Fatalf("n=%d: a changed chunk opened: %v", n, err)
		}
	}
}
