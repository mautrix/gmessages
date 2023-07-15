package crypto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
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

func (t *JWK) MarshalPubKey() ([]byte, error) {
	pubKey, err := t.GetPublicKey()
	if err != nil {
		return nil, err
	}
	return elliptic.Marshal(pubKey.Curve, pubKey.X, pubKey.Y), nil
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
