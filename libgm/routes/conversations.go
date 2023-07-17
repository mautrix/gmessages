package routes

import "go.mau.fi/mautrix-gmessages/libgm/gmproto"

var LIST_CONVERSATIONS_WITH_UPDATES = Route{
	Action:         gmproto.ActionType_LIST_CONVERSATIONS,
	MessageType:    gmproto.MessageType_BUGLE_ANNOTATION,
	BugleRoute:     gmproto.BugleRoute_DataEvent,
	ResponseStruct: &gmproto.Conversations{},
	UseSessionID:   false,
	UseTTL:         true,
}

var LIST_CONVERSATIONS = Route{
	Action:         gmproto.ActionType_LIST_CONVERSATIONS,
	MessageType:    gmproto.MessageType_BUGLE_MESSAGE,
	BugleRoute:     gmproto.BugleRoute_DataEvent,
	ResponseStruct: &gmproto.Conversations{},
	UseSessionID:   false,
	UseTTL:         true,
}

var GET_CONVERSATION_TYPE = Route{
	Action:         gmproto.ActionType_GET_CONVERSATION_TYPE,
	MessageType:    gmproto.MessageType_BUGLE_MESSAGE,
	BugleRoute:     gmproto.BugleRoute_DataEvent,
	ResponseStruct: &gmproto.GetConversationTypeResponse{},
	UseSessionID:   false,
	UseTTL:         true,
}

var GET_PARTICIPANT_THUMBNAIL = Route{
	Action:         gmproto.ActionType_GET_PARTICIPANTS_THUMBNAIL,
	MessageType:    gmproto.MessageType_BUGLE_MESSAGE,
	BugleRoute:     gmproto.BugleRoute_DataEvent,
	ResponseStruct: &gmproto.ParticipantThumbnail{},
	UseSessionID:   false,
	UseTTL:         true,
}

var UPDATE_CONVERSATION = Route{
	Action:         gmproto.ActionType_UPDATE_CONVERSATION,
	MessageType:    gmproto.MessageType_BUGLE_MESSAGE,
	BugleRoute:     gmproto.BugleRoute_DataEvent,
	ResponseStruct: &gmproto.UpdateConversationResponse{},
	UseSessionID:   false,
	UseTTL:         true,
}

var TYPING_UPDATES = Route{
	Action:         gmproto.ActionType_TYPING_UPDATES,
	MessageType:    gmproto.MessageType_BUGLE_MESSAGE,
	BugleRoute:     gmproto.BugleRoute_DataEvent,
	ResponseStruct: nil,
	UseSessionID:   false,
	UseTTL:         true,
}
