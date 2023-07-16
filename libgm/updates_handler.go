package libgm

import (
	"go.mau.fi/mautrix-gmessages/libgm/events"
	"go.mau.fi/mautrix-gmessages/libgm/pblite"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
)

func (c *Client) handleUpdatesEvent(res *pblite.Response) {
	switch res.Data.Action {
	case binary.ActionType_GET_UPDATES:
		data, ok := res.Data.Decrypted.(*binary.UpdateEvents)
		if !ok {
			c.Logger.Error().Any("res", res).Msg("failed to assert ActionType_GET_UPDATES event into UpdateEvents")
			return
		}

		switch evt := data.Event.(type) {
		case *binary.UpdateEvents_UserAlertEvent:
			c.rpc.logContent(res)
			c.handleUserAlertEvent(res, evt.UserAlertEvent)

		case *binary.UpdateEvents_SettingsEvent:
			c.rpc.logContent(res)
			c.triggerEvent(evt.SettingsEvent)

		case *binary.UpdateEvents_ConversationEvent:
			if c.rpc.deduplicateUpdate(res) {
				return
			}
			c.triggerEvent(evt.ConversationEvent.GetData())

		case *binary.UpdateEvents_MessageEvent:
			if c.rpc.deduplicateUpdate(res) {
				return
			}
			c.triggerEvent(evt.MessageEvent.GetData())

		case *binary.UpdateEvents_TypingEvent:
			c.rpc.logContent(res)
			c.triggerEvent(evt.TypingEvent.GetData())
		default:
			c.Logger.Debug().Any("evt", evt).Any("res", res).Msg("Got unknown event type")
		}

	default:
		c.Logger.Error().Any("response", res).Msg("ignoring response.")
	}
}

func (c *Client) handleClientReady(newSessionId string) {
	c.Logger.Info().Any("sessionId", newSessionId).Msg("Client is ready!")
	conversations, convErr := c.ListConversations(25, binary.ListConversationsPayload_INBOX)
	if convErr != nil {
		panic(convErr)
	}
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
		newSessionId := res.Data.RequestID
		c.Logger.Info().Any("sessionId", newSessionId).Msg("[NEW_BROWSER_ACTIVE] Opened new browser connection")
		if newSessionId != c.sessionHandler.sessionID {
			evt := events.NewBrowserActive(newSessionId)
			c.triggerEvent(evt)
		} else {
			go c.handleClientReady(newSessionId)
		}
	default:
		c.triggerEvent(data)
	}
}
