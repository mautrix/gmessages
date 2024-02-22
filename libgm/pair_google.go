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

package libgm

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/big"
	"time"

	"github.com/google/uuid"
	"go.mau.fi/util/random"
	"golang.org/x/crypto/hkdf"
	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-gmessages/libgm/events"
	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

func (c *Client) handleGaiaPairingEvent(msg *IncomingRPCMessage) {
	c.Logger.Debug().Any("evt", msg.Gaia).Msg("Gaia event")
}

func (c *Client) baseSignInGaiaPayload() *gmproto.SignInGaiaRequest {
	return &gmproto.SignInGaiaRequest{
		AuthMessage: &gmproto.AuthMessage{
			RequestID:     uuid.NewString(),
			Network:       util.GoogleNetwork,
			ConfigVersion: util.ConfigMessage,
		},
		Inner: &gmproto.SignInGaiaRequest_Inner{
			DeviceID: &gmproto.SignInGaiaRequest_Inner_DeviceID{
				UnknownInt1: 3,
				DeviceID:    fmt.Sprintf("messages-web-%x", c.AuthData.SessionID[:]),
			},
		},
		Network: util.GoogleNetwork,
	}
}

func (c *Client) signInGaiaInitial() (*gmproto.SignInGaiaResponse, error) {
	payload := c.baseSignInGaiaPayload()
	payload.UnknownInt3 = 1
	return typedHTTPResponse[*gmproto.SignInGaiaResponse](
		c.makeProtobufHTTPRequest(util.SignInGaiaURL, payload, ContentTypePBLite),
	)
}

func (c *Client) signInGaiaGetToken() (*gmproto.SignInGaiaResponse, error) {
	key, err := x509.MarshalPKIXPublicKey(c.AuthData.RefreshKey.GetPublicKey())
	if err != nil {
		return nil, err
	}

	payload := c.baseSignInGaiaPayload()
	payload.Inner.SomeData = &gmproto.SignInGaiaRequest_Inner_Data{
		SomeData: key,
	}
	resp, err := typedHTTPResponse[*gmproto.SignInGaiaResponse](
		c.makeProtobufHTTPRequest(util.SignInGaiaURL, payload, ContentTypePBLite),
	)
	if err != nil {
		return nil, err
	}
	c.updateTachyonAuthToken(resp.GetTokenData())
	c.AuthData.Mobile = resp.GetDeviceData().GetDeviceWrapper().GetDevice()
	c.AuthData.Browser = resp.GetDeviceData().GetDeviceWrapper().GetDevice()
	return resp, nil
}

type PairingSession struct {
	UUID          uuid.UUID
	Start         time.Time
	DestRegID     uuid.UUID
	PairingKeyDSA *ecdsa.PrivateKey
	InitPayload   []byte
	NextKey       []byte
}

func NewPairingSession(destRegID uuid.UUID) PairingSession {
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	return PairingSession{
		UUID:          uuid.New(),
		Start:         time.Now(),
		DestRegID:     destRegID,
		PairingKeyDSA: ec,
	}
}

