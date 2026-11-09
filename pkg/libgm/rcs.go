package libgm

import (
	"bufio"
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.mau.fi/util/pblite"
	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-gmessages/pkg/libgm/crypto"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/util"
)

const RCSNetwork = "RCS"

const (
	RCSMessageTypeMessage    = 36
	RCSMessageTypeReflection = 45
)

// RCSSession is a secondary-device registration on Google's RCS backend (Tachyon "jms"),
// which lets the client send and receive RCS without relaying through the phone.
type RCSSession struct {
	BaseURL        string          `json:"base_url"`
	Self           *gmproto.Device `json:"self"`
	Link           *gmproto.Device `json:"link"`
	Key            *crypto.JWK     `json:"key"`
	Token          []byte          `json:"token"`
	ReflectionKey  []byte          `json:"reflection_key,omitempty"`
	TokenExpiry    time.Time       `json:"token_expiry"`
	RegistrationID string          `json:"registration_id"`
	DeviceID       string          `json:"device_id"`
}

// RCSTachyonURL converts the SIM's rcsAddress2 (e.g. "instantmessaging-pa-jms-preprod-us.googleapis.com:443")
// into the browser-facing base URL the web client uses.
func RCSTachyonURL(simAddress string) string {
	if simAddress == "" {
		return ""
	}
	if !strings.HasPrefix(simAddress, "https://") {
		simAddress = "https://" + simAddress
	}
	simAddress = strings.Replace(simAddress, "googleapis.com", "clients6.google.com", 1)
	simAddress = strings.Replace(simAddress, "-preprod", "", 1)
	simAddress = strings.Replace(simAddress, ":443", "", 1)
	return strings.TrimSuffix(simAddress, "/")
}

func (s *RCSSession) rpcURL(service, method string) string {
	return fmt.Sprintf("%s/$rpc/google.internal.communications.instantmessaging.v1.%s/%s", s.BaseURL, service, method)
}

func (s *RCSSession) authMessage(withToken bool) *gmproto.AuthMessage {
	am := &gmproto.AuthMessage{
		RequestID:     uuid.NewString(),
		Network:       RCSNetwork,
		ConfigVersion: util.ConfigMessage,
	}
	if withToken {
		am.TachyonAuthToken = s.Token
	}
	return am
}

func (s *RCSSession) registrationInfo() (*gmproto.RCSRegistrationInfo, error) {
	pub, err := s.Key.GetPublicKey()
	if err != nil {
		return nil, err
	}
	spki, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, err
	}
	return &gmproto.RCSRegistrationInfo{
		DeviceID:     &gmproto.RCSDeviceID{Type: 3, DeviceID: s.DeviceID},
		PublicKey:    &gmproto.RCSPublicKey{Type: 2, Key: spki},
		Locale:       "en",
		ClientFlags:  &gmproto.RCSIntList{Values: []int32{6}},
		Capabilities: &gmproto.RCSIntList{Values: []int32{4, 5}},
	}, nil
}

// NewRCSSession prepares an unregistered session for the given SIM tachyon address and own number.
func (c *Client) NewRCSSession(simAddress, ownNumber string) (*RCSSession, error) {
	if c.AuthData.Browser == nil || c.AuthData.Browser.GetSourceID() == "" {
		return nil, errors.New("RCS secondary registration needs a Google account login")
	}
	return &RCSSession{
		BaseURL:  RCSTachyonURL(simAddress),
		Self:     &gmproto.Device{UserID: 1, SourceID: ownNumber, Network: RCSNetwork},
		Link:     &gmproto.Device{UserID: 16, SourceID: c.AuthData.Browser.GetSourceID(), Network: RCSNetwork},
		Key:      crypto.GenerateECDSAKey(),
		DeviceID: fmt.Sprintf("messages-web-%x", c.AuthData.SessionID[:]),
	}, nil
}

func (c *Client) RCSListIdentities(ctx context.Context, s *RCSSession) (*gmproto.ListIdentitiesResponse, error) {
	return typedHTTPResponse[*gmproto.ListIdentitiesResponse](c.makeProtobufHTTPRequestContext(ctx, s.rpcURL("Registration", "ListIdentities"), &gmproto.ListIdentitiesRequest{
		AuthMessage: s.authMessage(false),
		LinkID:      s.Link,
	}, ContentTypePBLite, false))
}

