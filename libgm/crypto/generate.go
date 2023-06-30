package crypto

import (
	"crypto/rand"
)

func GenerateKey(length int) ([]byte, error) {
	key := make([]byte, length)
	_, err := rand.Read(key)
	if err != nil {
		return nil, err
	}
	return key, nil
}

func GenerateKeys() ([]byte, []byte) {
	key, err := GenerateKey(32)
	if err != nil {
		panic(err)
	}
	key2, err2 := GenerateKey(32)
	if err2 != nil {
		panic(err2)
	}
	return key, key2
}
