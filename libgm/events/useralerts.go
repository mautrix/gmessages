package events

type BrowserActive struct {
	SessionId string
}

func NewBrowserActive(sessionId string) *BrowserActive {
	return &BrowserActive{
		SessionId: sessionId,
	}
}

type MOBILE_BATTERY_RESTORED struct{}

func NewMobileBatteryRestored() *MOBILE_BATTERY_RESTORED {
	return &MOBILE_BATTERY_RESTORED{}
}

type MOBILE_BATTERY_LOW struct{}

func NewMobileBatteryLow() *MOBILE_BATTERY_LOW {
	return &MOBILE_BATTERY_LOW{}
}

type MOBILE_DATA_CONNECTION struct{}

func NewMobileDataConnection() *MOBILE_DATA_CONNECTION {
	return &MOBILE_DATA_CONNECTION{}
}

type MOBILE_WIFI_CONNECTION struct{}

func NewMobileWifiConnection() *MOBILE_WIFI_CONNECTION {
	return &MOBILE_WIFI_CONNECTION{}
}
