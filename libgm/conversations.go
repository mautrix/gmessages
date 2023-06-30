package libgm

import (
	"fmt"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
)

type Conversations struct {
	client *Client

	watching string // current open conversation

	openConversation          openConversation
	fetchConversationMessages fetchConversationMessages
}

func (c *Conversations) List(count int64) (*binary.Conversations, error) {
	encryptedProtoPayload := &binary.ListCoversationsPayload{Count: count, Field4: 1}
	instruction, _ := c.client.instructions.GetInstruction(LIST_CONVERSATIONS)
	sentRequestID, _ := c.client.createAndSendRequest(instruction.Opcode, c.client.ttl, false, encryptedProtoPayload.ProtoReflect())

	responses, err := c.client.sessionHandler.WaitForResponse(sentRequestID, instruction.Opcode)
	if err != nil {
		return nil, err
	}
	decryptedProto, decryptErr := responses[0].decryptData()
	if decryptErr != nil {
		return nil, decryptErr
	}

	if decryptedData, ok := decryptedProto.(*binary.Conversations); ok {
		return decryptedData, nil
	} else {
		return nil, fmt.Errorf("failed to assert decryptedProto into type Conversations")
	}
}

func (c *Conversations) SendMessage(messageBuilder *MessageBuilder, selfParticipantID string) (*binary.SendMessageResponse, error) {
	hasSelfParticipantId := messageBuilder.GetSelfParticipantID()
	if hasSelfParticipantId == "" {
		messageBuilder.SetSelfParticipantID(selfParticipantID)
	}

	encryptedProtoPayload, failedToBuild := messageBuilder.Build()
	if failedToBuild != nil {
		panic(failedToBuild)
	}

	instruction, _ := c.client.instructions.GetInstruction(SEND_TEXT_MESSAGE)
	c.client.Logger.Debug().Any("payload", encryptedProtoPayload).Msg("SendMessage Payload")
	sentRequestID, _ := c.client.createAndSendRequest(instruction.Opcode, c.client.ttl, false, encryptedProtoPayload.ProtoReflect())

	responses, err := c.client.sessionHandler.WaitForResponse(sentRequestID, instruction.Opcode)
	if err != nil {
		panic(err)
		return nil, err
	}

	decryptedProto, decryptErr := responses[0].decryptData()
	if decryptErr != nil {
		return nil, decryptErr
	}

	if decryptedData, ok := decryptedProto.(*binary.SendMessageResponse); ok {
		return decryptedData, nil
	} else {
		return nil, fmt.Errorf("failed to assert decryptedProto into type SendMessageResponse")
	}
}

func (c *Conversations) FetchMessages(convId string, count int64, cursor *binary.Cursor) (*binary.FetchMessagesResponse, error) {

	var openConversationRes []*Response
	var openConversationErr error
	if c.watching != convId {
		openConversationRes, openConversationErr = c.openConversation.Execute(convId)
		if openConversationErr != nil {
			return nil, openConversationErr
		}
		c.watching = convId
	}

	fetchMessagesRes, fetchMessagesErr := c.fetchConversationMessages.Execute(convId, count, cursor)
	if fetchMessagesErr != nil {
		return nil, fetchMessagesErr
	}

	fetchedMessagesResponse, processFail := c.client.processFetchMessagesResponse(fetchMessagesRes, openConversationRes, nil)
	if processFail != nil {
		return nil, processFail
	}

	return fetchedMessagesResponse, nil
}

type fetchConversationMessages struct {
	client *Client
}

func (f *fetchConversationMessages) Execute(convId string, count int64, cursor *binary.Cursor) ([]*Response, error) {
	encryptedProtoPayload := &binary.FetchConversationMessagesPayload{ConversationID: convId, Count: count, Cursor: cursor}
	instruction, _ := f.client.instructions.GetInstruction(FETCH_MESSAGES_CONVERSATION)
	sentRequestID, _ := f.client.createAndSendRequest(instruction.Opcode, f.client.ttl, false, encryptedProtoPayload.ProtoReflect())

	responses, err := f.client.sessionHandler.WaitForResponse(sentRequestID, instruction.Opcode)
	if err != nil {
		return nil, err
	}

	return responses, nil
}

type openConversation struct {
	client *Client
}

