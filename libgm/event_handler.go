package libgm

import (
	"go.mau.fi/mautrix-gmessages/libgm/binary"
)

func (c *Client) handleEventOpCode(response *Response) {
	c.Logger.Debug().Any("res", response).Msg("got event response?")
	eventData := &binary.Event{}
	decryptedErr := c.cryptor.DecryptAndDecodeData(response.Data.EncryptedData, eventData)
	if decryptedErr != nil {
		panic(decryptedErr)
	}
	switch evt := eventData.Event.(type) {
	case *binary.Event_MessageEvent:
		c.handleMessageEvent(response, evt)
	case *binary.Event_ConversationEvent:
		c.handleConversationEvent(response, evt)
	case *binary.Event_UserAlertEvent:
		c.handleUserAlertEvent(response, evt)
	default:
		c.Logger.Debug().Any("res", response).Msg("unknown event")
	}
}
