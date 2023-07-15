package libgm

import (
	"go.mau.fi/mautrix-gmessages/libgm/events"
	"go.mau.fi/mautrix-gmessages/libgm/pblite"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
)

func (c *Client) handlePairingEvent(response *pblite.Response) {
	pairEventData, ok := response.Data.Decrypted.(*binary.PairEvents)

	if !ok {
		c.Logger.Error().Any("pairEventData", pairEventData).Msg("failed to assert response into PairEvents")
		return
	}

	switch evt := pairEventData.Event.(type) {
	case *binary.PairEvents_Paired:
		callbackErr := c.pairCallback(evt.Paired)
		if callbackErr != nil {
			panic(callbackErr)
		}
	case *binary.PairEvents_Revoked:
		c.Logger.Debug().Any("data", evt).Msg("Revoked Device")
		c.triggerEvent(evt.Revoked)
	default:
		c.Logger.Debug().Any("response", response).Any("evt", evt).Msg("Invalid PairEvents type")
	}
}

func (c *Client) NewDevicePair(mobile, browser *binary.Device) *pblite.DevicePair {
	return &pblite.DevicePair{
		Mobile:  mobile,
		Browser: browser,
	}
}

func (c *Client) pairCallback(data *binary.PairedData) error {

	tokenData := data.GetTokenData()
	c.updateTachyonAuthToken(tokenData.GetTachyonAuthToken())
	c.updateTTL(tokenData.GetTTL())

	devicePair := c.NewDevicePair(data.Mobile, data.Browser)
	c.updateDevicePair(devicePair)

	webEncryptionKeyResponse, webErr := c.GetWebEncryptionKey()
	if webErr != nil {
		return webErr
	}
	c.updateWebEncryptionKey(webEncryptionKeyResponse.GetKey())

	c.triggerEvent(&events.PairSuccessful{data})

	reconnectErr := c.Reconnect()
	if reconnectErr != nil {
		return reconnectErr
	}
	return nil
}
