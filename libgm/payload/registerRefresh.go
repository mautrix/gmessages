package payload

import (
	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/pblite"
)

func RegisterRefresh(sig []byte, requestID string, timestamp int64, browser *binary.Device, tachyonAuthToken []byte) ([]byte, error) {
	payload := &binary.RegisterRefreshPayload{
		MessageAuth: &binary.AuthMessage{
			RequestID:        requestID,
			TachyonAuthToken: tachyonAuthToken,
			ConfigVersion:    ConfigMessage,
		},
		CurrBrowserDevice: browser,
		UnixTimestamp:     timestamp,
		Signature:         sig,
		EmptyRefreshArr:   &binary.EmptyRefreshArr{EmptyArr: &binary.EmptyArr{}},
		MessageType:       2, // hmm
	}

	jsonMessage, serializeErr := pblite.Marshal(payload)
	if serializeErr != nil {
		return nil, serializeErr
	}

	return jsonMessage, nil
}
