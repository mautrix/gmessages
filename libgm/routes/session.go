package routes

import "go.mau.fi/mautrix-gmessages/libgm/gmproto"

var IS_BUGLE_DEFAULT = Route{
	Action:         gmproto.ActionType_IS_BUGLE_DEFAULT,
	MessageType:    gmproto.MessageType_BUGLE_MESSAGE,
	BugleRoute:     gmproto.BugleRoute_DataEvent,
	ResponseStruct: &gmproto.IsBugleDefaultResponse{},
	UseSessionID:   false,
	UseTTL:         true,
}

var GET_UPDATES = Route{
	Action:         gmproto.ActionType_GET_UPDATES,
	MessageType:    gmproto.MessageType_BUGLE_MESSAGE,
	BugleRoute:     gmproto.BugleRoute_DataEvent,
	ResponseStruct: &gmproto.UpdateEvents{},
	UseSessionID:   true,
	UseTTL:         false,
}

var NOTIFY_DITTO_ACTIVITY = Route{
	Action:         gmproto.ActionType_NOTIFY_DITTO_ACTIVITY,
	MessageType:    gmproto.MessageType_BUGLE_MESSAGE,
	BugleRoute:     gmproto.BugleRoute_DataEvent,
	ResponseStruct: nil,
	UseSessionID:   false,
	UseTTL:         true,
}

var LIST_CONTACTS = Route{
	Action:         gmproto.ActionType_LIST_CONTACTS,
	MessageType:    gmproto.MessageType_BUGLE_MESSAGE,
	BugleRoute:     gmproto.BugleRoute_DataEvent,
	ResponseStruct: &gmproto.ListContactsResponse{},
	UseSessionID:   false,
	UseTTL:         true,
}

var LIST_TOP_CONTACTS = Route{
	Action:         gmproto.ActionType_LIST_TOP_CONTACTS,
	MessageType:    gmproto.MessageType_BUGLE_MESSAGE,
	BugleRoute:     gmproto.BugleRoute_DataEvent,
	ResponseStruct: &gmproto.ListTopContactsResponse{},
	UseSessionID:   false,
	UseTTL:         true,
}

var GET_OR_CREATE_CONVERSATION = Route{
	Action:         gmproto.ActionType_GET_OR_CREATE_CONVERSATION,
	MessageType:    gmproto.MessageType_BUGLE_MESSAGE,
	BugleRoute:     gmproto.BugleRoute_DataEvent,
	ResponseStruct: &gmproto.GetOrCreateConversationResponse{},
	UseSessionID:   false,
	UseTTL:         true,
}
