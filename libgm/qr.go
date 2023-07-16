package libgm

import (
	"encoding/base64"

	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

func (c *Client) GenerateQRCodeData(pairingKey []byte) (string, error) {
	urlData := &binary.URLData{
		PairingKey: pairingKey,
		AESKey:     c.AuthData.RequestCrypto.AESKey,
		HMACKey:    c.AuthData.RequestCrypto.HMACKey,
	}
	encodedURLData, err := proto.Marshal(urlData)
	if err != nil {
		return "", err
	}
	cData := base64.StdEncoding.EncodeToString(encodedURLData)
	return util.QRCodeURLBase + cData, nil
}
