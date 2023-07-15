package payload

import (
	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

// 202306220406
var ConfigMessage = &binary.ConfigVersion{
	V1: 2023,
	V2: 7,
	V3: 10,
	V4: 4,
	V5: 6,
}
var Network = "Bugle"
var BrowserDetailsMessage = &binary.BrowserDetails{
	UserAgent:   util.UserAgent,
	BrowserType: util.BrowserType,
	Os:          util.OS,
	SomeBool:    true,
}
