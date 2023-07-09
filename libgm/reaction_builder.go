package libgm

import (
	"fmt"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/metadata"
)

type ReactionBuilderError struct {
	errMsg string
}

func (rbe *ReactionBuilderError) Error() string {
	return fmt.Sprintf("Failed to build reaction builder: %s", rbe.errMsg)
}

type ReactionBuilder struct {
	messageID string
	emoji     []byte
	action    binary.Reaction

	emojiType int64
}

func (rb *ReactionBuilder) SetAction(action binary.Reaction) *ReactionBuilder {
	rb.action = action
	return rb
}

/*
Emoji is a unicode string like "\U0001F44D" or a string like "👍"
*/
func (rb *ReactionBuilder) SetEmoji(emoji string) *ReactionBuilder {
	emojiType, exists := metadata.Emojis[emoji]
	if exists {
		rb.emojiType = emojiType
	} else {
		rb.emojiType = 8
	}

	rb.emoji = []byte(emoji)
	return rb
}

func (rb *ReactionBuilder) SetMessageID(messageId string) *ReactionBuilder {
	rb.messageID = messageId
	return rb
}

func (rb *ReactionBuilder) Build() (*binary.SendReactionPayload, error) {
	if rb.messageID == "" {
		return nil, &ReactionBuilderError{
			errMsg: "messageID can not be empty",
		}
	}

	if rb.action == 0 {
		return nil, &ReactionBuilderError{
			errMsg: "action can not be empty",
		}
	}

	if rb.emojiType == 0 {
		return nil, &ReactionBuilderError{
			errMsg: "failed to set emojiType",
		}
	}

	if rb.emoji == nil {
		return nil, &ReactionBuilderError{
			errMsg: "failed to set emoji",
		}
	}

	return &binary.SendReactionPayload{
		MessageID: rb.messageID,
		ReactionData: &binary.ReactionData{
			EmojiUnicode: rb.emoji,
			EmojiType:    rb.emojiType,
		},
		Action: rb.action,
	}, nil
}

func (c *Client) NewReactionBuilder() *ReactionBuilder {
	return &ReactionBuilder{}
}
