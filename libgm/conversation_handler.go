package libgm

import (
	"go.mau.fi/mautrix-gmessages/libgm/pblite"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
)

func (c *Client) handleConversationEvent(res *pblite.Response, data *binary.Conversation) {
	c.triggerEvent(data)
}
