package events

type BrowserActive struct {
	SessionID string
}

func NewBrowserActive(sessionID string) *BrowserActive {
	return &BrowserActive{
		SessionID: sessionID,
	}
}
