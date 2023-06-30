package cache

import (
	"fmt"
	"log"
	"sort"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
)

type SmallInfo struct {
	Type          int64  `json:"type,omitempty"`
	Number        string `json:"number,omitempty"`
	ParticipantId string `json:"participantId,omitempty"`
}

type Participant struct {
	SmallInfo   *SmallInfo `json:"smallInfo,omitempty"`
	HexHash     string     `json:"hexHash,omitempty"`
	IsMe        bool       `json:"isMe,omitempty"`
	Bs          int64      `json:"bs,omitempty"`
	DisplayName string     `json:"displayName,omitempty"`
}

type ImagePixels struct {
	Width  int64 `json:"width,omitempty"`
	Height int64 `json:"height,omitempty"`
}

type ImageMessage struct {
	SomeNumber    int64       `json:"someNumber"`
	ImageId       string      `json:"imageId"`
	ImageName     string      `json:"imageName"`
	Size          int64       `json:"size"`
	Pixels        ImagePixels `json:"pixels"`
	ImageBuffer   []byte      `json:"imageBuffer"`
	DecryptionKey []byte      `json:"decryptionKey"`
}

type TextMessage struct {
	Content string `json:"content"`
}

type IsFromMe struct {
	FromMe bool `json:"fromMe"`
}

type MessageData struct {
	OrderInternal string        `json:"orderInternal"`
	TextData      *TextMessage  `json:"textData,omitempty"`
	ImageData     *ImageMessage `json:"imageData,omitempty"`
}

type MessageStatus struct {
	Code    int64  `json:"code,omitempty"`
	ErrMsg  string `json:"errMsg,omitempty"`
	MsgType string `json:"msgType,omitempty"`
}

type Message struct {
	cache *Cache

	MessageId     string        `json:"messageId"`
	From          IsFromMe      `json:"from"`
	MessageStatus MessageStatus `json:"details"`
	Timestamp     int64         `json:"timestamp"`
	ConvId        string        `json:"convId"`
	ParticipantId string        `json:"participantId"`
	MessageData   []MessageData `json:"messageData"`
	MessageType   string        `json:"messageType"`
}

func (m *Message) FromMe() bool {
	conv, _ := m.cache.Conversations.GetConversation(m.ConvId)
	return m.ParticipantId == conv.SelfParticipantId
}

