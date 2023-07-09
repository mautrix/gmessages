package libgm

import (
	"log"

	"go.mau.fi/mautrix-gmessages/libgm/pblite"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/events"
)

func (c *Client) handleUserAlertEvent(res *pblite.Response, data *binary.UserAlertEvent) {
	alertType := data.AlertType
	switch alertType {
	case binary.AlertType_BROWSER_ACTIVE:
		newSessionId := res.Data.RequestId
		c.Logger.Info().Any("sessionId", newSessionId).Msg("[NEW_BROWSER_ACTIVE] Opened new browser connection")
		if newSessionId != c.sessionHandler.sessionId {
			evt := events.NewBrowserActive(newSessionId)
			c.triggerEvent(evt)
		} else {
			c.Logger.Info().Any("sessionId", newSessionId).Msg("Client is ready!")
			conversations, convErr := c.Conversations.List(25)
			if convErr != nil {
				log.Fatal(convErr)
			}
			c.Logger.Debug().Any("conversations", conversations).Msg("got conversations")
			notifyErr := c.Session.NotifyDittoActivity()
			if notifyErr != nil {
				log.Fatal(notifyErr)
			}
			readyEvt := events.NewClientReady(newSessionId, conversations)
			c.triggerEvent(readyEvt)
		}

	case binary.AlertType_MOBILE_BATTERY_LOW:
		c.Logger.Info().Msg("[MOBILE_BATTERY_LOW] Mobile device is on low battery")
		evt := events.NewMobileBatteryLow()
		c.triggerEvent(evt)

	case binary.AlertType_MOBILE_BATTERY_RESTORED:
		c.Logger.Info().Msg("[MOBILE_BATTERY_RESTORED] Mobile device has restored enough battery!")
		evt := events.NewMobileBatteryRestored()
		c.triggerEvent(evt)

	case binary.AlertType_MOBILE_DATA_CONNECTION:
		c.Logger.Info().Msg("[MOBILE_DATA_CONNECTION] Mobile device is now using data connection")
		evt := events.NewMobileDataConnection()
		c.triggerEvent(evt)

	case binary.AlertType_MOBILE_WIFI_CONNECTION:
		c.Logger.Info().Msg("[MOBILE_WIFI_CONNECTION] Mobile device is now using wifi connection")
		evt := events.NewMobileWifiConnection()
		c.triggerEvent(evt)

	default:
		c.Logger.Info().Any("data", data).Any("res", res).Msg("Got unknown alert type")
	}
}
