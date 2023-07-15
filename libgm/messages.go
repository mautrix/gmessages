package libgm

import (
	"fmt"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
)

type Messages struct {
	client *Client
}

func (m *Messages) React(payload *binary.SendReactionPayload) (*binary.SendReactionResponse, error) {
	actionType := binary.ActionType_SEND_REACTION

	sentRequestId, sendErr := m.client.sessionHandler.completeSendMessage(actionType, true, payload)
	if sendErr != nil {
		return nil, sendErr
	}

	response, err := m.client.sessionHandler.WaitForResponse(sentRequestId, actionType)
	if err != nil {
		return nil, err
	}

	res, ok := response.Data.Decrypted.(*binary.SendReactionResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type %T, expected *binary.SendReactionResponse", response.Data.Decrypted)
	}

	m.client.Logger.Debug().Any("res", res).Msg("sent reaction!")
	return res, nil
}

func (m *Messages) Delete(messageId string) (*binary.DeleteMessageResponse, error) {
	payload := &binary.DeleteMessagePayload{MessageID: messageId}
	actionType := binary.ActionType_DELETE_MESSAGE

	sentRequestId, sendErr := m.client.sessionHandler.completeSendMessage(actionType, true, payload)
	if sendErr != nil {
		return nil, sendErr
	}

	response, err := m.client.sessionHandler.WaitForResponse(sentRequestId, actionType)
	if err != nil {
		return nil, err
	}

	res, ok := response.Data.Decrypted.(*binary.DeleteMessageResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type %T, expected *binary.DeleteMessagesResponse", response.Data.Decrypted)
	}

	m.client.Logger.Debug().Any("res", res).Msg("deleted message!")
	return res, nil
}

func (m *Messages) MarkRead(conversationID, messageID string) error {
	payload := &binary.MessageReadPayload{ConversationID: conversationID, MessageID: messageID}
	actionType := binary.ActionType_MESSAGE_READ

	sentRequestId, sendErr := m.client.sessionHandler.completeSendMessage(actionType, true, payload)
	if sendErr != nil {
		return sendErr
	}

	_, err := m.client.sessionHandler.WaitForResponse(sentRequestId, actionType)
	if err != nil {
		return err
	}

	return nil
}
