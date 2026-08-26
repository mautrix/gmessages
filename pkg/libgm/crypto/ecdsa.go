package crypto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"go.mau.fi/util/exerrors"
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

func (t *JWK) GetPrivateKey() (*ecdsa.PrivateKey, error) {
	d := t.D
	if len(d) < 32 {
		d = append(make([]byte, 32-len(d)), d...)
	}
	return ecdsa.ParseRawPrivateKey(elliptic.P256(), d)
}

func (t *JWK) GetPublicKey() (*ecdsa.PublicKey, error) {
	if len(t.X) > 32 || len(t.Y) > 32 {
		return nil, fmt.Errorf("invalid public key length: X=%d, Y=%d", len(t.X), len(t.Y))
	}
	publicKeyBytes := make([]byte, 65)
	publicKeyBytes[0] = 4
	copy(publicKeyBytes[33-len(t.X):33], t.X)
	copy(publicKeyBytes[65-len(t.Y):], t.Y)
	return ecdsa.ParseUncompressedPublicKey(elliptic.P256(), publicKeyBytes)
}

// GenerateECDSAKey generates a new ECDSA private key with P-256 curve
func GenerateECDSAKey() *JWK {
	privKey := exerrors.Must(ecdsa.GenerateKey(elliptic.P256(), rand.Reader))
	privateKeyBytes := exerrors.Must(privKey.Bytes())
	publicKeyBytes := exerrors.Must(privKey.PublicKey.Bytes())
	return &JWK{
		KeyType: "EC",
		Curve:   "P-256",
		D:       privateKeyBytes,
		X:       publicKeyBytes[1:33],
		Y:       publicKeyBytes[33:],
	}
}
