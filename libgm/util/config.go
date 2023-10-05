package util

import (
	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
)

var ConfigMessage = &gmproto.ConfigVersion{
	Year:  2023,
	Month: 9,
	Day:   28,
	V1:    4,
	V2:    6,
}
var Network = "Bugle"
var BrowserDetailsMessage = &gmproto.BrowserDetails{
	UserAgent:   UserAgent,
	BrowserType: gmproto.BrowserType_OTHER,
	OS:          "libgm",
	DeviceType:  gmproto.DeviceType_TABLET,
}
