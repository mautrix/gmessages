package libgm

import (
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
			c.handleSettingsEvent(res, evt.SettingsEvent)

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
			c.handleTypingEvent(res, evt.TypingEvent.GetData())
		default:
			c.Logger.Debug().Any("evt", evt).Any("res", res).Msg("Got unknown event type")
		}

	default:
		c.Logger.Error().Any("response", res).Msg("ignoring response.")
	}
}
