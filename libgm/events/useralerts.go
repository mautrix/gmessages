package events

type BrowserActive struct {
	SessionId string
}

func NewBrowserActive(sessionId string) *BrowserActive {
	return &BrowserActive{
		SessionId: sessionId,
	}
}
