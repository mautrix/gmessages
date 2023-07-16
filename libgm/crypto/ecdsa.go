package crypto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"math/big"
)

type RawURLBytes []byte

func (rub RawURLBytes) MarshalJSON() ([]byte, error) {
	out := make([]byte, 2+base64.RawURLEncoding.EncodedLen(len(rub)))
	out[0] = '"'
	base64.RawURLEncoding.Encode(out[1:], rub)
	out[len(out)-1] = '"'
	return out, nil
}

func (rub *RawURLBytes) UnmarshalJSON(in []byte) error {
	if len(in) < 2 || in[0] != '"' || in[len(in)-1] != '"' {
		return fmt.Errorf("invalid value for RawURLBytes: not a JSON string")
	}
	*rub = make([]byte, base64.RawURLEncoding.DecodedLen(len(in)-2))
	_, err := base64.RawURLEncoding.Decode(*rub, in[1:len(in)-1])
	return err
}

type JWK struct {
	KeyType string      `json:"kty"`
	Curve   string      `json:"crv"`
	D       RawURLBytes `json:"d"`
	X       RawURLBytes `json:"x"`
	Y       RawURLBytes `json:"y"`
}

func (t *JWK) GetPrivateKey() *ecdsa.PrivateKey {
	return &ecdsa.PrivateKey{
		PublicKey: *t.GetPublicKey(),
		D:         new(big.Int).SetBytes(t.D),
	}
}

func (t *JWK) GetPublicKey() *ecdsa.PublicKey {
	return &ecdsa.PublicKey{
		Curve: elliptic.P256(),
		X:     new(big.Int).SetBytes(t.X),
		Y:     new(big.Int).SetBytes(t.Y),
	}
}

// GenerateECDSAKey generates a new ECDSA private key with P-256 curve
func GenerateECDSAKey() *JWK {
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(fmt.Errorf("failed to generate ecdsa key: %w", err))
	}
	return &JWK{
		KeyType: "EC",
		Curve:   "P-256",
		D:       privKey.D.Bytes(),
		X:       privKey.X.Bytes(),
		Y:       privKey.Y.Bytes(),
	}
}
