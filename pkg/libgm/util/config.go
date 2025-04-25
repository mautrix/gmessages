package util

import (
	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
)

var ConfigMessage = &gmproto.ConfigVersion{
	Year:  2025,
	Month: 4,
	Day:   24,
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
