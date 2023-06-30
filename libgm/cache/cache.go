package cache

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
)

type Cache struct {
	Conversations Conversations `json:"conversations"`
	Settings      Settings      `json:"sim,omitempty"`
}

func LoadCache(path string) Cache {
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		log.Fatal(readErr)
	}
	var cache Cache
	err := json.Unmarshal(data, &cache)
	if err != nil {
		log.Fatal(err)
	}
	return cache
}

func (c *Cache) OrderMapToInterface() map[string]interface{} {
	convIdMapStringInterface := make(map[string]interface{})
	for key, value := range c.Conversations.Order {
		convIdMapStringInterface[strconv.Itoa(key)] = value
	}
	return convIdMapStringInterface
}

func (c *Cache) SetSettings(settings *binary.Settings) {
	c.Settings = Settings{
		CarrierName: settings.Data.SimData.CarrierName,
		HexHash:     settings.Data.SimData.HexHash,
		Version:     settings.Version,
	}
}

func (c *Cache) SetMessages(messages *binary.FetchMessagesResponse) {
	for _, msg := range messages.Messages {
		convo, ok := c.Conversations.Conversations[msg.ConversationId]
		if !ok {
			// handle error, such as creating a new conversation or returning
			fmt.Printf("Could not find conversation with id %s", msg.ConversationId)
			return
		} else {
			convo.UpdateMessage(msg)
		}
	}
}

func (c *Cache) SetConversations(conversations *binary.Conversations) {
	for order, conv := range conversations.Conversations {
		convertedConv := NewConversation(c, conv)
		c.Conversations.Order[order] = conv.ConversationId
		c.Conversations.Conversations[conv.ConversationId] = convertedConv
	}
}
