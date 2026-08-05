// mautrix-gmessages - A Matrix-Google Messages puppeting bridge.
// Copyright (C) 2024 Tulir Asokan
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package connector

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"maunium.net/go/mautrix/bridgev2/networkid"

	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
)

// Google Messages normally echoes the tmpID that was passed in SendMessageRequest back in the
// message events for that message, which is how outgoing messages are matched to the Matrix event
// that caused them. However, the server sometimes stops filling the field entirely, which makes
// every message sent from Matrix come back as if it was a new message sent from the phone, causing
// the bridge to duplicate it into the Matrix room.
//
// To recover from that, recently sent messages are remembered here and matched against incoming
// echoes by conversation and content when the echo has no tmpID.
const (
	// pendingSendTTL is how long a sent message can be matched against a tmpID-less echo.
	pendingSendTTL = 60 * time.Second
	// maxPendingSends is a hard cap on remembered sends to bound memory usage if echoes stop arriving.
	maxPendingSends = 100
)

type pendingSend struct {
	txnID    networkid.TransactionID
	convID   string
	textHash string
	sentAt   time.Time
}

// hashMessageText hashes the text content of a message. The same hash is computed for outgoing
// requests and incoming messages, so it can be used to match the two together.
func hashMessageText(subject string, parts []*gmproto.MessageInfo) string {
	hasher := sha256.New()
	hasher.Write([]byte(subject))
	hasher.Write([]byte{0x00})
	for _, part := range parts {
		data, ok := part.Data.(*gmproto.MessageInfo_MessageContent)
		if !ok {
			continue
		}
		hasher.Write([]byte(data.MessageContent.GetContent()))
		hasher.Write([]byte{0x00})
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

// expireOldPendingSends drops entries that are too old to be matched anymore.
// The caller must hold pendingSendsLock.
func (gc *GMClient) expireOldPendingSends(now time.Time) {
	firstValid := 0
	for firstValid < len(gc.pendingSends) && now.Sub(gc.pendingSends[firstValid].sentAt) > pendingSendTTL {
		firstValid++
	}
	gc.pendingSends = gc.pendingSends[firstValid:]
	if len(gc.pendingSends) > maxPendingSends {
		gc.pendingSends = gc.pendingSends[len(gc.pendingSends)-maxPendingSends:]
	}
}

// addPendingSend remembers a message that was just sent, so that an echo without a tmpID can still
// be matched back to the Matrix event that caused it.
func (gc *GMClient) addPendingSend(txnID networkid.TransactionID, req *gmproto.SendMessageRequest) {
	entry := &pendingSend{
		txnID:    txnID,
		convID:   req.GetConversationID(),
		textHash: hashMessageText("", req.GetMessagePayload().GetMessageInfo()),
		sentAt:   time.Now(),
	}
	gc.pendingSendsLock.Lock()
	defer gc.pendingSendsLock.Unlock()
	gc.expireOldPendingSends(entry.sentAt)
	gc.pendingSends = append(gc.pendingSends, entry)
}

// removePendingSend forgets a message that turned out not to have been sent.
func (gc *GMClient) removePendingSend(txnID networkid.TransactionID) {
	gc.pendingSendsLock.Lock()
	defer gc.pendingSendsLock.Unlock()
	for i, entry := range gc.pendingSends {
		if entry.txnID == txnID {
			gc.pendingSends = append(gc.pendingSends[:i], gc.pendingSends[i+1:]...)
			return
		}
	}
}

// matchPendingSend finds the transaction ID of the message that the given echo is most likely a
// remote echo of. It returns an empty string if the message isn't an outgoing one or if there's no
// recently sent message with the same content in the same conversation.
//
// A match is consumed, as only the first echo for a message needs to resolve the pending event.
func (gc *GMClient) matchPendingSend(msg *gmproto.Message) networkid.TransactionID {
	status := msg.GetMessageStatus().GetStatus()
	// Statuses between 1 and 99 are outgoing types. Anything else was not sent by us.
	if status < 1 || status >= 100 {
		return ""
	}
	textHash := hashMessageText(msg.GetSubject(), msg.GetMessageInfo())
	gc.pendingSendsLock.Lock()
	defer gc.pendingSendsLock.Unlock()
	gc.expireOldPendingSends(time.Now())
	// The oldest match is used, as messages are echoed in the order they were sent.
	for i, entry := range gc.pendingSends {
		if entry.convID != msg.GetConversationID() || entry.textHash != textHash {
			continue
		}
		gc.pendingSends = append(gc.pendingSends[:i], gc.pendingSends[i+1:]...)
		return entry.txnID
	}
	return ""
}
