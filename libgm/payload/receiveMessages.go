package payload

import (
	"github.com/google/uuid"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/pblite"
)

func ReceiveMessages(rpcKey []byte) ([]byte, string, error) {
	payload := &binary.ReceiveMessagesRequest{
		Auth: &binary.AuthMessage{
			RequestID:        uuid.New().String(),
			TachyonAuthToken: rpcKey,
			ConfigVersion:    ConfigMessage,
		},
		Unknown: &binary.ReceiveMessagesRequest_UnknownEmptyObject2{
			Unknown: &binary.ReceiveMessagesRequest_UnknownEmptyObject1{},
		},
	}
	jsonData, err := pblite.Marshal(payload)
	if err != nil {
		return nil, "", err
	}
	return jsonData, payload.Auth.RequestID, nil
}
