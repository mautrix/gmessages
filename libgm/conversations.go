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
		return nil, fmt.Errorf("failed to assert response into Conversations")
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
		return nil, fmt.Errorf("failed to assert response into GetConversationTypeResponse")
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
		return nil, fmt.Errorf("failed to assert response into FetchMessagesResponse")
	}

	decryptErr := c.client.decryptMedias(res)
	if decryptErr != nil {
		return nil, decryptErr
	}

	c.client.Logger.Debug().Any("messageData", res).Msg("fetchmessages")
	return res, nil
}

func (c *Conversations) SendMessage(messageBuilder *MessageBuilder) (*binary.SendMessageResponse, error) {
	payload, failedToBuild := messageBuilder.Build()
	if failedToBuild != nil {
		return nil, failedToBuild
	}

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
		return nil, fmt.Errorf("failed to assert response into SendMessageResponse")
	}

	c.client.Logger.Debug().Any("res", res).Msg("sent message!")
	return res, nil
}
