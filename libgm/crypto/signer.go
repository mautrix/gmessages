package crypto

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
)

func (t *JWK) SignRequest(requestID string, timestamp int64) ([]byte, error) {
	signBytes := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", requestID, timestamp)))

	privKey, privErr := t.GetPrivateKey()
	if privErr != nil {
		return nil, privErr
	}

	return ecdsa.SignASN1(rand.Reader, privKey, signBytes[:])
}
