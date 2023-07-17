package libgm

import (
	"encoding/base64"
)

func (s *SessionHandler) waitResponse(requestID string) chan *IncomingRPCMessage {
	ch := make(chan *IncomingRPCMessage, 1)
	s.responseWaitersLock.Lock()
	s.responseWaiters[requestID] = ch
	s.responseWaitersLock.Unlock()
	return ch
}

func (s *SessionHandler) cancelResponse(requestID string, ch chan *IncomingRPCMessage) {
	s.responseWaitersLock.Lock()
	close(ch)
	delete(s.responseWaiters, requestID)
	s.responseWaitersLock.Unlock()
}

func (s *SessionHandler) receiveResponse(msg *IncomingRPCMessage) bool {
	requestID := msg.Message.SessionID
	s.responseWaitersLock.Lock()
	ch, ok := s.responseWaiters[requestID]
	if !ok {
		s.responseWaitersLock.Unlock()
		return false
	}
	delete(s.responseWaiters, requestID)
	s.responseWaitersLock.Unlock()
	evt := s.client.Logger.Trace().
		Str("request_id", requestID)
	if evt.Enabled() {
		if msg.DecryptedData != nil {
			evt.Str("data", base64.StdEncoding.EncodeToString(msg.DecryptedData))
		}
		if msg.DecryptedMessage != nil {
			evt.Str("proto_name", string(msg.DecryptedMessage.ProtoReflect().Descriptor().FullName()))
		}
	}
	evt.Msg("Received response")
	ch <- msg
	return true
}
