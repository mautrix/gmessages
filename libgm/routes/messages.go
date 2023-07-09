package routes

import "go.mau.fi/mautrix-gmessages/libgm/binary"

var LIST_MESSAGES = Route{
	Action:         binary.ActionType_LIST_MESSAGES,
	MessageType:    binary.MessageType_BUGLE_MESSAGE,
	BugleRoute:     binary.BugleRoute_DataEvent,
	ResponseStruct: &binary.FetchMessagesResponse{},
	UseSessionID:   false,
	UseTTL:         true,
}

var SEND_MESSAGE = Route{
	Action:         binary.ActionType_SEND_MESSAGE,
	MessageType:    binary.MessageType_BUGLE_MESSAGE,
	BugleRoute:     binary.BugleRoute_DataEvent,
	ResponseStruct: &binary.SendMessageResponse{},
	UseSessionID:   false,
	UseTTL:         true,
}

var SEND_REACTION = Route{
	Action:         binary.ActionType_SEND_REACTION,
	MessageType:    binary.MessageType_BUGLE_MESSAGE,
	BugleRoute:     binary.BugleRoute_DataEvent,
	ResponseStruct: &binary.SendReactionResponse{},
	UseSessionID:   false,
	UseTTL:         true,
}

var DELETE_MESSAGE = Route{
	Action:         binary.ActionType_DELETE_MESSAGE,
	MessageType:    binary.MessageType_BUGLE_MESSAGE,
	BugleRoute:     binary.BugleRoute_DataEvent,
	ResponseStruct: &binary.DeleteMessageResponse{},
	UseSessionID:   false,
	UseTTL:         true,
}
