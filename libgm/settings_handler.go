package libgm

import (
	"go.mau.fi/mautrix-gmessages/libgm/pblite"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/events"
)

func (c *Client) handleSettingsEvent(res *pblite.Response, data *binary.Settings) {
	evt := events.NewSettingsUpdated(data)
	c.triggerEvent(evt)
}
