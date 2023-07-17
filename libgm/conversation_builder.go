package libgm

import (
	"fmt"

	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
)

type ConversationBuilderError struct {
	errMsg string
}

func (cbe *ConversationBuilderError) Error() string {
	return fmt.Sprintf("Failed to build conversation builder: %s", cbe.errMsg)
}

type ConversationBuilder struct {
	conversationId string

	actionStatus gmproto.ConversationActionStatus
	status       gmproto.ConversationStatus
	muteStatus   *gmproto.ConversationMuteStatus
}

func (cb *ConversationBuilder) SetConversationId(conversationId string) *ConversationBuilder {
	cb.conversationId = conversationId
	return cb
}

// For block, unblock, block & report
func (cb *ConversationBuilder) SetConversationActionStatus(actionStatus gmproto.ConversationActionStatus) *ConversationBuilder {
	cb.actionStatus = actionStatus
	return cb
}

// For archive, unarchive, delete
func (cb *ConversationBuilder) SetConversationStatus(status gmproto.ConversationStatus) *ConversationBuilder {
	cb.status = status
	return cb
}

func (cb *ConversationBuilder) SetMuteStatus(muteStatus *gmproto.ConversationMuteStatus) *ConversationBuilder {
	cb.muteStatus = muteStatus
	return cb
}

func (cb *ConversationBuilder) Build(protoMessage proto.Message) (proto.Message, error) {
	if cb.conversationId == "" {
		return nil, &ConversationBuilderError{errMsg: "conversationID can not be empty"}
	}

	switch protoMessage.(type) {
	case *gmproto.UpdateConversationRequest:
		payload, failedBuild := cb.buildUpdateConversationPayload()
		if failedBuild != nil {
			return nil, failedBuild
		}
		return payload, nil
	default:
		panic("Invalid protoMessage conversation builder type")
	}
	return nil, &ConversationBuilderError{errMsg: "failed to build for unknown reasons"}
}

func (cb *ConversationBuilder) buildUpdateConversationPayload() (*gmproto.UpdateConversationRequest, error) {
	if cb.actionStatus == 0 && cb.status == 0 && cb.muteStatus == nil {
		return nil, &ConversationBuilderError{errMsg: "actionStatus, status & muteStatus can not be empty when updating conversation, set atleast 1"}
	}

	payload := &gmproto.UpdateConversationRequest{}

	if cb.actionStatus != 0 {
		payload.Action = cb.actionStatus
		payload.Action5 = &gmproto.ConversationAction5{
			Field2: true,
		}
		payload.ConversationID = cb.conversationId
	} else if cb.status != 0 || cb.muteStatus != nil {
		payload.Data = &gmproto.UpdateConversationData{ConversationID: cb.conversationId}
		if cb.muteStatus != nil {
			payload.Data.Data = &gmproto.UpdateConversationData_Mute{Mute: *cb.muteStatus}
		} else if cb.status != 0 {
			payload.Data.Data = &gmproto.UpdateConversationData_Status{Status: cb.status}
		}
	}

	return payload, nil
}

func (c *Client) NewConversationBuilder() *ConversationBuilder {
	return &ConversationBuilder{}
}
