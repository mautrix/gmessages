package libgm

import (
	"fmt"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
)

func (c *Client) ListConversations(count int64, folder binary.ListConversationsPayload_Folder) (*binary.Conversations, error) {
	payload := &binary.ListConversationsPayload{Count: count, Folder: folder}
	//var actionType binary.ActionType
	//if !c.synced {
	//	actionType = binary.ActionType_LIST_CONVERSATIONS_SYNC
	//	c.synced = true
	//} else {
	actionType := binary.ActionType_LIST_CONVERSATIONS

	response, err := c.sessionHandler.sendMessage(actionType, payload)
	if err != nil {
		return nil, err
	}

	res, ok := response.Data.Decrypted.(*binary.Conversations)
	if !ok {
		return nil, fmt.Errorf("unexpected response type %T, expected *binary.Conversations", response.Data.Decrypted)
	}

	return res, nil
}

func (c *Client) ListContacts() (*binary.ListContactsResponse, error) {
	payload := &binary.ListContactsPayload{
		I1: 1,
		I2: 350,
		I3: 50,
	}
	actionType := binary.ActionType_LIST_CONTACTS

	response, err := c.sessionHandler.sendMessage(actionType, payload)
	if err != nil {
		return nil, err
	}

	res, ok := response.Data.Decrypted.(*binary.ListContactsResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type %T, expected *binary.ListContactsResponse", response.Data.Decrypted)
	}

	return res, nil
}

func (c *Client) ListTopContacts() (*binary.ListTopContactsResponse, error) {
	payload := &binary.ListTopContactsPayload{
		Count: 8,
	}
	actionType := binary.ActionType_LIST_TOP_CONTACTS

	response, err := c.sessionHandler.sendMessage(actionType, payload)
	if err != nil {
		return nil, err
	}

	res, ok := response.Data.Decrypted.(*binary.ListTopContactsResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type %T, expected *binary.ListTopContactsResponse", response.Data.Decrypted)
	}

	return res, nil
}

func (c *Client) GetOrCreateConversation(numbers []string) (*binary.GetOrCreateConversationResponse, error) {
	contacts := make([]*binary.ContactNumber, len(numbers))
	for i, number := range numbers {
		contacts[i] = &binary.ContactNumber{
			MysteriousInt: 7,
			Number:        number,
			Number2:       number,
		}
	}
	payload := &binary.GetOrCreateConversationPayload{
		Numbers: contacts,
	}
	actionType := binary.ActionType_GET_OR_CREATE_CONVERSATION

	response, err := c.sessionHandler.sendMessage(actionType, payload)
	if err != nil {
		return nil, err
	}

	res, ok := response.Data.Decrypted.(*binary.GetOrCreateConversationResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type %T, expected *binary.GetOrCreateConversationResponse", response.Data.Decrypted)
	}

	return res, nil
}

func (c *Client) GetConversationType(conversationID string) (*binary.GetConversationTypeResponse, error) {
	payload := &binary.ConversationTypePayload{ConversationID: conversationID}
	actionType := binary.ActionType_GET_CONVERSATION_TYPE

	response, err := c.sessionHandler.sendMessage(actionType, payload)
	if err != nil {
		return nil, err
	}

	res, ok := response.Data.Decrypted.(*binary.GetConversationTypeResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type %T, expected *binary.GetConversationTypeResponse", response.Data.Decrypted)
	}

	return res, nil
}

func (c *Client) FetchMessages(conversationID string, count int64, cursor *binary.Cursor) (*binary.FetchMessagesResponse, error) {
	payload := &binary.FetchConversationMessagesPayload{ConversationID: conversationID, Count: count}
	if cursor != nil {
		payload.Cursor = cursor
	}

	actionType := binary.ActionType_LIST_MESSAGES

	response, err := c.sessionHandler.sendMessage(actionType, payload)
	if err != nil {
		return nil, err
	}

	res, ok := response.Data.Decrypted.(*binary.FetchMessagesResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type %T, expected *binary.FetchMessagesResponse", response.Data.Decrypted)
	}

	return res, nil
}

func (c *Client) SendMessage(payload *binary.SendMessagePayload) (*binary.SendMessageResponse, error) {
	actionType := binary.ActionType_SEND_MESSAGE

	response, err := c.sessionHandler.sendMessage(actionType, payload)
	if err != nil {
		return nil, err
	}

	res, ok := response.Data.Decrypted.(*binary.SendMessageResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type %T, expected *binary.SendMessageResponse", response.Data.Decrypted)
	}

	return res, nil
}

func (c *Client) GetParticipantThumbnail(convID string) (*binary.ParticipantThumbnail, error) {
	payload := &binary.GetParticipantThumbnailPayload{ConversationID: convID}
	actionType := binary.ActionType_GET_PARTICIPANTS_THUMBNAIL

	response, err := c.sessionHandler.sendMessage(actionType, payload)
	if err != nil {
		return nil, err
	}

	res, ok := response.Data.Decrypted.(*binary.ParticipantThumbnail)
	if !ok {
		return nil, fmt.Errorf("unexpected response type %T, expected *binary.ParticipantThumbnail", response.Data.Decrypted)
	}

	return res, nil
}

func (c *Client) UpdateConversation(convBuilder *ConversationBuilder) (*binary.UpdateConversationResponse, error) {
	data := &binary.UpdateConversationPayload{}

	payload, buildErr := convBuilder.Build(data)
	if buildErr != nil {
		panic(buildErr)
	}

	actionType := binary.ActionType_UPDATE_CONVERSATION

	response, err := c.sessionHandler.sendMessage(actionType, payload)
	if err != nil {
		return nil, err
	}

	res, ok := response.Data.Decrypted.(*binary.UpdateConversationResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type %T, expected *binary.UpdateConversationResponse", response.Data.Decrypted)
	}

	return res, nil
}

func (c *Client) SetTyping(convID string) error {
	payload := &binary.TypingUpdatePayload{Data: &binary.SetTypingIn{ConversationID: convID, Typing: true}}
	actionType := binary.ActionType_TYPING_UPDATES

	_, err := c.sessionHandler.sendMessage(actionType, payload)
	return err
}
