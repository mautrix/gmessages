package crypto

import (
	"encoding/base64"

	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
)

var SequenceOne = []int{1, 2, 840, 10045, 2, 1}
var SequenceTwo = []int{1, 2, 840, 10045, 3, 1, 7}

func EncodeValues(a *[]byte, b []int) {
	*a = append(*a, 6)
	idx := len(*a)
	*a = append(*a, 0)
	*a = append(*a, byte(40*b[0]+b[1]))
	for i := 2; i < len(b); i++ {
		d := b[i]
		e := make([]byte, 0)
		if d > 128 {
			e = append(e, byte(d/128)+128)
		}
		e = append(e, byte(d%128))
		*a = append(*a, e...)
	}
	(*a)[idx] = byte(len(*a) - idx - 1)
}

func AppendBytes(a []byte, b []byte) []byte {
	newA := make([]byte, len(a))
	copy(newA, a)

	newA = HelperAppendBytes(newA, 48)
	newA = HelperAppendBytes(newA, byte(len(b)))
	for _, value := range b {
		newA = HelperAppendBytes(newA, value)
	}
	return newA
}

func HelperAppendBytes(a []byte, b byte) []byte {
	return append(a, b)
}

func AppendByteSequence(byteArr1 []byte, byteArr2 []byte, uncompressedPublicKey []byte) []byte {
	copiedByteArray := AppendBytes(byteArr1, byteArr2)
	copiedByteArray = HelperAppendBytes(copiedByteArray, 3)
	copiedByteArray = HelperAppendBytes(copiedByteArray, uint8(len(uncompressedPublicKey)+1))
	copiedByteArray = HelperAppendBytes(copiedByteArray, 0)
	return copiedByteArray
}

func EncodeProtoB64(message proto.Message) (string, error) {
	protoBytes, protoErr := binary.EncodeProtoMessage(message)
	if protoErr != nil {
		return "", protoErr
	}
	encodedStr := base64.StdEncoding.EncodeToString(protoBytes)
	return encodedStr, nil
}
