package libgm

import (
	"go.mau.fi/mautrix-gmessages/libgm/pblite"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/events"
)

func (c *Client) handleTypingEvent(res *pblite.Response, data *binary.TypingData) {
	typingType := data.Type

	var evt events.TypingEvent
	switch typingType {
	case binary.TypingTypes_STARTED_TYPING:
		evt = events.NewStartedTyping(data)
	case binary.TypingTypes_STOPPED_TYPING:
		evt = events.NewStoppedTyping(data)
	default:
		c.Logger.Debug().Any("data", data).Msg("got unknown TypingData evt")
	}

	if evt != nil {
		c.triggerEvent(evt)
	}
}
