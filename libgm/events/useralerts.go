package events

type BrowserActive struct {
	SessionId string
}

func NewBrowserActive(sessionId string) *BrowserActive {
	return &BrowserActive{
		SessionId: sessionId,
	}
}

type Battery struct{}

func NewBattery() *Battery {
	return &Battery{}
}

type DataConnection struct{}

func NewDataConnection() *DataConnection {
	return &DataConnection{}
}
