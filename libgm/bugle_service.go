package libgm

import "go.mau.fi/mautrix-gmessages/libgm/binary"

func (c *Client) handleBugleOpCode(bugleData *binary.BugleBackendService) {
	switch bugleData.Data.Type {
	case 2:
		c.Logger.Info().Any("type", bugleData.Data.Type).Msg("Updated sessionId to " + c.sessionHandler.sessionId + " due to BROWSER_ACTIVE alert")
	case 6:
		c.Logger.Info().Any("type", bugleData.Data.Type).Msg("USER_ALERT:BATTERY") // tf ?
	}
}
