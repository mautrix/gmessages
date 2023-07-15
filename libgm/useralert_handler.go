package libgm

import (
	"go.mau.fi/mautrix-gmessages/libgm/pblite"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/events"
)

func (c *Client) handleClientReady(newSessionId string) {
	c.Logger.Info().Any("sessionId", newSessionId).Msg("Client is ready!")
	conversations, convErr := c.ListConversations(25)
	if convErr != nil {
		panic(convErr)
	}
	c.Logger.Debug().Any("conversations", conversations).Msg("got conversations")
	notifyErr := c.NotifyDittoActivity()
	if notifyErr != nil {
		panic(notifyErr)
	}
	readyEvt := events.NewClientReady(newSessionId, conversations)
	c.triggerEvent(readyEvt)
}

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
			go c.handleClientReady(newSessionId)
		}
	default:
		c.triggerEvent(data)
	}
}
