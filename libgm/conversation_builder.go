package libgm

import (
	"fmt"
	"log"

	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
)

type ConversationBuilderError struct {
	errMsg string
}

func (cbe *ConversationBuilderError) Error() string {
	return fmt.Sprintf("Failed to build conversation builder: %s", cbe.errMsg)
}

type ConversationBuilder struct {
	conversationId string

	actionStatus binary.ConversationActionStatus
	status       binary.ConversationStatus
	muteStatus   *binary.ConversationMuteStatus
}

func (cb *ConversationBuilder) SetConversationId(conversationId string) *ConversationBuilder {
	cb.conversationId = conversationId
	return cb
}

// For block, unblock, block & report
func (cb *ConversationBuilder) SetConversationActionStatus(actionStatus binary.ConversationActionStatus) *ConversationBuilder {
	cb.actionStatus = actionStatus
	return cb
}

// For archive, unarchive, delete
func (cb *ConversationBuilder) SetConversationStatus(status binary.ConversationStatus) *ConversationBuilder {
	cb.status = status
	return cb
}

func (cb *ConversationBuilder) SetMuteStatus(muteStatus *binary.ConversationMuteStatus) *ConversationBuilder {
	cb.muteStatus = muteStatus
	return cb
}

func (cb *ConversationBuilder) Build(protoMessage proto.Message) (proto.Message, error) {
	if cb.conversationId == "" {
		return nil, &ConversationBuilderError{errMsg: "conversationID can not be empty"}
	}

	switch protoMessage.(type) {
	case *binary.UpdateConversationPayload:
		payload, failedBuild := cb.buildUpdateConversationPayload()
		if failedBuild != nil {
			return nil, failedBuild
		}
		return payload, nil
	default:
		log.Fatal("Invalid protoMessage conversation builder type")
	}
	return nil, &ConversationBuilderError{errMsg: "failed to build for unknown reasons"}
}

func (cb *ConversationBuilder) buildUpdateConversationPayload() (*binary.UpdateConversationPayload, error) {
	if cb.actionStatus == 0 && cb.status == 0 && cb.muteStatus == nil {
		return nil, &ConversationBuilderError{errMsg: "actionStatus, status & muteStatus can not be empty when updating conversation, set atleast 1"}
	}

	payload := &binary.UpdateConversationPayload{}

	if cb.actionStatus != 0 {
		payload.Action = cb.actionStatus
		payload.Action5 = &binary.ConversationAction5{
			Field2: true,
		}
		payload.ConversationID = cb.conversationId
	} else if cb.status != 0 || cb.muteStatus != nil {
		payload.Data = &binary.UpdateConversationData{ConversationID: cb.conversationId}
		if cb.muteStatus != nil {
			payload.Data.Data = &binary.UpdateConversationData_Mute{Mute: *cb.muteStatus}
		} else if cb.status != 0 {
			payload.Data.Data = &binary.UpdateConversationData_Status{Status: cb.status}
		}
	}

	return payload, nil
}

func (c *Client) NewConversationBuilder() *ConversationBuilder {
	return &ConversationBuilder{}
}
