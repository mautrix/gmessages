package events

import (
	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

type CLIENT_READY struct {
	Session       *util.SessionResponse
	Conversations []*binary.Conversation
}

func NewClientReady(session *util.SessionResponse, conversationList *binary.Conversations) *CLIENT_READY {
	return &CLIENT_READY{
		Session:       session,
		Conversations: conversationList.Conversations,
	}
}
