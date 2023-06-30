package events

type BROWSER_ACTIVE struct {
	SessionId string
}

func NewBrowserActive(sessionId string) *BROWSER_ACTIVE {
	return &BROWSER_ACTIVE{
		SessionId: sessionId,
	}
}

type BATTERY struct{}

func NewBattery() *BATTERY {
	return &BATTERY{}
}

type DATA_CONNECTION struct{}

func NewDataConnection() *DATA_CONNECTION {
	return &DATA_CONNECTION{}
}
