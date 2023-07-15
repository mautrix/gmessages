package libgm

import (
	"encoding/base64"
	"fmt"

	"go.mau.fi/mautrix-gmessages/libgm/pblite"
)

func (s *SessionHandler) waitResponse(requestID string) chan *pblite.Response {
	ch := make(chan *pblite.Response, 1)
	s.responseWaitersLock.Lock()
	// DEBUG
	if _, ok := s.responseWaiters[requestID]; ok {
		panic(fmt.Errorf("request %s already has a response waiter", requestID))
	}
	// END DEBUG
	s.responseWaiters[requestID] = ch
	s.responseWaitersLock.Unlock()
	return ch
}

func (s *SessionHandler) cancelResponse(requestID string, ch chan *pblite.Response) {
	s.responseWaitersLock.Lock()
	close(ch)
	delete(s.responseWaiters, requestID)
	s.responseWaitersLock.Unlock()
}

func (s *SessionHandler) receiveResponse(resp *pblite.Response) bool {
	s.responseWaitersLock.Lock()
	ch, ok := s.responseWaiters[resp.Data.RequestID]
	if !ok {
		s.responseWaitersLock.Unlock()
		return false
	}
	delete(s.responseWaiters, resp.Data.RequestID)
	s.responseWaitersLock.Unlock()
	evt := s.client.Logger.Trace().
		Str("request_id", resp.Data.RequestID)
	if evt.Enabled() && resp.Data.Decrypted != nil {
		evt.Str("proto_name", string(resp.Data.Decrypted.ProtoReflect().Descriptor().FullName())).
			Str("data", base64.StdEncoding.EncodeToString(resp.Data.RawDecrypted))
	}
	evt.Msg("Received response")
	ch <- resp
	return true
}
