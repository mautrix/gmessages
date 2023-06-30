package crypto

import (
	"crypto/rand"
	"log"
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
		log.Fatal(err)
	}
	key2, err2 := GenerateKey(32)
	if err2 != nil {
		log.Fatal(err2)
	}
	return key, key2
}