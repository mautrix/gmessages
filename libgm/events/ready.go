package events

import (
	"fmt"
	"net/http"

	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
)

type ClientReady struct {
	SessionID     string
	Conversations []*gmproto.Conversation
}

func NewClientReady(sessionID string, conversationList *gmproto.Conversations) *ClientReady {
	return &ClientReady{
		SessionID:     sessionID,
		Conversations: conversationList.Conversations,
	}
}

type AuthTokenRefreshed struct{}

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
