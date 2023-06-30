package events

type BrowserActive struct {
	SessionID string
}

func NewBrowserActive(sessionID string) *BrowserActive {
	return &BrowserActive{
		SessionID: sessionID,
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
