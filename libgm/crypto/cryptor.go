package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"

	"google.golang.org/protobuf/reflect/protoreflect"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
)

type Cryptor struct {
	AES_CTR_KEY_256, SHA_256_KEY []byte
}

func NewCryptor(aes_key []byte, sha_key []byte) *Cryptor {
	if aes_key != nil && sha_key != nil {
		return &Cryptor{
			AES_CTR_KEY_256: aes_key,
			SHA_256_KEY:     sha_key,
		}
	}
	aes_key, sha_key = GenerateKeys()
	return &Cryptor{
		AES_CTR_KEY_256: aes_key,
		SHA_256_KEY:     sha_key,
	}
}

func (c *Cryptor) SaveAsJson() {
	AES_B64, SHA_B64 := EncodeBase64Standard(c.AES_CTR_KEY_256), EncodeBase64Standard(c.SHA_256_KEY)
	inter := struct {
		AES_CTR_KEY_256 string
		SHA_256_KEY     string
	}{
		AES_CTR_KEY_256: AES_B64,
		SHA_256_KEY:     SHA_B64,
	}
	jsonData, _ := json.Marshal(inter)
	os.WriteFile("cryptor.json", jsonData, os.ModePerm)
}

func (c *Cryptor) Encrypt(plaintext []byte) ([]byte, error) {
	iv := make([]byte, aes.BlockSize)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(c.AES_CTR_KEY_256)
	if err != nil {
		return nil, err
	}

	ciphertext := make([]byte, len(plaintext))
	stream := cipher.NewCTR(block, iv)
	stream.XORKeyStream(ciphertext, plaintext)

	ciphertext = append(ciphertext, iv...)

	mac := hmac.New(sha256.New, c.SHA_256_KEY)
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

	mac := hmac.New(sha256.New, c.SHA_256_KEY)
	mac.Write(encryptedDataWithoutHMAC)
	expectedHMAC := mac.Sum(nil)

	if !hmac.Equal(hmacSignature, expectedHMAC) {
		return nil, errors.New("HMAC mismatch")
	}

	iv := encryptedDataWithoutHMAC[len(encryptedDataWithoutHMAC)-16:]
	encryptedDataWithoutHMAC = encryptedDataWithoutHMAC[:len(encryptedDataWithoutHMAC)-16]

	block, err := aes.NewCipher(c.AES_CTR_KEY_256)
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
