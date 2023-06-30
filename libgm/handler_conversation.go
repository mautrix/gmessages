package textgapi

import (
	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/cache"
	"go.mau.fi/mautrix-gmessages/libgm/events"
)

func (c *Client) handleConversationEvent(response *Response, evtData *binary.Event_ConversationEvent) {
	lastCacheConv, notExists := c.cache.Conversations.GetConversation(evtData.ConversationEvent.Data.ConversationId)
	evtConv := evtData.ConversationEvent.Data

	//c.Logger.Debug().Any("convData", evtConv).Msg("Got conversation event!")
	var eventData events.ConversationEvent
	if evtConv.Status == 3 {
		lastCacheConv.Delete()
		eventData = events.NewConversationDeleted(lastCacheConv)
		c.triggerEvent(eventData)
		return
	}
	updatedCacheConv := c.cache.Conversations.UpdateConversation(evtConv)
	eventData = c.getConversationEventInterface(lastCacheConv, updatedCacheConv, notExists)
	if eventData == nil {
		return
	}
	c.triggerEvent(eventData)
}

func (c *Client) getConversationEventInterface(lastCacheConv *cache.Conversation, updatedCacheConv *cache.Conversation, notExists error) events.ConversationEvent {
	var evt events.ConversationEvent
	convStatus := updatedCacheConv.Status

	switch convStatus {
	case 1: // unarchived
		if lastCacheConv.Status != 1 {
			evt = events.NewConversationUnarchived(updatedCacheConv)
		}
	case 2: // archived
		evt = events.NewConversationArchived(updatedCacheConv)
	case 3: // deleted
		evt = events.NewConversationDeleted(updatedCacheConv)
	}
	return evt
}
