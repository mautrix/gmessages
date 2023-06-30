package events

import "go.mau.fi/mautrix-gmessages/libgm/cache"

type ConversationEvent interface {
	GetConversation() *cache.Conversation
}

// Triggered when tabbing out of a conversation
type CONVERSATION_EXIT struct {
	Conversation *cache.Conversation
}

func (c *CONVERSATION_EXIT) GetConversation() *cache.Conversation {
	return c.Conversation
}

// Triggered when a conversation is archived
type CONVERSATION_ARCHIVED struct {
	Conversation *cache.Conversation
}

func (c *CONVERSATION_ARCHIVED) GetConversation() *cache.Conversation {
	return c.Conversation
}

// Triggered when a conversation is unarchived
type CONVERSATION_UNARCHIVED struct {
	Conversation *cache.Conversation
}

func (c *CONVERSATION_UNARCHIVED) GetConversation() *cache.Conversation {
	return c.Conversation
}

// Triggered when a conversation is deleted
type CONVERSATION_DELETED struct {
	Conversation *cache.Conversation
}

func (c *CONVERSATION_DELETED) GetConversation() *cache.Conversation {
	return c.Conversation
}

func NewConversationExit(conversation *cache.Conversation) ConversationEvent {
	return &CONVERSATION_EXIT{
		Conversation: conversation,
	}
}

func NewConversationArchived(conversation *cache.Conversation) ConversationEvent {
	return &CONVERSATION_ARCHIVED{
		Conversation: conversation,
	}
}

func NewConversationUnarchived(conversation *cache.Conversation) ConversationEvent {
	return &CONVERSATION_UNARCHIVED{
		Conversation: conversation,
	}
}

func NewConversationDeleted(conversation *cache.Conversation) ConversationEvent {
	return &CONVERSATION_DELETED{
		Conversation: conversation,
	}
}