type LatestMessage struct {
	Content     string `json:"content,omitempty"`
	FromMe      bool   `json:"fromMe,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	MessageId   string `json:"messageId,omitempty"`
}

type Conversation struct {
	cache *Cache

	MessageOrder      map[int]string     `json:"messageOrder"`
	Messages          map[string]Message `json:"messages"`
	ConversationId    string             `json:"conversationId"`
	DisplayName       string             `json:"displayName"`
	LatestMessage     LatestMessage      `json:"latestMessage"`
	IsGroupChat       bool               `json:"isGroupChat,omitempty"`
	Timestamp         int64              `json:"timestamp"`
	Status            int64              `json:"status"`
	HexHash           string             `json:"hexHash"`
	Type              int64              `json:"type"`
	SelfParticipantId string             `json:"selfParticipantId,omitempty"`
	Participants      []Participant      `json:"participants"`
	ParticipantIds    []string           `json:"participantIds,omitempty"` // excluded self id
}

type Conversations struct {
	cache *Cache
	/*
	   {0: "1", 1: "4"}
	   order -> conversationId
	       index 0 = first conversation in order
	*/
	Order map[int]string `json:"order"`
	/*
	   Map conversations by conversationId
	*/
	Conversations map[string]*Conversation `json:"conversations"`
}

func (c *Conversations) SetCache(cache *Cache) {
	c.cache = cache
}

func (c *Conversations) DeleteConversation(convId string) {
	delete(c.Conversations, convId)
}

func (c *Conversations) UpdateConversation(conversation *binary.Conversation) *Conversation {
	newConversation := NewConversation(c.cache, conversation)
	c.Conversations[conversation.ConversationId] = newConversation
	return newConversation
}

func (c *Conversation) GetMessage(msgId string) (Message, error) {
	message, foundMsg := c.Messages[msgId]
	if !foundMsg {
		return Message{}, fmt.Errorf("could not find that message cached")
	}
	return message, nil
}

func (c *Conversation) Delete() {
	c.cache.Conversations.DeleteConversation(c.ConversationId)
}

func (c *Conversation) UpdateMessage(msg *binary.Message) Message {
	newMsg := NewMessage(c.cache, msg)

	if c.Messages == nil {
		log.Println("c.messages was nil so created new map")
		c.Messages = make(map[string]Message)
	}

	c.Messages[msg.MessageId] = newMsg
	return newMsg
}

func (c *Conversations) GetConversationByOrder(order int) (*Conversation, error) {
	convId, ok := c.Order[order]
	if !ok {
		return &Conversation{}, fmt.Errorf("could not find a conversation that occupies that order")
	}
	conversation, foundConvo := c.Conversations[convId]
	if !foundConvo {
		return &Conversation{}, fmt.Errorf("could not find that conversation cached, oddly enough it seems to be cached in the order map though... investigate further")
	}
	return conversation, nil
}

func (c *Conversation) GetOrderSlice() []string {
	keys := make([]string, 0, len(c.Messages))
	for k := range c.Messages {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return c.Messages[keys[i]].Timestamp < c.Messages[keys[j]].Timestamp
	})
	return keys
}

func (c *Conversations) GetOrderSlice() []int {
	s := make([]int, 0)
	for i := range c.Order {
		s = append(s, i)
	}
	return s
}

func (c *Conversations) GetConversation(convId string) (*Conversation, error) {
	convo, ok := c.Conversations[convId]
	if !ok {
		// handle error, such as creating a new conversation or returning
		return &Conversation{}, fmt.Errorf("could not find conversation cached")
	} else {
		return convo, nil
	}
}

func NewConversation(cache *Cache, conv *binary.Conversation) *Conversation {
	currConv, convErr := cache.Conversations.GetConversation(conv.ConversationId)
	participants := ParseParticipants(conv.Participants)
	newConversation := Conversation{
		cache:             cache,
		ConversationId:    conv.ConversationId,
		DisplayName:       conv.Name,
		Timestamp:         conv.TimestampMs,
		Status:            conv.Status,
		HexHash:           conv.HashHex,
		Type:              conv.Type,
		Participants:      participants,
		ParticipantIds:    conv.OtherParticipants,
		SelfParticipantId: conv.SelfParticipantId,
	}

	if conv.LatestMessage != nil {
		newConversation.LatestMessage = LatestMessage{
			Content:     conv.LatestMessage.Content,
			FromMe:      conv.LatestMessage.FromMe,
			DisplayName: conv.LatestMessage.DisplayName,
			MessageId:   conv.MessageId,
		}
	}

	if convErr == nil {
		newConversation.MessageOrder = currConv.MessageOrder
		newConversation.Messages = currConv.Messages
	} else {
		newConversation.MessageOrder = make(map[int]string)
		newConversation.Messages = make(map[string]Message)
	}
	return &newConversation
}

func ParseParticipants(participants []*binary.Participant) []Participant {
	partSlice := make([]Participant, 0)
	for _, p := range participants {
		partSlice = append(partSlice, Participant{
			SmallInfo: &SmallInfo{
				Type:          p.SmallInfo.Type,
				Number:        p.SmallInfo.Number,
				ParticipantId: p.SmallInfo.ParticipantId,
			},
			HexHash:     p.HashHex,
			IsMe:        p.IsMe,
			Bs:          p.Bs,
			DisplayName: p.DisplayName,
		})
	}
	return partSlice
}

func NewMessage(cache *Cache, message *binary.Message) Message {
	msg := Message{
		cache:     cache,
		MessageId: message.MessageId,
		ConvId:    message.ConversationId,
		From: IsFromMe{
			FromMe: message.From.FromMe,
		},
		Timestamp:     message.Timestamp,
		ParticipantId: message.ParticipantId,
		MessageType:   message.Type.String(),
		MessageStatus: MessageStatus{
			Code:    message.MessageStatus.Code,
			ErrMsg:  message.MessageStatus.ErrMsg,
			MsgType: message.MessageStatus.MsgStatus,
		},
		MessageData: make([]MessageData, 0),
	}
	for _, data := range message.MessageInfo {
		msgData := MessageData{
			OrderInternal: data.OrderInternal,
		}
		switch d := data.Data.(type) {
		case *binary.MessageInfo_ImageContent:
			msgData.ImageData = &ImageMessage{
				SomeNumber:    d.ImageContent.SomeNumber,
				ImageId:       d.ImageContent.ImageId,
				ImageName:     d.ImageContent.ImageName,
				Size:          d.ImageContent.Size,
				Pixels:        ImagePixels{Width: d.ImageContent.Pixels.Width, Height: d.ImageContent.Pixels.Height},
				ImageBuffer:   d.ImageContent.ImageData,
				DecryptionKey: d.ImageContent.DecryptionKey,
			}
		case *binary.MessageInfo_MessageContent:
			msgData.TextData = &TextMessage{
				Content: d.MessageContent.Content,
			}
		}
		msg.MessageData = append(msg.MessageData, msgData)
	}
	return msg
}
