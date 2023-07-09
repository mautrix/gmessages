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
	r, s, err := ecdsa.Sign(rand.Reader, key, hash[:])
	if err != nil {
		return nil, err
	}

	rBytes := r.Bytes()
	sBytes := s.Bytes()

	rBytes = EncodeBNA(rBytes)
	sBytes = EncodeBNA(sBytes)

	sigLen := len(rBytes) + len(sBytes) + 6 // 2 bytes for each sequence tag and 2 bytes for each length field
	sig := make([]byte, sigLen)
	sig[0] = 48
	sig[1] = byte(sigLen - 2)
	sig[2] = 2
	sig[3] = byte(len(rBytes))
	copy(sig[4:], rBytes)
	sig[4+len(rBytes)] = 2
	sig[5+len(rBytes)] = byte(len(sBytes))
	copy(sig[6+len(rBytes):], sBytes)

	return sig, nil
}
