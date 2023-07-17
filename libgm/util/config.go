package util

import (
	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
)

var ConfigMessage = &gmproto.ConfigVersion{
	Year:  2023,
	Month: 7,
	Day:   10,
	V1:    4,
	V2:    6,
}
var Network = "Bugle"
var BrowserDetailsMessage = &gmproto.BrowserDetails{
	UserAgent:   UserAgent,
	BrowserType: gmproto.BrowserTypes_OTHER,
	OS:          "libgm",
	SomeBool:    true,
}
