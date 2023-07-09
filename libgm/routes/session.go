package routes

import "go.mau.fi/mautrix-gmessages/libgm/binary"

var IS_BUGLE_DEFAULT = Route{
	Action:         binary.ActionType_IS_BUGLE_DEFAULT,
	MessageType:    binary.MessageType_BUGLE_MESSAGE,
	BugleRoute:     binary.BugleRoute_DataEvent,
	ResponseStruct: &binary.IsBugleDefaultResponse{},
	UseSessionID:   false,
	UseTTL:         true,
}

var GET_UPDATES = Route{
	Action:         binary.ActionType_GET_UPDATES,
	MessageType:    binary.MessageType_BUGLE_MESSAGE,
	BugleRoute:     binary.BugleRoute_DataEvent,
	ResponseStruct: &binary.UpdateEvents{},
	UseSessionID:   true,
	UseTTL:         false,
}

var NOTIFY_DITTO_ACTIVITY = Route{
	Action:         binary.ActionType_NOTIFY_DITTO_ACTIVITY,
	MessageType:    binary.MessageType_BUGLE_MESSAGE,
	BugleRoute:     binary.BugleRoute_DataEvent,
	ResponseStruct: nil,
	UseSessionID:   false,
	UseTTL:         true,
}
