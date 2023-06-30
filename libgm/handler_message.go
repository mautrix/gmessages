package textgapi

import (
	"log"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/cache"

	//"github.com/0xzer/textgapi/cache"
	"go.mau.fi/mautrix-gmessages/libgm/events"
)

func (c *Client) handleMessageEvent(response *Response, evtData *binary.Event_MessageEvent) {
	msgData := evtData.MessageEvent.Data
	currConv, convNotFound := c.cache.Conversations.GetConversation(msgData.ConversationId)
	if convNotFound != nil {
		log.Fatal(convNotFound)
	}
	lastCacheMsg, errGetMsg := currConv.GetMessage(msgData.MessageId)

	updatedCacheMsg := currConv.UpdateMessage(msgData)
	eventData := c.getMessageEventInterface(currConv, lastCacheMsg, errGetMsg, updatedCacheMsg)
	if eventData == nil {
		return
	}
	c.triggerEvent(eventData)
}

func (c *Client) getMessageEventInterface(currConv *cache.Conversation, lastCacheMsg cache.Message, lastCacheErr error, evtMsg cache.Message) events.MessageEvent {
	var evt events.MessageEvent

	msgStatusCode := evtMsg.MessageStatus.Code
	fromMe := evtMsg.FromMe()

	switch msgStatusCode {
	case 5: // sending
		if lastCacheErr != nil {
			if fromMe {
				evt = events.NewMessageSending(evtMsg)
			} else {
				evt = events.NewMessageReceiving(evtMsg)
			}
		}
	case 1: // sent
		if lastCacheMsg.MessageStatus.Code != 1 {
			if fromMe {
				evt = events.NewMessageSent(evtMsg)
			} else {
				evt = events.NewMessageReceived(evtMsg)
			}
		}
	default:
		c.Logger.Debug().Any("data", evtMsg).Msg("Unknown msgstatus code")
	}

	return evt
}
