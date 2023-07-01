package libgm

import (
	"encoding/base64"

	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
)

func (c *Client) handleEventOpCode(response *Response) {
	c.Logger.Debug().Any("res", response).Msg("got event response?")
	eventData := &binary.Event{}
	data, decryptedErr := c.cryptor.Decrypt(response.Data.EncryptedData)
	if decryptedErr != nil {
		panic(decryptedErr)
	}
	c.Logger.Debug().Str("protobuf_data", base64.StdEncoding.EncodeToString(data)).Msg("decrypted data")
	err := proto.Unmarshal(data, eventData)
	if err != nil {
		panic(err)
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
