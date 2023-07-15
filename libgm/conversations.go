package libgm

import (
	"fmt"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
)

type Conversations struct {
	client *Client

	synced bool
}

// default is 25 count
func (c *Conversations) List(count int64) (*binary.Conversations, error) {
	payload := &binary.ListCoversationsPayload{Count: count, Field4: 1}
	var actionType binary.ActionType

	if !c.synced {
		actionType = binary.ActionType_LIST_CONVERSATIONS_SYNC
		c.synced = true
	} else {
		actionType = binary.ActionType_LIST_CONVERSATIONS
	}

	sentRequestId, sendErr := c.client.sessionHandler.completeSendMessage(actionType, true, payload)
	if sendErr != nil {
		return nil, sendErr
	}

	response, err := c.client.sessionHandler.WaitForResponse(sentRequestId, actionType)
	if err != nil {
		return nil, err
	}

	res, ok := response.Data.Decrypted.(*binary.Conversations)
	if !ok {
		return nil, fmt.Errorf("unexpected response type %T, expected *binary.Conversations", response.Data.Decrypted)
	}

	return res, nil
}

func (c *Conversations) GetType(conversationId string) (*binary.GetConversationTypeResponse, error) {
	payload := &binary.ConversationTypePayload{ConversationID: conversationId}
	actionType := binary.ActionType_GET_CONVERSATION_TYPE

	sentRequestId, sendErr := c.client.sessionHandler.completeSendMessage(actionType, true, payload)
	if sendErr != nil {
		return nil, sendErr
	}

	response, err := c.client.sessionHandler.WaitForResponse(sentRequestId, actionType)
	if err != nil {
		return nil, err
	}

	res, ok := response.Data.Decrypted.(*binary.GetConversationTypeResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type %T, expected *binary.GetConversationTypeResponse", response.Data.Decrypted)
	}

	return res, nil
}

func (c *Conversations) FetchMessages(conversationId string, count int64, cursor *binary.Cursor) (*binary.FetchMessagesResponse, error) {
	payload := &binary.FetchConversationMessagesPayload{ConversationID: conversationId, Count: count}
	if cursor != nil {
		payload.Cursor = cursor
	}

	actionType := binary.ActionType_LIST_MESSAGES

	sentRequestId, sendErr := c.client.sessionHandler.completeSendMessage(actionType, true, payload)
	if sendErr != nil {
		return nil, sendErr
	}

	response, err := c.client.sessionHandler.WaitForResponse(sentRequestId, actionType)
	if err != nil {
		return nil, err
	}

	res, ok := response.Data.Decrypted.(*binary.FetchMessagesResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type %T, expected *binary.FetchMessagesResponse", response.Data.Decrypted)
	}

	return res, nil
}

func (c *Conversations) SendMessage(payload *binary.SendMessagePayload) (*binary.SendMessageResponse, error) {
	actionType := binary.ActionType_SEND_MESSAGE

	sentRequestId, sendErr := c.client.sessionHandler.completeSendMessage(actionType, true, payload)
	if sendErr != nil {
		return nil, sendErr
	}

	response, err := c.client.sessionHandler.WaitForResponse(sentRequestId, actionType)
	if err != nil {
		return nil, err
	}

	res, ok := response.Data.Decrypted.(*binary.SendMessageResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type %T, expected *binary.SendMessageResponse", response.Data.Decrypted)
	}

	c.client.Logger.Debug().Any("res", res).Msg("sent message!")
	return res, nil
}

func (c *Conversations) GetParticipantThumbnail(convID string) (*binary.ParticipantThumbnail, error) {
	payload := &binary.GetParticipantThumbnailPayload{ConversationID: convID}
	actionType := binary.ActionType_GET_PARTICIPANTS_THUMBNAIL

	sentRequestId, sendErr := c.client.sessionHandler.completeSendMessage(actionType, true, payload)
	if sendErr != nil {
		return nil, sendErr
	}

	response, err := c.client.sessionHandler.WaitForResponse(sentRequestId, actionType)
	if err != nil {
		return nil, err
	}

	res, ok := response.Data.Decrypted.(*binary.ParticipantThumbnail)
	if !ok {
		return nil, fmt.Errorf("unexpected response type %T, expected *binary.ParticipantThumbnail", response.Data.Decrypted)
	}

	return res, nil
}

func (c *Conversations) Update(convBuilder *ConversationBuilder) (*binary.UpdateConversationResponse, error) {
	data := &binary.UpdateConversationPayload{}

	payload, buildErr := convBuilder.Build(data)
	if buildErr != nil {
		panic(buildErr)
	}

	actionType := binary.ActionType_UPDATE_CONVERSATION

	sentRequestId, sendErr := c.client.sessionHandler.completeSendMessage(actionType, true, payload)
	if sendErr != nil {
		return nil, sendErr
	}

	response, err := c.client.sessionHandler.WaitForResponse(sentRequestId, actionType)
	if err != nil {
		return nil, err
	}

	res, ok := response.Data.Decrypted.(*binary.UpdateConversationResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type %T, expected *binary.UpdateConversationResponse", response.Data.Decrypted)
	}

	return res, nil
}

func (c *Conversations) SetTyping(convID string) error {
	payload := &binary.TypingUpdatePayload{Data: &binary.SetTypingIn{ConversationID: convID, Typing: true}}
	actionType := binary.ActionType_TYPING_UPDATES

	sentRequestId, sendErr := c.client.sessionHandler.completeSendMessage(actionType, true, payload)
	if sendErr != nil {
		return sendErr
	}

	_, err := c.client.sessionHandler.WaitForResponse(sentRequestId, actionType)
	if err != nil {
		return err
	}
	return nil
}
