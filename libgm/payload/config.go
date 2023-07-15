package payload

import (
	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/util"
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
	UserAgent:   util.UserAgent,
	BrowserType: binary.BrowserTypes_OTHER,
	Os:          "libgm",
	SomeBool:    true,
}
