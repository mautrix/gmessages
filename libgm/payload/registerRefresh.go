package payload

import (
	"encoding/json"

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
		EmptyRefreshArr:   &binary.EmptyRefreshArr{EmptyArr: &binary.EmptyEmptyArr{}},
		MessageType:       2, // hmm
	}

	serialized, serializeErr := pblite.Serialize(payload.ProtoReflect())
	if serializeErr != nil {
		return nil, serializeErr
	}

	jsonMessage, marshalErr := json.Marshal(serialized)
	if marshalErr != nil {
		return nil, marshalErr
	}

	return jsonMessage, nil
}
