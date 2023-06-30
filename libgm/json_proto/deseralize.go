package json_proto

import (
	"fmt"

	"google.golang.org/protobuf/reflect/protoreflect"
)

func Deserialize(data []interface{}, m protoreflect.Message) error {
	for i := 0; i < m.Descriptor().Fields().Len(); i++ {
		fieldDescriptor := m.Descriptor().Fields().Get(i)
		index := int(fieldDescriptor.Number()) - 1
		if index < 0 || index >= len(data) || data[index] == nil {
			continue
		}

		val := data[index]

		switch fieldDescriptor.Kind() {
		case protoreflect.MessageKind:
			nestedData, ok := val.([]interface{})
			if !ok {
				return fmt.Errorf("expected slice, got %T", val)
			}
			nestedMessage := m.NewField(fieldDescriptor).Message()
			if err := Deserialize(nestedData, nestedMessage); err != nil {
				return err
			}
			m.Set(fieldDescriptor, protoreflect.ValueOfMessage(nestedMessage))
		case protoreflect.BytesKind:
			bytes, ok := val.([]byte)
			if !ok {
				return fmt.Errorf("expected bytes, got %T", val)
			}
			m.Set(fieldDescriptor, protoreflect.ValueOfBytes(bytes))
		case protoreflect.Int32Kind, protoreflect.Int64Kind:
			num, ok := val.(float64)
			if !ok {
				return fmt.Errorf("expected number, got %T", val)
			}
			m.Set(fieldDescriptor, protoreflect.ValueOf(int64(num)))
		case protoreflect.StringKind:
			str, ok := val.(string)
			if !ok {
				return fmt.Errorf("expected string, got %T", val)
			}
			m.Set(fieldDescriptor, protoreflect.ValueOf(str))
		default:
			// ignore fields of other types
		}
	}
	return nil
}
