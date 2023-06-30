package pblite

/*
in protobuf, a message looks like this:

message SomeMessage {
	string stringField1 = 1;
	int64 intField = 6;
	bytes byteField = 9;
}

but when this function is done serializing this protobuf message into a slice, it should look something like this:

[
	"someString",
	nil,
	nil,
	nil,
	nil,
	6,
	nil,
	nil,
	"\x9\x91\x942"
]

Any integer should be translated into int64, it doesn't matter if it's defined as int32 in the proto schema.
In the finished serialized slice it should be int64.
Let's also take in count where there is a message nested inside a message:
message SomeMessage {
	string stringField1 = 1;
	NestedMessage1 nestedMessage1 = 2;
	int64 intField = 6;
	bytes byteField = 9;
}

message NestedMessage1 {
	string msg1 = 1;
}

Then the serialized output would be:
[
	"someString",
	["msg1FieldValue"],
	nil,
	nil,
	nil,
	6,
	nil,
	nil,
	"\x9\x91\x942"
]
This means that any slice inside of the current slice, indicates another message nested inside of it.
*/

import (
	"encoding/base64"
	"fmt"

	"google.golang.org/protobuf/reflect/protoreflect"
)

func Serialize(m protoreflect.Message) ([]any, error) {
	maxFieldNumber := 0
	for i := 0; i < m.Descriptor().Fields().Len(); i++ {
		fieldNumber := int(m.Descriptor().Fields().Get(i).Number())
		if fieldNumber > maxFieldNumber {
			maxFieldNumber = fieldNumber
		}
	}

	serialized := make([]any, maxFieldNumber)
	for i := 0; i < m.Descriptor().Fields().Len(); i++ {
		fieldDescriptor := m.Descriptor().Fields().Get(i)
		fieldValue := m.Get(fieldDescriptor)
		fieldNumber := int(fieldDescriptor.Number())
		if !m.Has(fieldDescriptor) {
			continue
		}
		switch fieldDescriptor.Kind() {
		case protoreflect.MessageKind:
			serializedMsg, err := Serialize(fieldValue.Message().Interface().ProtoReflect())
			if err != nil {
				return nil, err
			}
			serialized[fieldNumber-1] = serializedMsg
		case protoreflect.BytesKind:
			serialized[fieldNumber-1] = base64.StdEncoding.EncodeToString(fieldValue.Bytes())
		case protoreflect.Int32Kind, protoreflect.Int64Kind:
			serialized[fieldNumber-1] = fieldValue.Int()
		case protoreflect.Uint32Kind, protoreflect.Uint64Kind:
			serialized[fieldNumber-1] = fieldValue.Uint()
		case protoreflect.FloatKind, protoreflect.DoubleKind:
			serialized[fieldNumber-1] = fieldValue.Float()
		case protoreflect.EnumKind:
			serialized[fieldNumber-1] = int(fieldValue.Enum())
		case protoreflect.BoolKind:
			serialized[fieldNumber-1] = fieldValue.Bool()
		case protoreflect.StringKind:
			serialized[fieldNumber-1] = fieldValue.String()
		default:
			return nil, fmt.Errorf("unsupported field type %s in %s", fieldDescriptor.Kind(), fieldDescriptor.FullName())
		}
	}

	return serialized, nil
}
