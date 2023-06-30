package textgapi

import (
	"go.mau.fi/mautrix-gmessages/libgm/binary"
)

func (c *Client) handleConversationEvent(response *Response, evtData *binary.Event_ConversationEvent) {
	c.triggerEvent(evtData)
}
