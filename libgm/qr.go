package libgm

import (
	"encoding/base64"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

func (p *Pairer) GenerateQRCodeData() (string, error) {
	urlData := &binary.UrlData{
		PairingKey: p.pairingKey,
		AESKey:     p.client.authData.Cryptor.AESKey,
		HMACKey:    p.client.authData.Cryptor.HMACKey,
	}
	encodedUrlData, err := binary.EncodeProtoMessage(urlData)
	if err != nil {
		return "", err
	}
	cData := base64.StdEncoding.EncodeToString(encodedUrlData)
	return util.QRCodeURLBase + cData, nil
}
