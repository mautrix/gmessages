package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"math"
)

type AESGCMHelper struct {
	key []byte
	gcm cipher.AEAD
}

func NewAESGCMHelper(key []byte) (*AESGCMHelper, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("unsupported AES key length (got=%d expected=32)", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	return &AESGCMHelper{key: key, gcm: gcm}, nil
}

func (c *AESGCMHelper) encryptChunk(data []byte, aad []byte) []byte {
	nonce := make([]byte, c.gcm.NonceSize(), c.gcm.NonceSize()+len(data))
	_, err := rand.Read(nonce)
	if err != nil {
		panic(fmt.Errorf("out of randomness: %w", err))
	}

	// Pass nonce as the dest, so we have it pre-appended to the output
	return c.gcm.Seal(nonce, nonce, data, aad)
}

func (c *AESGCMHelper) decryptChunk(data []byte, aad []byte) ([]byte, error) {
	if len(data) < c.gcm.NonceSize() {
		return nil, fmt.Errorf("invalid encrypted data length (got=%d)", len(data))
	}

	nonce := data[:c.gcm.NonceSize()]
	ciphertext := data[c.gcm.NonceSize():]

	decrypted, err := c.gcm.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, err
	}

	return decrypted, nil
}

const outgoingRawChunkSize = 1 << 15

func (c *AESGCMHelper) EncryptData(data []byte) ([]byte, error) {
	chunkOverhead := c.gcm.NonceSize() + c.gcm.Overhead()
	chunkSize := outgoingRawChunkSize - chunkOverhead

	chunkCount := int(math.Ceil(float64(len(data)) / float64(chunkSize)))
	encrypted := make([]byte, 2, 2+len(data)+28*chunkCount)
	encrypted[0] = 0
	encrypted[1] = byte(math.Log2(float64(outgoingRawChunkSize)))

	var chunkIndex uint32
	for i := 0; i < len(data); i += chunkSize {
		isLastChunk := false
		if i+chunkSize >= len(data) {
			chunkSize = len(data) - i
			isLastChunk = true
		}

		chunk := make([]byte, chunkSize)
		copy(chunk, data[i:i+chunkSize])

		aad := c.calculateAAD(chunkIndex, isLastChunk)
		encrypted = append(encrypted, c.encryptChunk(data[i:i+chunkSize], aad)...)
		chunkIndex++
	}

	return encrypted, nil
}

func (c *AESGCMHelper) DecryptData(encryptedData []byte) ([]byte, error) {
	if len(encryptedData) == 0 || len(c.key) != 32 {
		return encryptedData, nil
	}
	if encryptedData[0] != 0 {
		return nil, fmt.Errorf("invalid first-byte header signature (got=%o , expected=%o)", encryptedData[0], 0)
	}

	chunkSize := 1 << encryptedData[1]
	encryptedData = encryptedData[2:]

	var chunkIndex uint32
	chunkCount := int(math.Ceil(float64(len(encryptedData)) / float64(chunkSize)))
	decryptedData := make([]byte, 0, len(encryptedData)-28*chunkCount)

	for i := 0; i < len(encryptedData); i += chunkSize {
		isLastChunk := false
		if i+chunkSize >= len(encryptedData) {
			chunkSize = len(encryptedData) - i
			isLastChunk = true
		}

		chunk := make([]byte, chunkSize)
		copy(chunk, encryptedData[i:i+chunkSize])

		aad := c.calculateAAD(chunkIndex, isLastChunk)
		decryptedChunk, err := c.decryptChunk(chunk, aad)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt chunk #%d: %w", chunkIndex+1, err)
		}
		decryptedData = append(decryptedData, decryptedChunk...)
		chunkIndex++
	}

	return decryptedData, nil
}

func (c *AESGCMHelper) calculateAAD(index uint32, isLastChunk bool) []byte {
	aad := make([]byte, 5)
	binary.BigEndian.PutUint32(aad[1:5], index)
	if isLastChunk {
		aad[0] = 1
	}
	return aad
}
