package events

import (
	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
)

type QR struct {
	URL string
}

type PairSuccessful struct {
	PhoneID string
	QRData  *gmproto.PairedData
}
