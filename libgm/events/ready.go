package events

import (
	"net/http"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
)

type ClientReady struct {
	SessionId     string
	Conversations []*binary.Conversation
}

func NewClientReady(sessionId string, conversationList *binary.Conversations) *ClientReady {
	return &ClientReady{
		SessionId:     sessionId,
		Conversations: conversationList.Conversations,
	}
}

type AuthTokenRefreshed struct {
	Token []byte
}

func NewAuthTokenRefreshed(token []byte) *AuthTokenRefreshed {
	return &AuthTokenRefreshed{
		Token: token,
	}
}

type ListenFatalError struct {
	Resp *http.Response
}

type ListenTemporaryError struct {
	Error error
}

type ListenRecovered struct{}
