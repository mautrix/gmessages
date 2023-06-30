package events

import (
	"go.mau.fi/mautrix-gmessages/libgm/cache"
)

type MessageEvent interface {
	GetMessage() cache.Message
}

type MESSAGE_SENDING struct {
	Message cache.Message
}

func (m *MESSAGE_SENDING) GetMessage() cache.Message {
	return m.Message
}

func NewMessageSending(message cache.Message) MessageEvent {
	return &MESSAGE_SENDING{
		Message: message,
	}
}

type MESSAGE_SENT struct {
	Message cache.Message
}

func (m *MESSAGE_SENT) GetMessage() cache.Message {
	return m.Message
}

func NewMessageSent(message cache.Message) MessageEvent {
	return &MESSAGE_SENT{
		Message: message,
	}
}

type MESSAGE_RECEIVING struct {
	Message cache.Message
}

func (m *MESSAGE_RECEIVING) GetMessage() cache.Message {
	return m.Message
}

func NewMessageReceiving(message cache.Message) MessageEvent {
	return &MESSAGE_RECEIVING{
		Message: message,
	}
}

type MESSAGE_RECEIVED struct {
	Message cache.Message
}

func (m *MESSAGE_RECEIVED) GetMessage() cache.Message {
	return m.Message
}

func NewMessageReceived(message cache.Message) MessageEvent {
	return &MESSAGE_RECEIVED{
		Message: message,
	}
}
