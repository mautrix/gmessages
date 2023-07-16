package crypto

import (
	"crypto/rand"
	"fmt"
)

func GenerateKey(length int) []byte {
	key := make([]byte, length)
	_, err := rand.Read(key)
	if err != nil {
		panic(fmt.Errorf("failed to read random bytes: %w", err))
	}
	return key
}
