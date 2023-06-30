package payload

import (
	"encoding/base64"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

func RefreshPhoneRelay(rpcKey string) ([]byte, *binary.Container, error) {
	decodedRpcKey, err1 := base64.StdEncoding.DecodeString(rpcKey)
	if err1 != nil {
		return nil, nil, err1
	}
	payload := &binary.Container{
		PhoneRelay: &binary.PhoneRelayBody{
			Id:     util.RandomUUIDv4(),
			Bugle:  "Bugle",
			RpcKey: decodedRpcKey,
			Date: &binary.Date{
				Year: 2023,
				Seq1: 6,
				Seq2: 8,
				Seq3: 4,
				Seq4: 6,
			},
		},
	}
	encodedPayload, err2 := binary.EncodeProtoMessage(payload)
	if err2 != nil {
		return nil, payload, err2
	}
	return encodedPayload, payload, nil
}
