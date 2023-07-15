package routes

import (
	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
)

type Route struct {
	Action         binary.ActionType
	MessageType    binary.MessageType
	BugleRoute     binary.BugleRoute
	ResponseStruct proto.Message
	UseSessionID   bool
	UseTTL         bool
}

var Routes = map[binary.ActionType]Route{
	binary.ActionType_IS_BUGLE_DEFAULT:           IS_BUGLE_DEFAULT,
	binary.ActionType_GET_UPDATES:                GET_UPDATES,
	binary.ActionType_LIST_CONVERSATIONS:         LIST_CONVERSATIONS,
	binary.ActionType_LIST_CONVERSATIONS_SYNC:    LIST_CONVERSATIONS_WITH_UPDATES,
	binary.ActionType_MESSAGE_READ:               MESSAGE_READ,
	binary.ActionType_NOTIFY_DITTO_ACTIVITY:      NOTIFY_DITTO_ACTIVITY,
	binary.ActionType_GET_CONVERSATION_TYPE:      GET_CONVERSATION_TYPE,
	binary.ActionType_LIST_MESSAGES:              LIST_MESSAGES,
	binary.ActionType_SEND_MESSAGE:               SEND_MESSAGE,
	binary.ActionType_SEND_REACTION:              SEND_REACTION,
	binary.ActionType_DELETE_MESSAGE:             DELETE_MESSAGE,
	binary.ActionType_GET_PARTICIPANTS_THUMBNAIL: GET_PARTICIPANT_THUMBNAIL,
}
