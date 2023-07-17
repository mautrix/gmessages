package libgm

import (
	"fmt"

	"go.mau.fi/mautrix-gmessages/libgm/events"
	"go.mau.fi/mautrix-gmessages/libgm/pblite"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
)

func (c *Client) handlePairingEvent(response *pblite.Response) {
	pairEventData, ok := response.Data.Decrypted.(*binary.PairEvents)
	if !ok {
		c.Logger.Error().Type("decrypted_type", response.Data.Decrypted).Msg("Unexpected data type in pair event")
		return
	}

	switch evt := pairEventData.Event.(type) {
	case *binary.PairEvents_Paired:
		c.completePairing(evt.Paired)
	case *binary.PairEvents_Revoked:
		c.triggerEvent(evt.Revoked)
	default:
		c.Logger.Debug().Any("evt", evt).Msg("Unknown pair event type")
	}
}

func (c *Client) completePairing(data *binary.PairedData) {
	c.updateTachyonAuthToken(data.GetTokenData().GetTachyonAuthToken(), data.GetTokenData().GetTTL())
	c.AuthData.Mobile = data.Mobile
	c.AuthData.Browser = data.Browser

	c.triggerEvent(&events.PairSuccessful{PairedData: data})

	err := c.Reconnect()
	if err != nil {
		c.triggerEvent(&events.ListenFatalError{Error: fmt.Errorf("failed to reconnect after pair success: %w", err)})
	}
}
