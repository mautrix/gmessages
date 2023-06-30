package textgapi

import (
	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/crypto"
	"go.mau.fi/mautrix-gmessages/libgm/events"
	"go.mau.fi/mautrix-gmessages/libgm/util"

	"github.com/skip2/go-qrcode"
)

func (p *Pairer) GenerateQRCodeData() (string, error) {
	urlData := &binary.UrlData{
		PairingKey:      p.pairingKey,
		AES_CTR_KEY_256: p.client.cryptor.AES_CTR_KEY_256,
		SHA_256_KEY:     p.client.cryptor.SHA_256_KEY,
	}
	encodedUrlData, err := binary.EncodeProtoMessage(urlData)
	if err != nil {
		return "", err
	}
	cData := crypto.Base64Encode(encodedUrlData)
	return util.QR_CODE_URL + cData, nil
}

func (p *Pairer) GenerateQRCode(size int) (*events.QRCODE_UPDATED, error) {
	data, err1 := p.GenerateQRCodeData()
	if err1 != nil {
		return nil, err1
	}
	png, err2 := qrcode.Encode(data, qrcode.Highest, size)
	if err2 != nil {
		return nil, err2
	}
	return events.NewQrCodeUpdated(png, size, size, data), nil
}
