package libgm

import (
	"encoding/base64"

	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

func (p *Pairer) GenerateQRCodeData() (string, error) {
	urlData := &binary.URLData{
		PairingKey: p.pairingKey,
		AESKey:     p.client.authData.RequestCrypto.AESKey,
		HMACKey:    p.client.authData.RequestCrypto.HMACKey,
	}
	encodedURLData, err := proto.Marshal(urlData)
	if err != nil {
		return "", err
	}
	cData := base64.StdEncoding.EncodeToString(encodedURLData)
	return util.QRCodeURLBase + cData, nil
}
