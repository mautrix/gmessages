package events

import "go.mau.fi/mautrix-gmessages/libgm/binary"

type TypingEvent interface {
	GetConversation() string
}

type User struct {
	Field1 int64
	Number string
}

type STARTED_TYPING struct {
	ConversationId string
	User           User
}

func (t *STARTED_TYPING) GetConversation() string {
	return t.ConversationId
}

func NewStartedTyping(data *binary.TypingData) TypingEvent {
	return &STARTED_TYPING{
		ConversationId: data.ConversationID,
		User: User{
			Field1: data.User.Field1,
			Number: data.User.Number,
		},
	}
}

type STOPPED_TYPING struct {
	ConversationId string
	User           User
}

func (t *STOPPED_TYPING) GetConversation() string {
	return t.ConversationId
}

func NewStoppedTyping(data *binary.TypingData) TypingEvent {
	return &STOPPED_TYPING{
		ConversationId: data.ConversationID,
		User: User{
			Field1: data.User.Field1,
			Number: data.User.Number,
		},
	}
}
