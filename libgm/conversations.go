package libgm

import (
	"fmt"

	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
)

func (c *Client) ListConversations(count int64, folder gmproto.ListConversationsPayload_Folder) (*gmproto.Conversations, error) {
	payload := &gmproto.ListConversationsPayload{Count: count, Folder: folder}
	//var actionType gmproto.ActionType
	//if !c.synced {
	//	actionType = gmproto.ActionType_LIST_CONVERSATIONS_SYNC
	//	c.synced = true
	//} else {
	actionType := gmproto.ActionType_LIST_CONVERSATIONS

	response, err := c.sessionHandler.sendMessage(actionType, payload)
	if err != nil {
		return nil, err
	}

	res, ok := response.DecryptedMessage.(*gmproto.Conversations)
	if !ok {
		return nil, fmt.Errorf("unexpected response type %T, expected *gmproto.Conversations", response.DecryptedMessage)
	}

	return res, nil
}

func (c *Client) ListContacts() (*gmproto.ListContactsResponse, error) {
	payload := &gmproto.ListContactsPayload{
		I1: 1,
		I2: 350,
		I3: 50,
	}
	actionType := gmproto.ActionType_LIST_CONTACTS

	response, err := c.sessionHandler.sendMessage(actionType, payload)
	if err != nil {
		return nil, err
	}

	res, ok := response.DecryptedMessage.(*gmproto.ListContactsResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type %T, expected *gmproto.ListContactsResponse", response.DecryptedMessage)
	}

	return res, nil
}

func (c *Client) ListTopContacts() (*gmproto.ListTopContactsResponse, error) {
	payload := &gmproto.ListTopContactsPayload{
		Count: 8,
	}
	actionType := gmproto.ActionType_LIST_TOP_CONTACTS

	response, err := c.sessionHandler.sendMessage(actionType, payload)
	if err != nil {
		return nil, err
	}

	res, ok := response.DecryptedMessage.(*gmproto.ListTopContactsResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type %T, expected *gmproto.ListTopContactsResponse", response.DecryptedMessage)
	}

	return res, nil
}

func (c *Client) GetOrCreateConversation(req *gmproto.GetOrCreateConversationPayload) (*gmproto.GetOrCreateConversationResponse, error) {
	actionType := gmproto.ActionType_GET_OR_CREATE_CONVERSATION

	response, err := c.sessionHandler.sendMessage(actionType, req)
	if err != nil {
		return nil, err
	}

	res, ok := response.DecryptedMessage.(*gmproto.GetOrCreateConversationResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type %T, expected *gmproto.GetOrCreateConversationResponse", response.DecryptedMessage)
	}

	return res, nil
}

func (c *Client) GetConversationType(conversationID string) (*gmproto.GetConversationTypeResponse, error) {
	payload := &gmproto.ConversationTypePayload{ConversationID: conversationID}
	actionType := gmproto.ActionType_GET_CONVERSATION_TYPE

	response, err := c.sessionHandler.sendMessage(actionType, payload)
	if err != nil {
		return nil, err
	}

	res, ok := response.DecryptedMessage.(*gmproto.GetConversationTypeResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type %T, expected *gmproto.GetConversationTypeResponse", response.DecryptedMessage)
	}

	return res, nil
}

func (c *Client) FetchMessages(conversationID string, count int64, cursor *gmproto.Cursor) (*gmproto.FetchMessagesResponse, error) {
	payload := &gmproto.FetchConversationMessagesPayload{ConversationID: conversationID, Count: count}
	if cursor != nil {
		payload.Cursor = cursor
	}

	actionType := gmproto.ActionType_LIST_MESSAGES

	response, err := c.sessionHandler.sendMessage(actionType, payload)
	if err != nil {
		return nil, err
	}

	res, ok := response.DecryptedMessage.(*gmproto.FetchMessagesResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type %T, expected *gmproto.FetchMessagesResponse", response.DecryptedMessage)
	}

	return res, nil
}

func (c *Client) SendMessage(payload *gmproto.SendMessagePayload) (*gmproto.SendMessageResponse, error) {
	actionType := gmproto.ActionType_SEND_MESSAGE

	response, err := c.sessionHandler.sendMessage(actionType, payload)
	if err != nil {
		return nil, err
	}

	res, ok := response.DecryptedMessage.(*gmproto.SendMessageResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type %T, expected *gmproto.SendMessageResponse", response.DecryptedMessage)
	}

	return res, nil
}

func (c *Client) GetParticipantThumbnail(convID string) (*gmproto.ParticipantThumbnail, error) {
	payload := &gmproto.GetParticipantThumbnailPayload{ConversationID: convID}
	actionType := gmproto.ActionType_GET_PARTICIPANTS_THUMBNAIL

	response, err := c.sessionHandler.sendMessage(actionType, payload)
	if err != nil {
		return nil, err
	}

	res, ok := response.DecryptedMessage.(*gmproto.ParticipantThumbnail)
	if !ok {
		return nil, fmt.Errorf("unexpected response type %T, expected *gmproto.ParticipantThumbnail", response.DecryptedMessage)
	}

	return res, nil
}

func (c *Client) UpdateConversation(convBuilder *ConversationBuilder) (*gmproto.UpdateConversationResponse, error) {
	data := &gmproto.UpdateConversationPayload{}

	payload, buildErr := convBuilder.Build(data)
	if buildErr != nil {
		panic(buildErr)
	}

	actionType := gmproto.ActionType_UPDATE_CONVERSATION

	response, err := c.sessionHandler.sendMessage(actionType, payload)
	if err != nil {
		return nil, err
	}

	res, ok := response.DecryptedMessage.(*gmproto.UpdateConversationResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type %T, expected *gmproto.UpdateConversationResponse", response.DecryptedMessage)
	}

	return res, nil
}

func (c *Client) SetTyping(convID string) error {
	payload := &gmproto.TypingUpdatePayload{Data: &gmproto.SetTypingIn{ConversationID: convID, Typing: true}}
	actionType := gmproto.ActionType_TYPING_UPDATES

	_, err := c.sessionHandler.sendMessage(actionType, payload)
	return err
}
