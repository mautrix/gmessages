package payload

import (
	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

func RefreshPhoneRelay(rpcKey []byte) ([]byte, *binary.AuthenticationContainer, error) {
	payload := &binary.AuthenticationContainer{
		AuthMessage: &binary.AuthenticationMessage{
			RequestID:        util.RandomUUIDv4(),
			Network:          Network,
			TachyonAuthToken: rpcKey,
			ConfigVersion:    ConfigMessage,
		},
	}
	encodedPayload, err2 := binary.EncodeProtoMessage(payload)
	if err2 != nil {
		return nil, payload, err2
	}
	return encodedPayload, payload, nil
}
