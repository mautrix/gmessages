package libgm

import (
	"encoding/base64"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

func (p *Pairer) GenerateQRCodeData() (string, error) {
	urlData := &binary.UrlData{
		PairingKey:   p.pairingKey,
		AESCTR256Key: p.client.cryptor.AESCTR256Key,
		SHA256Key:    p.client.cryptor.SHA256Key,
	}
	encodedUrlData, err := binary.EncodeProtoMessage(urlData)
	if err != nil {
		return "", err
	}
	cData := base64.StdEncoding.EncodeToString(encodedUrlData)
	return util.QRCodeURLBase + cData, nil
}