func (ps *PairingSession) PreparePayloads() ([]byte, []byte, error) {
	pubKey := &gmproto.GenericPublicKey{
		Type: gmproto.PublicKeyType_EC_P256,
		PublicKey: &gmproto.GenericPublicKey_EcP256PublicKey{
			EcP256PublicKey: &gmproto.EcP256PublicKey{
				X: make([]byte, 33),
				Y: make([]byte, 33),
			},
		},
	}
	ps.PairingKeyDSA.X.FillBytes(pubKey.GetEcP256PublicKey().GetX()[1:])
	ps.PairingKeyDSA.Y.FillBytes(pubKey.GetEcP256PublicKey().GetY()[1:])

	finishPayload, err := proto.Marshal(&gmproto.Ukey2ClientFinished{
		PublicKey: pubKey,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal finish payload: %w", err)
	}
	finish, err := proto.Marshal(&gmproto.Ukey2Message{
		MessageType: gmproto.Ukey2Message_CLIENT_FINISH,
		MessageData: finishPayload,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal finish message: %w", err)
	}

	keyCommitment := sha512.Sum512(finish)
	initPayload, err := proto.Marshal(&gmproto.Ukey2ClientInit{
		Version: 1,
		Random:  random.Bytes(32),
		CipherCommitments: []*gmproto.Ukey2ClientInit_CipherCommitment{{
			HandshakeCipher: gmproto.Ukey2HandshakeCipher_P256_SHA512,
			Commitment:      keyCommitment[:],
		}},
		NextProtocol: "AES_256_CBC-HMAC_SHA256",
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal init payload: %w", err)
	}
	init, err := proto.Marshal(&gmproto.Ukey2Message{
		MessageType: gmproto.Ukey2Message_CLIENT_INIT,
		MessageData: initPayload,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal init message: %w", err)
	}
	ps.InitPayload = init
	return init, finish, nil
}

func doHKDF(key []byte, salt, info []byte) []byte {
	h := hkdf.New(sha256.New, key, salt, info)
	out := make([]byte, 32)
	_, err := io.ReadFull(h, out)
	if err != nil {
		panic(err)
	}
	return out
}

var encryptionKeyInfo = []byte{130, 170, 85, 160, 211, 151, 248, 131, 70, 202, 28, 238, 141, 57, 9, 185, 95, 19, 250, 125, 235, 29, 74, 179, 131, 118, 184, 37, 109, 168, 85, 16}
var pairingEmojis = []string{"😁", "😅", "🤣", "🫠", "🥰", "😇", "🤩", "😘", "😜", "🤗", "🤔", "🤐", "😴", "🥶", "🤯", "🤠", "🥳", "🥸", "😎", "🤓", "🧐", "🥹", "😭", "😱", "😖", "🥱", "😮\u200d💨", "🤡", "💩", "👻", "👽", "🤖", "😻", "💌", "💘", "💕", "❤", "💢", "💥", "💫", "💬", "🗯", "💤", "👋", "🙌", "🙏", "✍", "🦶", "👂", "🧠", "🦴", "👀", "🧑", "🧚", "🧍", "👣", "🐵", "🐶", "🐺", "🦊", "🦁", "🐯", "🦓", "🦄", "🐑", "🐮", "🐷", "🐿", "🐰", "🦇", "🐻", "🐨", "🐼", "🦥", "🐾", "🐔", "🐥", "🐦", "🕊", "🦆", "🦉", "🪶", "🦩", "🐸", "🐢", "🦎", "🐍", "🐳", "🐬", "🦭", "🐠", "🐡", "🦈", "🪸", "🐌", "🦋", "🐛", "🐝", "🐞", "🪱", "💐", "🌸", "🌹", "🌻", "🌱", "🌲", "🌴", "🌵", "🌾", "☘", "🍁", "🍂", "🍄", "🪺", "🍇", "🍈", "🍉", "🍋", "🍌", "🍍", "🍎", "🍐", "🍒", "🍓", "🥝", "🥥", "🥑", "🥕", "🌽", "🌶", "🫑", "🥦", "🥜", "🍞", "🥐", "🥨", "🧀", "🍗", "🍔", "🍟", "🍕", "🌭", "🌮", "🥗", "🥣", "🍿", "🦀", "🦑", "🍦", "🍩", "🍪", "🍫", "🍰", "🍬", "🍭", "☕", "🫖", "🍹", "🥤", "🧊", "🥢", "🍽", "🥄", "🧭", "🏔", "🌋", "🏕", "🏖", "🪵", "🏗", "🏡", "🏰", "🛝", "🚂", "🛵", "🛴", "🛼", "🚥", "⚓", "🛟", "⛵", "✈", "🚀", "🛸", "🧳", "⏰", "🌙", "🌡", "🌞", "🪐", "🌠", "🌧", "🌀", "🌈", "☂", "⚡", "❄", "⛄", "🔥", "🎇", "🧨", "✨", "🎈", "🎉", "🎁", "🏆", "🏅", "⚽", "⚾", "🏀", "🏐", "🏈", "🎾", "🎳", "🏓", "🥊", "⛳", "⛸", "🎯", "🪁", "🔮", "🎮", "🧩", "🧸", "🪩", "🖼", "🎨", "🧵", "🧶", "🦺", "🧣", "🧤", "🧦", "🎒", "🩴", "👟", "👑", "👒", "🎩", "🧢", "💎", "🔔", "🎤", "📻", "🎷", "🪗", "🎸", "🎺", "🎻", "🥁", "📺", "🔋", "💻", "💿", "☎", "🕯", "💡", "📖", "📚", "📬", "✏", "✒", "🖌", "🖍", "📝", "💼", "📋", "📌", "📎", "🔑", "🔧", "🧲", "🪜", "🧬", "🔭", "🩹", "🩺", "🪞", "🛋", "🪑", "🛁", "🧹", "🧺", "🔱", "🏁", "🐪", "🐘", "🦃", "🍞", "🍜", "🍠", "🚘", "🤿", "🃏", "👕", "📸", "🏷", "✂", "🧪", "🚪", "🧴", "🧻", "🪣", "🧽", "🚸"}

func (ps *PairingSession) ProcessServerInit(msg *gmproto.GaiaPairingResponseContainer) (string, error) {
	var ukeyMessage gmproto.Ukey2Message
	err := proto.Unmarshal(msg.GetData(), &ukeyMessage)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal server init message: %w", err)
	} else if ukeyMessage.GetMessageType() != gmproto.Ukey2Message_SERVER_INIT {
		return "", fmt.Errorf("unexpected message type: %v", ukeyMessage.GetMessageType())
	}
	var serverInit gmproto.Ukey2ServerInit
	err = proto.Unmarshal(ukeyMessage.GetMessageData(), &serverInit)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal server init payload: %w", err)
	} else if serverInit.GetVersion() != 1 {
		return "", fmt.Errorf("unexpected server init version: %d", serverInit.GetVersion())
	} else if serverInit.GetHandshakeCipher() != gmproto.Ukey2HandshakeCipher_P256_SHA512 {
		return "", fmt.Errorf("unexpected handshake cipher: %v", serverInit.GetHandshakeCipher())
	} else if len(serverInit.GetRandom()) != 32 {
		return "", fmt.Errorf("unexpected random length %d", len(serverInit.GetRandom()))
	}
	serverKeyData := serverInit.GetPublicKey().GetEcP256PublicKey()
	x, y := serverKeyData.GetX(), serverKeyData.GetY()
	if len(x) == 33 {
		if x[0] != 0 {
			return "", fmt.Errorf("server key x coordinate has unexpected prefix: %d", x[0])
		}
		x = x[1:]
	}
	if len(y) == 33 {
		if y[0] != 0 {
			return "", fmt.Errorf("server key y coordinate has unexpected prefix: %d", y[0])
		}
		y = y[1:]
	}
	serverPairingKeyDSA := &ecdsa.PublicKey{
		Curve: elliptic.P256(),
		X:     big.NewInt(0).SetBytes(x),
		Y:     big.NewInt(0).SetBytes(y),
	}
	serverPairingKeyDH, err := serverPairingKeyDSA.ECDH()
	if err != nil {
		return "", fmt.Errorf("invalid server key: %w", err)
	}
	ourPairingKeyDH, err := ps.PairingKeyDSA.ECDH()
	if err != nil {
		return "", fmt.Errorf("invalid our key: %w", err)
	}
	diffieHellman, err := ourPairingKeyDH.ECDH(serverPairingKeyDH)
	if err != nil {
		return "", fmt.Errorf("failed to calculate shared secret: %w", err)
	}
	sharedSecret := sha256.Sum256(diffieHellman)
	authInfo := append(ps.InitPayload, msg.GetData()...)
	ukeyV1Auth := doHKDF(sharedSecret[:], []byte("UKEY2 v1 auth"), authInfo)
	ps.NextKey = doHKDF(sharedSecret[:], []byte("UKEY2 v1 next"), authInfo)
	authNumber := binary.BigEndian.Uint32(ukeyV1Auth)
	pairingEmoji := pairingEmojis[int(authNumber)%len(pairingEmojis)]
	return pairingEmoji, nil
}

var (
	ErrNoCookies        = errors.New("gaia pairing requires cookies")
	ErrNoDevicesFound   = errors.New("no devices found for gaia pairing")
	ErrIncorrectEmoji   = errors.New("user chose incorrect emoji on phone")
	ErrPairingCancelled = errors.New("user cancelled pairing on phone")
	ErrPairingTimeout   = errors.New("pairing timed out")
)

func (c *Client) DoGaiaPairing(emojiCallback func(string)) error {
	if len(c.AuthData.Cookies) == 0 {
		return ErrNoCookies
	}
	sigResp, err := c.signInGaiaGetToken()
	if err != nil {
		return fmt.Errorf("failed to prepare gaia pairing: %w", err)
	}
	// TODO multiple devices?
	var destRegID string
	for _, dev := range sigResp.GetDeviceData().GetUnknownItems2() {
		if dev.GetDestOrSourceUUID() != sigResp.GetMaybeBrowserUUID() {
			destRegID = dev.GetDestOrSourceUUID()
			break
		}
	}
	if destRegID == "" {
		return ErrNoDevicesFound
	}
	destRegUUID, err := uuid.Parse(destRegID)
	if err != nil {
		return fmt.Errorf("failed to parse destination UUID: %w", err)
	}
	go c.doLongPoll(false)
	ps := NewPairingSession(destRegUUID)
	clientInit, clientFinish, err := ps.PreparePayloads()
	if err != nil {
		return fmt.Errorf("failed to prepare pairing payloads: %w", err)
	}
	serverInit, err := c.sendGaiaPairingMessage(ps, gmproto.ActionType_CREATE_GAIA_PAIRING_CLIENT_INIT, clientInit)
	if err != nil {
		return fmt.Errorf("failed to send client init: %w", err)
	}
	pairingEmoji, err := ps.ProcessServerInit(serverInit)
	if err != nil {
		return fmt.Errorf("error processing server init: %w", err)
	}
	emojiCallback(pairingEmoji)
	finishResp, err := c.sendGaiaPairingMessage(ps, gmproto.ActionType_CREATE_GAIA_PAIRING_CLIENT_FINISHED, clientFinish)
	if finishResp.GetFinishErrorType() != 0 {
		switch finishResp.GetFinishErrorCode() {
		case 5:
			return ErrIncorrectEmoji
		case 7:
			return ErrPairingCancelled
		case 6, 2, 3:
			return fmt.Errorf("%w (code: %d/%d)", ErrPairingTimeout, finishResp.GetFinishErrorType(), finishResp.GetFinishErrorCode())
		default:
			return fmt.Errorf("unknown error pairing: %d/%d", finishResp.GetFinishErrorType(), finishResp.GetFinishErrorCode())
		}
	}
	c.AuthData.RequestCrypto.AESKey = doHKDF(ps.NextKey, encryptionKeyInfo, []byte("client"))
	c.AuthData.RequestCrypto.HMACKey = doHKDF(ps.NextKey, encryptionKeyInfo, []byte("server"))
	c.triggerEvent(&events.PairSuccessful{})

	go func() {
		// Sleep for a bit to let the phone save the pair data. If we reconnect too quickly,
		// the phone won't recognize the session the bridge will get unpaired.
		time.Sleep(2 * time.Second)

		err := c.Reconnect()
		if err != nil {
			c.triggerEvent(&events.ListenFatalError{Error: fmt.Errorf("failed to reconnect after pair success: %w", err)})
		}
	}()
	return nil
}

func (c *Client) sendGaiaPairingMessage(sess PairingSession, action gmproto.ActionType, msg []byte) (*gmproto.GaiaPairingResponseContainer, error) {
	resp, err := c.sessionHandler.sendMessageWithParams(SendMessageParams{
		Action: action,
		Data: &gmproto.GaiaPairingRequestContainer{
			PairingUUID:    sess.UUID.String(),
			BrowserDetails: util.BrowserDetailsMessage,
			StartTimestamp: sess.Start.UnixMilli(),
			Data:           msg,
		},
		DontEncrypt: true,
		CustomTTL:   (300 * time.Second).Microseconds(),
		MessageType: gmproto.MessageType_GAIA_2,

		DestRegistrationIDs: []string{sess.DestRegID.String()},
		NoPingOnTimeout:     true,
	})
	if err != nil {
		return nil, err
	}
	var respDat gmproto.GaiaPairingResponseContainer
	err = proto.Unmarshal(resp.Message.UnencryptedData, &respDat)
	if err != nil {
		return nil, err
	}
	return &respDat, nil
}
