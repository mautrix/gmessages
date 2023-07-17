package routes

import "go.mau.fi/mautrix-gmessages/libgm/gmproto"

var LIST_MESSAGES = Route{
	Action:         gmproto.ActionType_LIST_MESSAGES,
	MessageType:    gmproto.MessageType_BUGLE_MESSAGE,
	BugleRoute:     gmproto.BugleRoute_DataEvent,
	ResponseStruct: &gmproto.FetchMessagesResponse{},
	UseSessionID:   false,
	UseTTL:         true,
}

var SEND_MESSAGE = Route{
	Action:         gmproto.ActionType_SEND_MESSAGE,
	MessageType:    gmproto.MessageType_BUGLE_MESSAGE,
	BugleRoute:     gmproto.BugleRoute_DataEvent,
	ResponseStruct: &gmproto.SendMessageResponse{},
	UseSessionID:   false,
	UseTTL:         true,
}

var SEND_REACTION = Route{
	Action:         gmproto.ActionType_SEND_REACTION,
	MessageType:    gmproto.MessageType_BUGLE_MESSAGE,
	BugleRoute:     gmproto.BugleRoute_DataEvent,
	ResponseStruct: &gmproto.SendReactionResponse{},
	UseSessionID:   false,
	UseTTL:         true,
}

var DELETE_MESSAGE = Route{
	Action:         gmproto.ActionType_DELETE_MESSAGE,
	MessageType:    gmproto.MessageType_BUGLE_MESSAGE,
	BugleRoute:     gmproto.BugleRoute_DataEvent,
	ResponseStruct: &gmproto.DeleteMessageResponse{},
	UseSessionID:   false,
	UseTTL:         true,
}

var MESSAGE_READ = Route{
	Action:         gmproto.ActionType_MESSAGE_READ,
	MessageType:    gmproto.MessageType_BUGLE_MESSAGE,
	BugleRoute:     gmproto.BugleRoute_DataEvent,
	ResponseStruct: nil,
	UseSessionID:   false,
	UseTTL:         true,
}
