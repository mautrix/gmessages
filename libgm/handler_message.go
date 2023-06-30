package libgm

import (
	"go.mau.fi/mautrix-gmessages/libgm/binary"
)

func (c *Client) handleMessageEvent(response *Response, evtData *binary.Event_MessageEvent) {
	c.triggerEvent(evtData)
}
