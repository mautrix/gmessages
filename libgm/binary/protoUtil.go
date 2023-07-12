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

func (et EmojiType) Unicode() string {
	switch et {
	case EmojiType_LIKE:
		return "👍"
	case EmojiType_LOVE:
		return "😍"
	case EmojiType_LAUGH:
		return "😂"
	case EmojiType_SURPRISED:
		return "😮"
	case EmojiType_SAD:
		return "😥"
	case EmojiType_ANGRY:
		return "😠"
	case EmojiType_DISLIKE:
		return "👎"
	case EmojiType_QUESTIONING:
		return "🤔"
	case EmojiType_CRYING_FACE:
		return "😢"
	case EmojiType_POUTING_FACE:
		return "😡"
	case EmojiType_RED_HEART:
		return "❤️"
	default:
		return ""
	}
}

func UnicodeToEmojiType(emoji string) EmojiType {
	switch emoji {
	case "👍":
		return EmojiType_LIKE
	case "😍":
		return EmojiType_LOVE
	case "😂":
		return EmojiType_LAUGH
	case "😮":
		return EmojiType_SURPRISED
	case "😥":
		return EmojiType_SAD
	case "😠":
		return EmojiType_ANGRY
	case "👎":
		return EmojiType_DISLIKE
	case "🤔":
		return EmojiType_QUESTIONING
	case "😢":
		return EmojiType_CRYING_FACE
	case "😡":
		return EmojiType_POUTING_FACE
	case "❤", "❤️":
		return EmojiType_RED_HEART
	default:
		return EmojiType_CUSTOM
	}
}
