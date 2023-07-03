package events

import (
	"net/http"

	"go.mau.fi/mautrix-gmessages/libgm/util"
)

type ClientReady struct {
	Session *util.SessionResponse
}

func NewClientReady(session *util.SessionResponse) *ClientReady {
	return &ClientReady{
		Session: session,
	}
}

type ListenFatalError struct {
	Resp *http.Response
}

type ListenTemporaryError struct {
	Error error
}

type ListenRecovered struct{}
