package pblite

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"

	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
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

func isPbliteBinary(descriptor protoreflect.FieldDescriptor) bool {
	opts := descriptor.Options().(*descriptorpb.FieldOptions)
	pbliteBinary, ok := proto.GetExtension(opts, gmproto.E_PbliteBinary).(bool)
	return ok && pbliteBinary
}

func deserializeOne(val any, index int, ref protoreflect.Message, insideList protoreflect.List, fieldDescriptor protoreflect.FieldDescriptor) (protoreflect.Value, error) {
	var num float64
	var expectedKind, str string
	var boolean, ok bool
	var outputVal protoreflect.Value
	if fieldDescriptor.IsList() && insideList == nil {
		nestedData, ok := val.([]any)
		if !ok {
			return outputVal, fmt.Errorf("expected untyped array at index %d for repeated field %s, got %T", index, fieldDescriptor.FullName(), val)
		}
		list := ref.NewField(fieldDescriptor).List()
		list.NewElement()
		for i, nestedVal := range nestedData {
			nestedParsed, err := deserializeOne(nestedVal, i, ref, list, fieldDescriptor)
			if err != nil {
				return outputVal, err
			}
			list.Append(nestedParsed)
		}
		return protoreflect.ValueOfList(list), nil
	}
	switch fieldDescriptor.Kind() {
	case protoreflect.MessageKind:
		ok = true
		var nestedMessage protoreflect.Message
		if insideList != nil {
			nestedMessage = insideList.NewElement().Message()
		} else {
			nestedMessage = ref.NewField(fieldDescriptor).Message()
		}
		if isPbliteBinary(fieldDescriptor) {
			bytesBase64, ok := val.(string)
			if !ok {
				return outputVal, fmt.Errorf("expected string at index %d for field %s, got %T", index, fieldDescriptor.FullName(), val)
			}
			bytes, err := base64.StdEncoding.DecodeString(bytesBase64)
			if err != nil {
				return outputVal, fmt.Errorf("failed to decode base64 at index %d for field %s: %w", index, fieldDescriptor.FullName(), err)
			}
			err = proto.Unmarshal(bytes, nestedMessage.Interface())
			if err != nil {
				return outputVal, fmt.Errorf("failed to unmarshal binary protobuf at index %d for field %s: %w", index, fieldDescriptor.FullName(), err)
			}
		} else {
			nestedData, ok := val.([]any)
			if !ok {
				return outputVal, fmt.Errorf("expected untyped array at index %d for field %s, got %T", index, fieldDescriptor.FullName(), val)
			}
			if err := deserializeFromSlice(nestedData, nestedMessage); err != nil {
				return outputVal, err
			}
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
		if str, ok = val.(string); ok {
			parsedVal, err := strconv.ParseInt(str, 10, 32)
			if err != nil {
				return outputVal, fmt.Errorf("failed to parse int32 at index %d for field %s: %w", index, fieldDescriptor.FullName(), err)
			}
			outputVal = protoreflect.ValueOfInt32(int32(parsedVal))
		} else {
			num, ok = val.(float64)
			expectedKind = "float64"
			outputVal = protoreflect.ValueOfInt32(int32(num))
		}
	case protoreflect.Int64Kind:
		if str, ok = val.(string); ok {
			parsedVal, err := strconv.ParseInt(str, 10, 64)
			if err != nil {
				return outputVal, fmt.Errorf("failed to parse int64 at index %d for field %s: %w", index, fieldDescriptor.FullName(), err)
			}
			outputVal = protoreflect.ValueOfInt64(parsedVal)
		} else {
			num, ok = val.(float64)
			expectedKind = "float64"
			outputVal = protoreflect.ValueOfInt64(int64(num))
		}
	case protoreflect.Uint32Kind:
		if str, ok = val.(string); ok {
			parsedVal, err := strconv.ParseUint(str, 10, 32)
			if err != nil {
				return outputVal, fmt.Errorf("failed to parse uint32 at index %d for field %s: %w", index, fieldDescriptor.FullName(), err)
			}
			outputVal = protoreflect.ValueOfUint32(uint32(parsedVal))
		} else {
			num, ok = val.(float64)
			expectedKind = "float64"
			outputVal = protoreflect.ValueOfUint32(uint32(num))
		}
	case protoreflect.Uint64Kind:
		if str, ok = val.(string); ok {
			parsedVal, err := strconv.ParseUint(str, 10, 64)
			if err != nil {
				return outputVal, fmt.Errorf("failed to parse uint64 at index %d for field %s: %w", index, fieldDescriptor.FullName(), err)
			}
			outputVal = protoreflect.ValueOfUint64(parsedVal)
		} else {
			num, ok = val.(float64)
			expectedKind = "float64"
			outputVal = protoreflect.ValueOfUint64(uint64(num))
		}
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
		if ok && isPbliteBinary(fieldDescriptor) {
			bytes, err := base64.StdEncoding.DecodeString(str)
			if err != nil {
				return outputVal, fmt.Errorf("failed to decode base64 at index %d for field %s: %w", index, fieldDescriptor.FullName(), err)
			}
			str = string(bytes)
		}
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
		outputVal, err := deserializeOne(val, index, ref, nil, fieldDescriptor)
		if err != nil {
			return err
		}
		ref.Set(fieldDescriptor, outputVal)
	}
	return nil
}