func (c *Client) RCSSignIn(ctx context.Context, s *RCSSession) error {
	reg, err := s.registrationInfo()
	if err != nil {
		return err
	}
	resp, err := typedHTTPResponse[*gmproto.SignInSecondaryResponse](c.makeProtobufHTTPRequestContext(ctx, s.rpcURL("Registration", "SignInSecondary"), &gmproto.SignInSecondaryRequest{
		AuthMessage:  s.authMessage(false),
		LinkID:       s.Link,
		SelfID:       s.Self,
		Registration: reg,
	}, ContentTypePBLite, false))
	if err != nil {
		return err
	}
	token := resp.GetResult().GetTokenData()
	if len(token.GetTachyonAuthToken()) == 0 {
		return fmt.Errorf("no token in SignInSecondary response")
	}
	s.Token = token.GetTachyonAuthToken()
	s.TokenExpiry = time.Now().Add(time.Duration(token.GetTTL()) * time.Microsecond)
	s.RegistrationID = string(resp.GetResult().GetRegistrationID())
	return nil
}

func (c *Client) RCSRefresh(ctx context.Context, s *RCSSession) error {
	reg, err := s.registrationInfo()
	if err != nil {
		return err
	}
	am := s.authMessage(true)
	timestamp := time.Now().UnixMilli() * 1000
	signBytes := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", am.RequestID, timestamp)))
	privKey, err := s.Key.GetPrivateKey()
	if err != nil {
		return err
	}
	sig, err := ecdsa.SignASN1(rand.Reader, privKey, signBytes[:])
	if err != nil {
		return err
	}
	resp, err := typedHTTPResponse[*gmproto.RegisterRefreshResponse](c.makeProtobufHTTPRequestContext(ctx, s.rpcURL("Registration", "RegisterRefresh"), &gmproto.RCSRegisterRefreshRequest{
		AuthMessage:   am,
		CurrDevice:    s.Self,
		UnixTimestamp: timestamp,
		Signature:     sig,
		Registration:  reg,
		MessageType:   2,
	}, ContentTypePBLite, false))
	if err != nil {
		return err
	}
	if len(resp.GetTokenData().GetTachyonAuthToken()) == 0 {
		return fmt.Errorf("no token in refresh response")
	}
	s.Token = resp.GetTokenData().GetTachyonAuthToken()
	s.TokenExpiry = time.Now().Add(time.Duration(resp.GetTokenData().GetTTL()) * time.Microsecond)
	return nil
}

func (c *Client) RCSLookupRegistered(ctx context.Context, s *RCSSession, numbers ...string) (*gmproto.LookupRegisteredResponse, error) {
	ids := make([]*gmproto.Device, len(numbers))
	for i, num := range numbers {
		ids[i] = &gmproto.Device{UserID: 1, SourceID: num, Network: RCSNetwork}
	}
	return typedHTTPResponse[*gmproto.LookupRegisteredResponse](c.makeProtobufHTTPRequestContext(ctx, s.rpcURL("Registration", "LookupRegistered"), &gmproto.LookupRegisteredRequest{
		AuthMessage:  s.authMessage(true),
		Ids:          ids,
		UnknownBool7: true,
	}, ContentTypePBLite, false))
}

// RCSSend sends a raw CPIM-style RCS message to a phone number (type 1) or group (type 2).
func (c *Client) RCSSend(ctx context.Context, s *RCSSession, dest *gmproto.Device, messageID string, content *gmproto.RCSContent) (*gmproto.TachyonSendMessageResponse, error) {
	payload, err := proto.Marshal(content)
	if err != nil {
		return nil, err
	}
	if messageID == "" {
		messageID = uuid.NewString()
	}
	return typedHTTPResponse[*gmproto.TachyonSendMessageResponse](c.makeProtobufHTTPRequestContext(ctx, s.rpcURL("Messaging", "SendMessage"), &gmproto.TachyonSendMessageRequest{
		Dest: dest,
		Message: &gmproto.TachyonInboxMessage{
			MessageID:   messageID,
			MessageType: RCSMessageTypeMessage,
			Payload:     payload,
		},
		AuthMessage: s.authMessage(true),
	}, ContentTypePBLite, false))
}

func RCSTextContent(text string) *gmproto.RCSContent {
	return &gmproto.RCSContent{Parts: []*gmproto.RCSContentPart{{
		ContentType: "text/plain;charset=UTF-8",
		Data:        []byte(text),
	}}}
}