func (o *openConversation) Execute(convId string) ([]*Response, error) {
	encryptedProtoPayload := &binary.OpenConversationPayload{ConversationID: convId}
	instruction, _ := o.client.instructions.GetInstruction(OPEN_CONVERSATION)
	sentRequestID, _ := o.client.createAndSendRequest(instruction.Opcode, o.client.ttl, false, encryptedProtoPayload.ProtoReflect())

	responses, err := o.client.sessionHandler.WaitForResponse(sentRequestID, instruction.Opcode)
	if err != nil {
		return nil, err
	}

	// Rest of the processing...

	return responses, nil
}

/*
func (c *Conversations) SendMessage(conversationID string, content string, participantCount string) (*binary.SendMessageResponse, error) {
	encryptedProtoPayload := payload.NewSendConversationTextMessage(conversationID, content, participantCount)
	sentRequestID, _ := c.client.createAndSendRequest(3, c.client.ttl, false, encryptedProtoPayload.ProtoReflect())
	c.client.Logger.Debug().Any("requestId", sentRequestID).Msg("Sent sendmessage request.")
	response, responseErr := c.client.sessionHandler.WaitForResponse(sentRequestID, 3)
	if responseErr != nil {
		c.client.Logger.Err(responseErr).Msg("SendMessage channel response error")
		return nil, responseErr
	} else {
		decryptedProto, decryptErr := response.decryptData()
		if decryptErr != nil {
			return nil, decryptErr
		}

		if decryptedData, ok := decryptedProto.(*binary.SendMessageResponse); ok {
			return decryptedData, nil
		} else {
			return nil, fmt.Errorf("failed to assert decryptedProto into type SendMessageResponse")
		}
	}
}

func (c *Conversations) PrepareOpen() (interface{}, error) {
	encryptedProtoPayload := &binary.PrepareOpenConversationPayload{Field2:1}
	sentRequestID, _ := c.client.createAndSendRequest(22, c.client.ttl, false, encryptedProtoPayload.ProtoReflect())
	c.client.Logger.Debug().Any("requestId", sentRequestID).Msg("Sent PrepareOpenConversation request.")
	response, responseErr := c.client.sessionHandler.WaitForResponse(sentRequestID, 22)
	if responseErr != nil {
		c.client.Logger.Err(responseErr).Msg("PrepareOpenConversation channel response error")
		return nil, responseErr
	} else {
		c.client.Logger.Info().Any("response", response).Msg("PrepareOpenConversation response data")
	}
	return nil, nil
}

func (c *Conversations) Open(conversationID string) (interface{}, error) {
	encryptedProtoPayload := &binary.OpenConversationPayload{ConversationID:conversationID}
	sentRequestID, _ := c.client.createAndSendRequest(21, c.client.ttl, false, encryptedProtoPayload.ProtoReflect())
	c.client.Logger.Debug().Any("requestId", sentRequestID).Msg("Sent OpenConversation request.")
	response, responseErr := c.client.sessionHandler.WaitForResponse(sentRequestID, 21)
	if responseErr != nil {
		c.client.Logger.Err(responseErr).Msg("OpenConversation channel response error")
		return nil, responseErr
	} else {
		c.client.Logger.Info().Any("response", response).Msg("OpenConversation response data")
	}
	return nil, nil
}

func (c *Conversations) FetchMessages(conversationID string, count int64) (*binary.FetchMessagesResponse, error) {
	encryptedProtoPayload := &binary.FetchConversationMessagesPayload{ConversationID:conversationID,Count:count}
	sentRequestID, _ := c.client.createAndSendRequest(2, c.client.ttl, false, encryptedProtoPayload.ProtoReflect())
	c.client.Logger.Debug().Any("requestId", sentRequestID).Msg("Sent FetchMessages request.")
	response, responseErr := c.client.sessionHandler.WaitForResponse(sentRequestID, 2)
	if responseErr != nil {
		c.client.Logger.Err(responseErr).Msg("FetchMessages channel response error")
		return nil, responseErr
	} else {
		decryptedMessages, decryptedErr := c.client.newMessagesResponse(response)
		if decryptedErr != nil {
			return nil, decryptedErr
		}
		return decryptedMessages, nil
	}
}
*/
