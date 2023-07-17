package routes

import (
	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
)

type Route struct {
	Action         gmproto.ActionType
	MessageType    gmproto.MessageType
	BugleRoute     gmproto.BugleRoute
	ResponseStruct proto.Message
	UseSessionID   bool
	UseTTL         bool
}

var Routes = map[gmproto.ActionType]Route{
	gmproto.ActionType_IS_BUGLE_DEFAULT:           IS_BUGLE_DEFAULT,
	gmproto.ActionType_GET_UPDATES:                GET_UPDATES,
	gmproto.ActionType_LIST_CONVERSATIONS:         LIST_CONVERSATIONS,
	gmproto.ActionType_LIST_CONVERSATIONS_SYNC:    LIST_CONVERSATIONS_WITH_UPDATES,
	gmproto.ActionType_MESSAGE_READ:               MESSAGE_READ,
	gmproto.ActionType_NOTIFY_DITTO_ACTIVITY:      NOTIFY_DITTO_ACTIVITY,
	gmproto.ActionType_GET_CONVERSATION_TYPE:      GET_CONVERSATION_TYPE,
	gmproto.ActionType_LIST_MESSAGES:              LIST_MESSAGES,
	gmproto.ActionType_SEND_MESSAGE:               SEND_MESSAGE,
	gmproto.ActionType_SEND_REACTION:              SEND_REACTION,
	gmproto.ActionType_DELETE_MESSAGE:             DELETE_MESSAGE,
	gmproto.ActionType_TYPING_UPDATES:             TYPING_UPDATES,
	gmproto.ActionType_GET_PARTICIPANTS_THUMBNAIL: GET_PARTICIPANT_THUMBNAIL,
	gmproto.ActionType_LIST_CONTACTS:              LIST_CONTACTS,
	gmproto.ActionType_LIST_TOP_CONTACTS:          LIST_TOP_CONTACTS,
	gmproto.ActionType_GET_OR_CREATE_CONVERSATION: GET_OR_CREATE_CONVERSATION,
}
