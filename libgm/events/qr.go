package events

import (
	"go.mau.fi/mautrix-gmessages/libgm/binary"
)

type QRCODE_UPDATED struct {
	URL string
}

type PairSuccessful struct {
	*binary.Container
}
