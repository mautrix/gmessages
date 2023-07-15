package events

import (
	"fmt"
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

type HTTPError struct {
	Action string
	Resp   *http.Response
}

func (he HTTPError) Error() string {
	return fmt.Sprintf("http %d while %s", he.Resp.StatusCode, he.Action)
}

type ListenFatalError struct {
	Error error
}

type ListenTemporaryError struct {
	Error error
}

type ListenRecovered struct{}
