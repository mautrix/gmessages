package pblite

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func Unmarshal(data []byte, m proto.Message) error {
	var anyData any
	if err := json.Unmarshal(data, &anyData); err != nil {
		return err
	}
	anyDataArr, ok := anyData.([]any)
	if !ok {
		return fmt.Errorf("expected array in JSON, got %T", anyData)
	}
	return deserializeFromSlice(anyDataArr, m.ProtoReflect())
}

func deserializeOne(val any, index int, ref protoreflect.Message, fieldDescriptor protoreflect.FieldDescriptor) (protoreflect.Value, error) {
	var num float64
	var expectedKind, str string
	var boolean, ok bool
	var outputVal protoreflect.Value
	switch fieldDescriptor.Kind() {
	case protoreflect.MessageKind:
		ok = true
		nestedData, ok := val.([]any)
		if !ok {
			return outputVal, fmt.Errorf("expected untyped array at index %d for field %s, got %T", index, fieldDescriptor.FullName(), val)
		}
		nestedMessage := ref.NewField(fieldDescriptor).Message()
		if err := deserializeFromSlice(nestedData, nestedMessage); err != nil {
			return outputVal, err
		}
		outputVal = protoreflect.ValueOfMessage(nestedMessage)
	case protoreflect.BytesKind:
		ok = true
		bytesBase64, ok := val.(string)
		if !ok {
			return outputVal, fmt.Errorf("expected string at index %d for field %s, got %T", index, fieldDescriptor.FullName(), val)
		}
		bytes, err := base64.StdEncoding.DecodeString(bytesBase64)
		if err != nil {
			return outputVal, fmt.Errorf("failed to decode base64 at index %d for field %s: %w", index, fieldDescriptor.FullName(), err)
		}

		outputVal = protoreflect.ValueOfBytes(bytes)
	case protoreflect.EnumKind:
		num, ok = val.(float64)
		expectedKind = "float64"
		outputVal = protoreflect.ValueOfEnum(protoreflect.EnumNumber(int32(num)))
	case protoreflect.Int32Kind:
		num, ok = val.(float64)
		expectedKind = "float64"
		outputVal = protoreflect.ValueOfInt32(int32(num))
	case protoreflect.Int64Kind:
		num, ok = val.(float64)
		expectedKind = "float64"
		outputVal = protoreflect.ValueOfInt64(int64(num))
	case protoreflect.Uint32Kind:
		num, ok = val.(float64)
		expectedKind = "float64"
		outputVal = protoreflect.ValueOfUint32(uint32(num))
	case protoreflect.Uint64Kind:
		num, ok = val.(float64)
		expectedKind = "float64"
		outputVal = protoreflect.ValueOfUint64(uint64(num))
	case protoreflect.FloatKind:
		num, ok = val.(float64)
		expectedKind = "float64"
		outputVal = protoreflect.ValueOfFloat32(float32(num))
	case protoreflect.DoubleKind:
		num, ok = val.(float64)
		expectedKind = "float64"
		outputVal = protoreflect.ValueOfFloat64(num)
	case protoreflect.StringKind:
		str, ok = val.(string)
		expectedKind = "string"
		outputVal = protoreflect.ValueOfString(str)
	case protoreflect.BoolKind:
		boolean, ok = val.(bool)
		expectedKind = "bool"
		outputVal = protoreflect.ValueOfBool(boolean)
	default:
		return outputVal, fmt.Errorf("unsupported field type %s in %s", fieldDescriptor.Kind(), fieldDescriptor.FullName())
	}
	if !ok {
		return outputVal, fmt.Errorf("expected %s at index %d for field %s, got %T", expectedKind, index, fieldDescriptor.FullName(), val)
	}
	return outputVal, nil
}

func deserializeFromSlice(data []any, ref protoreflect.Message) error {
	for i := 0; i < ref.Descriptor().Fields().Len(); i++ {
		fieldDescriptor := ref.Descriptor().Fields().Get(i)
		index := int(fieldDescriptor.Number()) - 1
		if index < 0 || index >= len(data) || data[index] == nil {
			continue
		}

		val := data[index]
		outputVal, err := deserializeOne(val, index, ref, fieldDescriptor)
		if err != nil {
			return err
		}
		ref.Set(fieldDescriptor, outputVal)
	}
	return nil
}
