package crypto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
)

type JWK struct {
	Kty          string   `json:"kty"`
	Crv          string   `json:"crv"`
	D            string   `json:"d"`
	X            string   `json:"x"`
	Y            string   `json:"y"`
	Ext          bool     `json:"ext"`
	KeyOps       []string `json:"key_ops"`
	PrivateBytes []byte   `json:"privateBytes,omitempty"`
}

// Returns a byte slice containing the JWK and an error if the generation or export failed.
func (t *JWK) Marshal() ([]byte, error) {
	JWKJSON, err := json.Marshal(t)
	if err != nil {
		fmt.Printf("Failed to marshal JWK: %v", err)
		return nil, err
	}
	fmt.Printf("%s\n", JWKJSON)
	return JWKJSON, err
}

func (t *JWK) PrivKeyB64Bytes() ([]byte, error) {
	decodedPrivateKey, err2 := base64.RawURLEncoding.DecodeString(t.D)
	return decodedPrivateKey, err2
}

func (t *JWK) ExtractPublicKeyDetails(pubKey []byte) *JWK {
	x := base64.RawURLEncoding.EncodeToString(pubKey[1:33])
	y := base64.RawURLEncoding.EncodeToString(pubKey[33:])
	return &JWK{
		Kty: "EC",
		Crv: "P-256",
		X:   x,
		Y:   y,
	}
}

func (t *JWK) DecompressPubkey() (*ecdsa.PublicKey, error) {
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

func (t *JWK) UncompressPubKey() ([]byte, error) {
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

	uncompressedPubKey := elliptic.Marshal(pubKey.Curve, pubKey.X, pubKey.Y)

	return uncompressedPubKey, nil
}

// GenerateECDSA_P256_JWK generates a new ECDSA private key with P-256 curve
func GenerateECDSA_P256_JWK() (*JWK, error) {
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		fmt.Printf("Failed to generate private key: %v", err)
		return nil, err
	}

	JWK := &JWK{
		Kty:    "EC",
		Crv:    "P-256",
		D:      base64.RawURLEncoding.EncodeToString(privKey.D.Bytes()),
		X:      base64.RawURLEncoding.EncodeToString(privKey.X.Bytes()),
		Y:      base64.RawURLEncoding.EncodeToString(privKey.Y.Bytes()),
		Ext:    true,
		KeyOps: []string{"sign"},
	}
	return JWK, nil
}
