package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"

	"google.golang.org/protobuf/reflect/protoreflect"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
)

type Cryptor struct {
	AESCTR256Key []byte
	SHA256Key    []byte
}

func NewCryptor(aesKey []byte, shaKey []byte) *Cryptor {
	if aesKey != nil && shaKey != nil {
		return &Cryptor{
			AESCTR256Key: aesKey,
			SHA256Key:    shaKey,
		}
	}
	aesKey, shaKey = GenerateKeys()
	return &Cryptor{
		AESCTR256Key: aesKey,
		SHA256Key:    shaKey,
	}
}

func (c *Cryptor) Encrypt(plaintext []byte) ([]byte, error) {
	iv := make([]byte, aes.BlockSize)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(c.AESCTR256Key)
	if err != nil {
		return nil, err
	}

	ciphertext := make([]byte, len(plaintext))
	stream := cipher.NewCTR(block, iv)
	stream.XORKeyStream(ciphertext, plaintext)

	ciphertext = append(ciphertext, iv...)

	mac := hmac.New(sha256.New, c.SHA256Key)
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

	mac := hmac.New(sha256.New, c.SHA256Key)
	mac.Write(encryptedDataWithoutHMAC)
	expectedHMAC := mac.Sum(nil)

	if !hmac.Equal(hmacSignature, expectedHMAC) {
		return nil, errors.New("HMAC mismatch")
	}

	iv := encryptedDataWithoutHMAC[len(encryptedDataWithoutHMAC)-16:]
	encryptedDataWithoutHMAC = encryptedDataWithoutHMAC[:len(encryptedDataWithoutHMAC)-16]

	block, err := aes.NewCipher(c.AESCTR256Key)
	if err != nil {
		return nil, err
	}
	stream := cipher.NewCTR(block, iv)
	stream.XORKeyStream(encryptedDataWithoutHMAC, encryptedDataWithoutHMAC)

	return encryptedDataWithoutHMAC, nil
}

func (c *Cryptor) DecryptAndDecodeData(encryptedData []byte, message protoreflect.ProtoMessage) error {
	decryptedData, err := c.Decrypt(encryptedData)
	if err != nil {
		return err
	}
	err = binary.DecodeProtoMessage(decryptedData, message)
	if err != nil {
		return err
	}
	return nil
}
