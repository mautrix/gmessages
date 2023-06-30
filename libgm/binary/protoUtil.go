package binary

import (
	"fmt"
	"google.golang.org/protobuf/proto"
)

func EncodeProtoMessage(message proto.Message) ([]byte, error) {
	data, err := proto.Marshal(message)
	if err != nil {
		return nil, fmt.Errorf("failed to encode proto message: %v", err)
	}
	return data, nil
}

func DecodeProtoMessage(data []byte, message proto.Message) error {
	err := proto.Unmarshal(data, message)
	if err != nil {
		return fmt.Errorf("failed to decode proto message: %v", err)
	}
	return nil
}