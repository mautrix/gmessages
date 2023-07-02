package payload

import (
	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

func GetWebEncryptionKey(WebPairKey []byte) ([]byte, *binary.Container, error) {
	id := util.RandomUUIDv4()
	payload := &binary.Container{
		PhoneRelay: &binary.PhoneRelayBody{
			ID:     id,
			RpcKey: WebPairKey,
			Date: &binary.Date{
				Year: 2023,
				Seq1: 6,
				Seq2: 22,
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
