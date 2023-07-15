package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
)

type Cryptor struct {
	AESKey  []byte `json:"aes_key"`
	HMACKey []byte `json:"hmac_key"`
}

func NewCryptor(aesKey []byte, hmacKey []byte) *Cryptor {
	if aesKey != nil && hmacKey != nil {
		return &Cryptor{
			AESKey:  aesKey,
			HMACKey: hmacKey,
		}
	}
	aesKey, hmacKey = GenerateKeys()
	return &Cryptor{
		AESKey:  aesKey,
		HMACKey: hmacKey,
	}
}

func (c *Cryptor) Encrypt(plaintext []byte) ([]byte, error) {
	iv := make([]byte, aes.BlockSize)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(c.AESKey)
	if err != nil {
		return nil, err
	}

	ciphertext := make([]byte, len(plaintext))
	stream := cipher.NewCTR(block, iv)
	stream.XORKeyStream(ciphertext, plaintext)

	ciphertext = append(ciphertext, iv...)

	mac := hmac.New(sha256.New, c.HMACKey)
	mac.Write(ciphertext)
	hmac := mac.Sum(nil)

	ciphertext = append(ciphertext, hmac...)

	return ciphertext, nil
}

func (c *Cryptor) Decrypt(encryptedData []byte) ([]byte, error) {
	if len(encryptedData) < 48 {
		return nil, errors.New("input data is too short")
	}

	hmacSignature := encryptedData[len(encryptedData)-32:]
	encryptedDataWithoutHMAC := encryptedData[:len(encryptedData)-32]

	mac := hmac.New(sha256.New, c.HMACKey)
	mac.Write(encryptedDataWithoutHMAC)
	expectedHMAC := mac.Sum(nil)

	if !hmac.Equal(hmacSignature, expectedHMAC) {
		return nil, errors.New("HMAC mismatch")
	}

	iv := encryptedDataWithoutHMAC[len(encryptedDataWithoutHMAC)-16:]
	encryptedDataWithoutHMAC = encryptedDataWithoutHMAC[:len(encryptedDataWithoutHMAC)-16]

	block, err := aes.NewCipher(c.AESKey)
	if err != nil {
		return nil, err
	}
	stream := cipher.NewCTR(block, iv)
	stream.XORKeyStream(encryptedDataWithoutHMAC, encryptedDataWithoutHMAC)

	return encryptedDataWithoutHMAC, nil
}
