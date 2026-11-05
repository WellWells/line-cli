package line

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
)

// fileMACChunk is the chunk size of a video's MAC: LINE MACs a video's
// ciphertext in 128 KiB chunks so its players can check it while streaming.
const fileMACChunk = 128 << 10

// errMacMismatch means the blob was not encrypted with this keyMaterial.
var errMacMismatch = errors.New("e2ee file: MAC mismatch")

// decryptE2EEFile decrypts a LINE E2EE media/file blob: ciphertext‖HMAC-SHA256(32).
// Keys come from HKDF-SHA256(keyMaterial, info="FileEncryption") → encKey(32)‖macKey(32)‖nonce(12),
// cipher is AES-256-CTR with a 12-byte nonce and a 32-bit big-endian block counter from 0.
// The MAC covers the ciphertext, or, for a video as LINE's apps send it, the
// SHA-256 hashes of its 128 KiB chunks.
func decryptE2EEFile(keyMaterialB64 string, blob []byte) ([]byte, error) {
	km, err := base64.StdEncoding.DecodeString(keyMaterialB64)
	if err != nil {
		return nil, err
	}
	if len(blob) < sha256.Size {
		return nil, errMacMismatch
	}
	d, err := hkdf.Key(sha256.New, km, nil, "FileEncryption", 76)
	if err != nil {
		return nil, err
	}
	encKey, macKey, nonce := d[:32], d[32:64], d[64:76]
	body, tag := blob[:len(blob)-sha256.Size], blob[len(blob)-sha256.Size:]
	m := hmac.New(sha256.New, macKey)
	m.Write(body)
	if !hmac.Equal(m.Sum(nil), tag) {
		m.Reset()
		m.Write(chunkHashes(body))
		if !hmac.Equal(m.Sum(nil), tag) {
			return nil, errMacMismatch
		}
	}
	block, err := aes.NewCipher(encKey)
	if err != nil {
		return nil, err
	}
	iv := make([]byte, aes.BlockSize)
	copy(iv, nonce)
	out := make([]byte, len(body))
	cipher.NewCTR(block, iv).XORKeyStream(out, body)
	return out, nil
}

// chunkHashes is SHA-256 of each 128 KiB chunk of b, concatenated: what a
// video's MAC covers.
func chunkHashes(b []byte) []byte {
	out := make([]byte, 0, (len(b)+fileMACChunk-1)/fileMACChunk*sha256.Size)
	for len(b) > 0 {
		n := min(len(b), fileMACChunk)
		h := sha256.Sum256(b[:n])
		out = append(out, h[:]...)
		b = b[n:]
	}
	return out
}
