package crypto

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

func (t *JWK) SignRequest(requestId string, timestamp int64) (string, error) {
	signBytes := []byte(fmt.Sprintf("%s:%d", requestId, timestamp))

	privKey, privErr := t.GetPrivateKey()
	if privErr != nil {
		return "", privErr
	}

	signature, sigErr := t.sign(privKey, signBytes)
	if sigErr != nil {
		return "", sigErr
	}
	encodedSignature := base64.StdEncoding.EncodeToString(signature)
	return encodedSignature, nil
}

func (t *JWK) sign(key *ecdsa.PrivateKey, msg []byte) ([]byte, error) {
	hash := sha256.Sum256(msg)
	return ecdsa.SignASN1(rand.Reader, key, hash[:])
}
