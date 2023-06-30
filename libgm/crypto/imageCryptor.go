package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"math"
)

type ImageCryptor struct {
	key []byte
}

func NewImageCryptor(key []byte) (*ImageCryptor, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("unsupported AES key length (got=%d expected=32)", len(key))
	}
	return &ImageCryptor{key: key}, nil
}

func (ic *ImageCryptor) GetKey() []byte {
	return ic.key
}

func (ic *ImageCryptor) UpdateDecryptionKey(key []byte) {
	ic.key = key
}

func (ic *ImageCryptor) Encrypt(imageBytes []byte, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(ic.key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	_, err = rand.Read(nonce)
	if err != nil {
		return nil, err
	}

	ciphertext := gcm.Seal(nonce, nonce, imageBytes, aad)
	return ciphertext, nil
}

func (ic *ImageCryptor) Decrypt(iv []byte, data []byte, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(ic.key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	if len(data) < gcm.NonceSize() {
		return nil, fmt.Errorf("invalid encrypted data length (got=%d)", len(data))
	}

	ciphertext := data[gcm.NonceSize():]

	decrypted, err := gcm.Open(nil, iv, ciphertext, aad)
	if err != nil {
		return nil, err
	}

	return decrypted, nil
}

func (ic *ImageCryptor) EncryptData(data []byte) ([]byte, error) {
	rawChunkSize := 1 << 15
	chunkSize := rawChunkSize - 28
	var tasks []chan []byte
	chunkIndex := 0

	for i := 0; i < len(data); i += chunkSize {
		if i+chunkSize > len(data) {
			chunkSize = len(data) - i
		}

		chunk := make([]byte, chunkSize)
		copy(chunk, data[i:i+chunkSize])

		aad := ic.calculateAAD(chunkIndex, i+chunkSize, len(data))
		tasks = append(tasks, make(chan []byte))
		go func(chunk, aad []byte, task chan []byte) {
			encrypted, err := ic.Encrypt(chunk, aad)
			if err != nil {
				fmt.Println(err)
				task <- nil
			} else {
				task <- encrypted
			}
		}(chunk, aad, tasks[chunkIndex])

		chunkIndex++
	}

	var result [][]byte
	for _, task := range tasks {
		encrypted := <-task
		if encrypted == nil {
			continue
		}
		result = append(result, encrypted)
	}

	var concatted []byte
	for _, r := range result {
		concatted = append(concatted, r...)
	}

	encryptedHeader := []byte{0, byte(math.Log2(float64(rawChunkSize)))}

	return append(encryptedHeader, concatted...), nil
}

func (ic *ImageCryptor) DecryptData(encryptedData []byte) ([]byte, error) {
	if len(encryptedData) == 0 || len(ic.key) != 32 {
		return encryptedData, nil
	}
	if encryptedData[0] != 0 {
		return nil, fmt.Errorf("invalid first-byte header signature (got=%o , expected=%o)", encryptedData[0], 0)
	}

	chunkSize := 1 << encryptedData[1]
	encryptedData = encryptedData[2:]

	var tasks []chan []byte
	chunkIndex := 0

	for i := 0; i < len(encryptedData); i += chunkSize {
		if i+chunkSize > len(encryptedData) {
			chunkSize = len(encryptedData) - i
		}

		chunk := make([]byte, chunkSize)
		copy(chunk, encryptedData[i:i+chunkSize])

		iv := chunk[:12]
		aad := ic.calculateAAD(chunkIndex, i+chunkSize, len(encryptedData))
		tasks = append(tasks, make(chan []byte))
		go func(iv, chunk, aad []byte, task chan []byte) {
			decrypted, err := ic.Decrypt(iv, chunk, aad)
			if err != nil {
				fmt.Println(err)
				task <- nil
			} else {
				task <- decrypted
			}
		}(iv, chunk, aad, tasks[chunkIndex])

		chunkIndex++
	}

	var result [][]byte
	for _, task := range tasks {
		decrypted := <-task
		if decrypted == nil {
			continue
		}
		result = append(result, decrypted)
	}

	var concatted []byte
	for _, r := range result {
		concatted = append(concatted, r...)
	}

	return concatted, nil
}

func (ic *ImageCryptor) calculateAAD(index, end, total int) []byte {
	aad := make([]byte, 5)

	i := 4
	for index > 0 {
		aad[i] = byte(index % 256)
		index = index / 256
		i--
	}

	if end >= total {
		aad[0] = 1
	}

	return aad
}
