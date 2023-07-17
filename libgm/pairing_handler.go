package libgm

import (
	"fmt"

	"go.mau.fi/mautrix-gmessages/libgm/events"
	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
)

func (c *Client) handlePairingEvent(msg *IncomingRPCMessage) {
	switch evt := msg.Pair.Event.(type) {
	case *gmproto.RPCPairData_Paired:
		c.completePairing(evt.Paired)
	case *gmproto.RPCPairData_Revoked:
		c.triggerEvent(evt.Revoked)
	default:
		c.Logger.Debug().Any("evt", evt).Msg("Unknown pair event type")
	}
}

func (c *Client) completePairing(data *gmproto.PairedData) {
	c.updateTachyonAuthToken(data.GetTokenData().GetTachyonAuthToken(), data.GetTokenData().GetTTL())
	c.AuthData.Mobile = data.Mobile
	c.AuthData.Browser = data.Browser

	c.triggerEvent(&events.PairSuccessful{PairedData: data})

	err := c.Reconnect()
	if err != nil {
		c.triggerEvent(&events.ListenFatalError{Error: fmt.Errorf("failed to reconnect after pair success: %w", err)})
	}
}
