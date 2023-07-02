package payload

import (
	"encoding/json"

	"github.com/google/uuid"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/pblite"
)

func ReceiveMessages(rpcKey []byte) ([]byte, string, error) {
	payload := &binary.ReceiveMessagesRequest{
		Auth: &binary.AuthMessage{
			RequestID: uuid.New().String(),
			RpcKey:    rpcKey,
			Date: &binary.Date{
				Year: 2023,
				Seq1: 6,
				Seq2: 22,
				Seq3: 4,
				Seq4: 6,
			},
		},
		Unknown: &binary.ReceiveMessagesRequest_UnknownEmptyObject2{
			Unknown: &binary.ReceiveMessagesRequest_UnknownEmptyObject1{},
		},
	}
	data, err := pblite.Serialize(payload.ProtoReflect())
	if err != nil {
		return nil, "", err
	}
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, "", err
	}
	return jsonData, payload.Auth.RequestID, nil
}
