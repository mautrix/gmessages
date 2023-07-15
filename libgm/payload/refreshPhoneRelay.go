package payload

import (
	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

func RefreshPhoneRelay(rpcKey []byte) ([]byte, *binary.AuthenticationContainer, error) {
	payload := &binary.AuthenticationContainer{
		AuthMessage: &binary.AuthMessage{
			RequestID:        util.RandomUUIDv4(),
			Network:          &Network,
			TachyonAuthToken: rpcKey,
			ConfigVersion:    ConfigMessage,
		},
	}
	encodedPayload, err2 := proto.Marshal(payload)
	if err2 != nil {
		return nil, payload, err2
	}
	return encodedPayload, payload, nil
}
