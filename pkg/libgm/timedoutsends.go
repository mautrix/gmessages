package libgm

import (
	"time"
)

// timedOutSendMaxAge is how long to wait for a late response to a message send that timed out.
const timedOutSendMaxAge = 1 * time.Hour

type timedOutSend struct {
	tmpID      string
	timedOutAt time.Time
}

func (c *Client) trackTimedOutSend(requestID, tmpID string) {
	c.timedOutSendsLock.Lock()
	defer c.timedOutSendsLock.Unlock()
	for id, send := range c.timedOutSends {
		if time.Since(send.timedOutAt) > timedOutSendMaxAge {
			delete(c.timedOutSends, id)
		}
	}
	c.timedOutSends[requestID] = timedOutSend{tmpID: tmpID, timedOutAt: time.Now()}
}

func (c *Client) popTimedOutSend(requestID string) (string, bool) {
	c.timedOutSendsLock.Lock()
	defer c.timedOutSendsLock.Unlock()
	send, ok := c.timedOutSends[requestID]
	if ok {
		delete(c.timedOutSends, requestID)
	}
	return send.tmpID, ok
}
