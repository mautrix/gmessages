package binary

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

func MakeReactionData(emoji string) *ReactionData {
	return &ReactionData{
		Unicode: emoji,
		Type:    UnicodeToEmojiType(emoji),
	}
}
