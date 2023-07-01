package events

import (
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
