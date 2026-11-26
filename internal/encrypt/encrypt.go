// Package encrypt — AES-128 шифрование целых сегментов HLS (аудио и
// видео): каждый сегмент в AES-128-CBC с PKCS7 и своим случайным IV, и
// строки EXT-X-KEY для media playlist ffmpeg. IV никогда не пишется в
// данные: он живёт только в playlist
package encrypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
)

// HLS — шифрование сегментов одним ключом job
type HLS struct {
	key  []byte
	size int // длина ключа и IV, байт
}

// New — шифрование ключом key, ключ ровно 16 байт
func New(key []byte) (*HLS, error) {
	a := &HLS{}
	a.init()

	if len(key) != a.size {
		return nil, fmt.Errorf("encrypt: key must be %d bytes", a.size)
	}

	a.key = key
	return a, nil
}

// init — постоянные значения: AES-128
func (a *HLS) init() {
	a.size = 16
}

// Segment — data с PKCS7, зашифрованные AES-128-CBC, и случайный IV,
// которым шифровали
func (a *HLS) Segment(data []byte) (encrypted, iv []byte, err error) {
	block, err := aes.NewCipher(a.key)
	if err != nil {
		return nil, nil, err
	}

	// новый случайный IV на каждый вызов: никогда не повторяется между
	// сегментами, вариантами и повторами
	iv = make([]byte, aes.BlockSize)
	_, err = rand.Read(iv)
	if err != nil {
		return nil, nil, err
	}

	// PKCS7: всегда 1..16 байт добивки, каждый равен её длине
	pad := aes.BlockSize - len(data)%aes.BlockSize
	encrypted = make([]byte, len(data)+pad)
	copy(encrypted, data)
	for i := len(data); i < len(encrypted); i++ {
		encrypted[i] = byte(pad)
	}

	// шифруем на месте
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(encrypted, encrypted)
	return encrypted, iv, nil
}
