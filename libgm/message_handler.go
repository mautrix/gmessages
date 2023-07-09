package libgm

import (
	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/pblite"
)

func (c *Client) handleMessageEvent(res *pblite.Response, data *binary.Message) {
	c.triggerEvent(data)
}
