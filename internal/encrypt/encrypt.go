// Package encrypt does AES-128 encryption of whole HLS segments (audio and
// video): each segment in AES-128-CBC with PKCS7 and its own random IV, plus
// EXT-X-KEY lines for the ffmpeg media playlist. The IV is never written into
// the data: it lives only in the playlist
package encrypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
)

// HLS encrypts segments with one job key
type HLS struct {
	key  []byte
	size int // key and IV length, bytes
}

// New returns encryption with key, the key is exactly 16 bytes
func New(key []byte) (*HLS, error) {
	a := &HLS{}
	a.init()

	if len(key) != a.size {
		return nil, fmt.Errorf("encrypt: key must be %d bytes", a.size)
	}

	a.key = key
	return a, nil
}

// init sets constant values: AES-128
func (a *HLS) init() {
	a.size = 16
}

// Segment returns data with PKCS7, encrypted with AES-128-CBC, and the random IV
// used to encrypt it
func (a *HLS) Segment(data []byte) (encrypted, iv []byte, err error) {
	block, err := aes.NewCipher(a.key)
	if err != nil {
		return nil, nil, err
	}

	// a new random IV on every call: never repeats across
	// segments, renditions and retries
	iv = make([]byte, aes.BlockSize)
	_, err = rand.Read(iv)
	if err != nil {
		return nil, nil, err
	}

	// PKCS7: always 1..16 padding bytes, each equal to its length
	pad := aes.BlockSize - len(data)%aes.BlockSize
	encrypted = make([]byte, len(data)+pad)
	copy(encrypted, data)
	for i := len(data); i < len(encrypted); i++ {
		encrypted[i] = byte(pad)
	}

	// encrypt in place
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(encrypted, encrypted)
	return encrypted, iv, nil
}