func (c *Client) RCSAck(ctx context.Context, s *RCSSession, msgs ...*gmproto.TachyonInboxMessage) error {
	acks := make([]*gmproto.AckMessageRequest_Message, len(msgs))
	for i, msg := range msgs {
		acks[i] = &gmproto.AckMessageRequest_Message{RequestID: msg.GetMessageID(), Device: msg.GetReceiver()}
	}
	_, err := typedHTTPResponse[*gmproto.OutgoingRPCResponse](c.makeProtobufHTTPRequestContext(ctx, s.rpcURL("Messaging", "AckMessages"), &gmproto.AckMessageRequest{
		AuthData: s.authMessage(true),
		EmptyArr: &gmproto.EmptyArr{},
		Acks:     acks,
	}, ContentTypePBLite, false))
	return err
}

// RCSReceive opens one ReceiveMessages long poll on the RCS backend and calls handler for every inbox
// message until the stream ends or ctx is cancelled.
func (c *Client) RCSReceive(ctx context.Context, s *RCSSession, handler func(*gmproto.TachyonInboxMessage)) error {
	resp, err := c.makeProtobufHTTPRequestContext(ctx, s.rpcURL("Messaging", "ReceiveMessages"), &gmproto.TachyonReceiveMessagesRequest{
		AuthMessage: s.authMessage(true),
		Unknown: &gmproto.TachyonReceiveMessagesRequest_UnknownEmptyObject2{
			Unknown: &gmproto.TachyonReceiveMessagesRequest_UnknownEmptyObject1{},
		},
	}, ContentTypePBLite, true)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
	}
	reader := bufio.NewReader(resp.Body)
	opening := make([]byte, 2)
	if _, err = io.ReadFull(reader, opening); err != nil {
		return err
	} else if string(opening) != "[[" {
		return fmt.Errorf("unexpected stream opening %q", opening)
	}
	var acc []byte
	buf := make([]byte, 32*1024)
	for {
		n, err := reader.Read(buf)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		chunk := buf[:n]
		if len(acc) == 0 {
			if string(chunk) == "]]" {
				continue
			}
			chunk = bytes.TrimPrefix(chunk, []byte{','})
		}
		acc = append(acc, chunk...)
		if !json.Valid(acc) {
			continue
		}
		block := acc
		acc = nil
		var item gmproto.TachyonStreamItem
		if err = pblite.Unmarshal(block, &item); err != nil {
			c.Logger.Debug().Err(err).Str("data", string(block)).Msg("Failed to parse RCS stream item")
			continue
		}
		if item.GetData() != nil {
			handler(item.GetData())
		}
	}
}

// RCSReflect tells the user's other devices (most importantly the phone) about a message that was sent
// directly, so it shows up in the phone's own history. The key comes from the phone's RCS settings.
func (c *Client) RCSReflect(ctx context.Context, s *RCSSession, messageID string, dest *gmproto.Device, content *gmproto.RCSContent) error {
	return c.RCSReflectTo(ctx, s, s.Self, messageID, dest, content)
}

func (c *Client) RCSReflectTo(ctx context.Context, s *RCSSession, self *gmproto.Device, messageID string, dest *gmproto.Device, content *gmproto.RCSContent) error {
	if len(s.ReflectionKey) == 0 {
		return errors.New("no reflection key")
	}
	plaintext, err := proto.Marshal(&gmproto.RCSReflectionPlaintext{Content: content})
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(s.ReflectionKey)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return err
	}
	envelope, err := proto.Marshal(&gmproto.RCSReflectionEnvelope{
		EncryptedContent: gcm.Seal(nonce, nonce, plaintext, nil),
		Destination:      dest,
	})
	if err != nil {
		return err
	}
	_, err = typedHTTPResponse[*gmproto.TachyonSendMessageResponse](c.makeProtobufHTTPRequestContext(ctx, s.rpcURL("Messaging", "SendMessage"), &gmproto.TachyonSendMessageRequest{
		Dest: self,
		Message: &gmproto.TachyonInboxMessage{
			MessageID:   messageID,
			MessageType: RCSMessageTypeReflection,
			Payload:     envelope,
		},
		AuthMessage: s.authMessage(true),
	}, ContentTypePBLite, false))
	return err
}
