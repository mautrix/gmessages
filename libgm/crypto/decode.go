package crypto

import (
	"encoding/base64"

	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
)

func DecodeAndEncodeB64(data string, msg proto.Message) error {
	decodedBytes, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return err
	}
	err = binary.DecodeProtoMessage(decodedBytes, msg)
	if err != nil {
		return err
	}
	return nil
}
