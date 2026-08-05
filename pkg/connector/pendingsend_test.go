package connector

import (
	"testing"

	"maunium.net/go/mautrix/bridgev2/networkid"

	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
)

func textPayload(texts ...string) []*gmproto.MessageInfo {
	parts := make([]*gmproto.MessageInfo, len(texts))
	for i, text := range texts {
		parts[i] = &gmproto.MessageInfo{
			Data: &gmproto.MessageInfo_MessageContent{
				MessageContent: &gmproto.MessageContent{Content: text},
			},
		}
	}
	return parts
}

func sendReq(convID string, texts ...string) *gmproto.SendMessageRequest {
	return &gmproto.SendMessageRequest{
		ConversationID: convID,
		MessagePayload: &gmproto.MessagePayload{
			ConversationID: convID,
			MessageInfo:    textPayload(texts...),
		},
	}
}

func echo(convID string, status gmproto.MessageStatusType, texts ...string) *gmproto.Message {
	return &gmproto.Message{
		ConversationID: convID,
		MessageStatus:  &gmproto.MessageStatus{Status: status},
		MessageInfo:    textPayload(texts...),
	}
}

func TestMatchPendingSend(t *testing.T) {
	gc := &GMClient{}
	gc.addPendingSend(networkid.TransactionID("tmp_1"), sendReq("6", "hello"))

	if got := gc.matchPendingSend(echo("6", gmproto.MessageStatusType_OUTGOING_SENDING, "hello")); got != "tmp_1" {
		t.Fatalf("expected tmp_1, got %q", got)
	}
	// The match is consumed, so later status events for the same message don't re-match.
	if got := gc.matchPendingSend(echo("6", gmproto.MessageStatusType_OUTGOING_COMPLETE, "hello")); got != "" {
		t.Fatalf("expected no second match, got %q", got)
	}
}

func TestMatchPendingSendIgnoresIncoming(t *testing.T) {
	gc := &GMClient{}
	gc.addPendingSend(networkid.TransactionID("tmp_1"), sendReq("6", "hello"))

	// An incoming message with identical text must never consume a pending send.
	if got := gc.matchPendingSend(echo("6", gmproto.MessageStatusType_INCOMING_COMPLETE, "hello")); got != "" {
		t.Fatalf("expected no match for incoming message, got %q", got)
	}
	if got := gc.matchPendingSend(echo("6", gmproto.MessageStatusType_OUTGOING_SENDING, "hello")); got != "tmp_1" {
		t.Fatalf("expected pending send to still be available, got %q", got)
	}
}

func TestMatchPendingSendWrongConversation(t *testing.T) {
	gc := &GMClient{}
	gc.addPendingSend(networkid.TransactionID("tmp_1"), sendReq("6", "hello"))

	if got := gc.matchPendingSend(echo("7", gmproto.MessageStatusType_OUTGOING_SENDING, "hello")); got != "" {
		t.Fatalf("expected no cross-conversation match, got %q", got)
	}
}

func TestMatchPendingSendDuplicateTextIsFIFO(t *testing.T) {
	gc := &GMClient{}
	gc.addPendingSend(networkid.TransactionID("tmp_1"), sendReq("6", "ok"))
	gc.addPendingSend(networkid.TransactionID("tmp_2"), sendReq("6", "ok"))

	if got := gc.matchPendingSend(echo("6", gmproto.MessageStatusType_OUTGOING_SENDING, "ok")); got != "tmp_1" {
		t.Fatalf("expected tmp_1 first, got %q", got)
	}
	if got := gc.matchPendingSend(echo("6", gmproto.MessageStatusType_OUTGOING_SENDING, "ok")); got != "tmp_2" {
		t.Fatalf("expected tmp_2 second, got %q", got)
	}
}

func TestRemovePendingSend(t *testing.T) {
	gc := &GMClient{}
	gc.addPendingSend(networkid.TransactionID("tmp_1"), sendReq("6", "hello"))
	gc.removePendingSend(networkid.TransactionID("tmp_1"))

	if got := gc.matchPendingSend(echo("6", gmproto.MessageStatusType_OUTGOING_SENDING, "hello")); got != "" {
		t.Fatalf("expected no match after removal, got %q", got)
	}
}

// The hash used for matching must be identical to the one getTextPart stores in message metadata,
// otherwise sends and echoes would never line up.
func TestHashMatchesGetTextPart(t *testing.T) {
	msg := echo("6", gmproto.MessageStatusType_OUTGOING_SENDING, "hello", "world")
	_, textHash := getTextPart(msg)
	if textHash == "" {
		t.Fatal("expected getTextPart to produce a hash")
	}
	if got := hashMessageText(msg.GetSubject(), msg.GetMessageInfo()); got != textHash {
		t.Fatalf("hash mismatch: getTextPart=%q hashMessageText=%q", textHash, got)
	}
}
