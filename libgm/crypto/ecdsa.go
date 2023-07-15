package crypto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"math/big"
)

type JWK struct {
	KeyType string `json:"kty"`
	Curve   string `json:"crv"`
	D       string `json:"d"`
	X       string `json:"x"`
	Y       string `json:"y"`
}

func (t *JWK) GetPrivateKey() (*ecdsa.PrivateKey, error) {
	curve := elliptic.P256()
	xBytes, err := base64.RawURLEncoding.DecodeString(t.X)
	if err != nil {
		return nil, err
	}
	yBytes, err := base64.RawURLEncoding.DecodeString(t.Y)
	if err != nil {
		return nil, err
	}
	dBytes, err := base64.RawURLEncoding.DecodeString(t.D)
	if err != nil {
		return nil, err
	}

	priv := &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{
			Curve: curve,
			X:     new(big.Int).SetBytes(xBytes),
			Y:     new(big.Int).SetBytes(yBytes),
		},
		D: new(big.Int).SetBytes(dBytes),
	}
	return priv, nil
}

func (t *JWK) GetPublicKey() (*ecdsa.PublicKey, error) {
	xBytes, err := base64.RawURLEncoding.DecodeString(t.X)
	if err != nil {
		return nil, err
	}
	yBytes, err := base64.RawURLEncoding.DecodeString(t.Y)
	if err != nil {
		return nil, err
	}

	x := new(big.Int).SetBytes(xBytes)
	y := new(big.Int).SetBytes(yBytes)
	pubKey := &ecdsa.PublicKey{
		Curve: elliptic.P256(),
		X:     x,
		Y:     y,
	}
	return pubKey, nil
}

func (t *JWK) MarshalX509PublicKey() ([]byte, error) {
	pubKey, err := t.GetPublicKey()
	if err != nil {
		return nil, err
	}
	return x509.MarshalPKIXPublicKey(pubKey)
}

func (t *JWK) SignRequest(requestID string, timestamp int64) ([]byte, error) {
	signBytes := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", requestID, timestamp)))

	privKey, privErr := t.GetPrivateKey()
	if privErr != nil {
		return nil, privErr
	}

	return ecdsa.SignASN1(rand.Reader, privKey, signBytes[:])
}

// GenerateECDSAKey generates a new ECDSA private key with P-256 curve
func GenerateECDSAKey() (*JWK, error) {
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	return &JWK{
		KeyType: "EC",
		Curve:   "P-256",
		D:       base64.RawURLEncoding.EncodeToString(privKey.D.Bytes()),
		X:       base64.RawURLEncoding.EncodeToString(privKey.X.Bytes()),
		Y:       base64.RawURLEncoding.EncodeToString(privKey.Y.Bytes()),
	}, nil
}
