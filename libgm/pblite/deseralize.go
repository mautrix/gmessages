package pblite

import (
	"encoding/base64"
	"fmt"

	"google.golang.org/protobuf/reflect/protoreflect"
)

func Deserialize(data []any, m protoreflect.Message) error {
	for i := 0; i < m.Descriptor().Fields().Len(); i++ {
		fieldDescriptor := m.Descriptor().Fields().Get(i)
		index := int(fieldDescriptor.Number()) - 1
		if index < 0 || index >= len(data) || data[index] == nil {
			continue
		}

		val := data[index]

		var num float64
		var expectedKind, str string
		var boolean, ok bool
		switch fieldDescriptor.Kind() {
		case protoreflect.MessageKind:
			nestedData, ok := val.([]any)
			if !ok {
				return fmt.Errorf("expected untyped array at index %d for field %s, got %T", index, fieldDescriptor.FullName(), val)
			}
			nestedMessage := m.NewField(fieldDescriptor).Message()
			if err := Deserialize(nestedData, nestedMessage); err != nil {
				return err
			}
			m.Set(fieldDescriptor, protoreflect.ValueOfMessage(nestedMessage))
		case protoreflect.BytesKind:
			bytesBase64, ok := val.(string)
			if !ok {
				return fmt.Errorf("expected string at index %d for field %s, got %T", index, fieldDescriptor.FullName(), val)
			}
			bytes, err := base64.StdEncoding.DecodeString(bytesBase64)
			if err != nil {
				return fmt.Errorf("failed to decode base64 at index %d for field %s: %w", index, fieldDescriptor.FullName(), err)
			}

			m.Set(fieldDescriptor, protoreflect.ValueOfBytes(bytes))
		case protoreflect.Int32Kind:
			num, ok = val.(float64)
			expectedKind = "float64"
			m.Set(fieldDescriptor, protoreflect.ValueOfInt32(int32(num)))
		case protoreflect.Int64Kind:
			num, ok = val.(float64)
			expectedKind = "float64"
			m.Set(fieldDescriptor, protoreflect.ValueOfInt64(int64(num)))
		case protoreflect.Uint32Kind:
			num, ok = val.(float64)
			expectedKind = "float64"
			m.Set(fieldDescriptor, protoreflect.ValueOfUint32(uint32(num)))
		case protoreflect.Uint64Kind:
			num, ok = val.(float64)
			expectedKind = "float64"
			m.Set(fieldDescriptor, protoreflect.ValueOfUint64(uint64(num)))
		case protoreflect.FloatKind:
			num, ok = val.(float64)
			expectedKind = "float64"
			m.Set(fieldDescriptor, protoreflect.ValueOfFloat32(float32(num)))
		case protoreflect.DoubleKind:
			num, ok = val.(float64)
			expectedKind = "float64"
			m.Set(fieldDescriptor, protoreflect.ValueOfFloat64(num))
		case protoreflect.StringKind:
			str, ok = val.(string)
			expectedKind = "string"
			m.Set(fieldDescriptor, protoreflect.ValueOfString(str))
		case protoreflect.BoolKind:
			boolean, ok = val.(bool)
			expectedKind = "bool"
			m.Set(fieldDescriptor, protoreflect.ValueOfBool(boolean))
		default:
			return fmt.Errorf("unsupported field type %s in %s", fieldDescriptor.Kind(), fieldDescriptor.FullName())
		}
		if !ok {
			return fmt.Errorf("expected %s at index %d for field %s, got %T", expectedKind, index, fieldDescriptor.FullName(), val)
		}
	}
	return nil
}
