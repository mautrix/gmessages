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
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func serializeOneOrList(fieldDescriptor protoreflect.FieldDescriptor, fieldValue protoreflect.Value) (any, error) {
	switch {
	case fieldDescriptor.IsList():
		var serializedList []any
		list := fieldValue.List()
		for i := 0; i < list.Len(); i++ {
			serialized, err := serializeOne(fieldDescriptor, list.Get(i))
			if err != nil {
				return nil, err
			}
			serializedList = append(serializedList, serialized)
		}
		return serializedList, nil
	default:
		return serializeOne(fieldDescriptor, fieldValue)
	}
}

func serializeOne(fieldDescriptor protoreflect.FieldDescriptor, fieldValue protoreflect.Value) (any, error) {
	switch fieldDescriptor.Kind() {
	case protoreflect.MessageKind:
		if isPbliteBinary(fieldDescriptor) {
			serializedMsg, err := proto.Marshal(fieldValue.Message().Interface())
			if err != nil {
				return nil, err
			}
			return base64.StdEncoding.EncodeToString(serializedMsg), nil
		} else {
			serializedMsg, err := SerializeToSlice(fieldValue.Message().Interface())
			if err != nil {
				return nil, err
			}
			return serializedMsg, nil
		}
	case protoreflect.BytesKind:
		return base64.StdEncoding.EncodeToString(fieldValue.Bytes()), nil
	case protoreflect.Int32Kind, protoreflect.Int64Kind:
		return fieldValue.Int(), nil
	case protoreflect.Uint32Kind, protoreflect.Uint64Kind:
		return fieldValue.Uint(), nil
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return fieldValue.Float(), nil
	case protoreflect.EnumKind:
		return int(fieldValue.Enum()), nil
	case protoreflect.BoolKind:
		return fieldValue.Bool(), nil
	case protoreflect.StringKind:
		if isPbliteBinary(fieldDescriptor) {
			return base64.StdEncoding.EncodeToString([]byte(fieldValue.String())), nil
		} else {
			return fieldValue.String(), nil
		}
	default:
		return nil, fmt.Errorf("unsupported field type %s in %s", fieldDescriptor.Kind(), fieldDescriptor.FullName())
	}
}

func SerializeToSlice(msg proto.Message) ([]any, error) {
	ref := msg.ProtoReflect()
	maxFieldNumber := 0
	for i := 0; i < ref.Descriptor().Fields().Len(); i++ {
		fieldNumber := int(ref.Descriptor().Fields().Get(i).Number())
		if fieldNumber > maxFieldNumber {
			maxFieldNumber = fieldNumber
		}
	}

	serialized := make([]any, maxFieldNumber)
	for i := 0; i < ref.Descriptor().Fields().Len(); i++ {
		fieldDescriptor := ref.Descriptor().Fields().Get(i)
		fieldValue := ref.Get(fieldDescriptor)
		fieldNumber := int(fieldDescriptor.Number())
		if !ref.Has(fieldDescriptor) {
			continue
		}
		serializedVal, err := serializeOneOrList(fieldDescriptor, fieldValue)
		if err != nil {
			return nil, err
		}
		serialized[fieldNumber-1] = serializedVal
	}

	return serialized, nil
}

func Marshal(m proto.Message) ([]byte, error) {
	serialized, err := SerializeToSlice(m)
	if err != nil {
		return nil, err
	}
	return json.Marshal(serialized)
}
