package util

import (
	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
)

var ConfigMessage = &gmproto.ConfigVersion{
	Year:  2026,
	Month: 3,
	Day:   18,
	V1:    4,
	V2:    6,
}

const QRNetwork = "Bugle"
const GoogleNetwork = "GDitto"

var BrowserDetailsMessage = &gmproto.BrowserDetails{
	UserAgent:   UserAgent,
	BrowserType: gmproto.BrowserType_OTHER,
	OS:          "libgm",
	DeviceType:  gmproto.DeviceType_TABLET,
}
