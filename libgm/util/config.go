package util

import (
	"go.mau.fi/mautrix-gmessages/libgm/binary"
)

var ConfigMessage = &binary.ConfigVersion{
	Year:  2023,
	Month: 7,
	Day:   10,
	V1:    4,
	V2:    6,
}
var Network = "Bugle"
var BrowserDetailsMessage = &binary.BrowserDetails{
	UserAgent:   UserAgent,
	BrowserType: binary.BrowserTypes_OTHER,
	Os:          "libgm",
	SomeBool:    true,
}
