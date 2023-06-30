package events

import (
	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

type ClientReady struct {
	Session       *util.SessionResponse
	Conversations []*binary.Conversation
}

func NewClientReady(session *util.SessionResponse, conversationList *binary.Conversations) *ClientReady {
	return &ClientReady{
		Session:       session,
		Conversations: conversationList.Conversations,
	}
}
