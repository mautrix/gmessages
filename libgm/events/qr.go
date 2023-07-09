package events

import (
	"go.mau.fi/mautrix-gmessages/libgm/binary"
)

type QR struct {
	URL string
}

type PairSuccessful struct {
	*binary.PairedData
}
