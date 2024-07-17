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
	"context"
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
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"go.mau.fi/util/exsync"
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

func (c *Client) signInGaiaInitial(ctx context.Context) (*gmproto.SignInGaiaResponse, error) {
	payload := c.baseSignInGaiaPayload()
	payload.UnknownInt3 = 1
	return typedHTTPResponse[*gmproto.SignInGaiaResponse](
		c.makeProtobufHTTPRequestContext(ctx, util.SignInGaiaURL, payload, ContentTypePBLite, false),
	)
}

func (c *Client) signInGaiaGetToken(ctx context.Context) (*gmproto.SignInGaiaResponse, error) {
	key, err := x509.MarshalPKIXPublicKey(c.AuthData.RefreshKey.GetPublicKey())
	if err != nil {
		return nil, err
	}

	payload := c.baseSignInGaiaPayload()
	payload.Inner.SomeData = &gmproto.SignInGaiaRequest_Inner_Data{
		SomeData: key,
	}
	resp, err := typedHTTPResponse[*gmproto.SignInGaiaResponse](
		c.makeProtobufHTTPRequestContext(ctx, util.SignInGaiaURL, payload, ContentTypePBLite, false),
	)
	if err != nil {
		return nil, err
	}
	c.updateTachyonAuthToken(resp.GetTokenData())
	device := resp.GetDeviceData().GetDeviceWrapper().GetDevice()
	lowercaseDevice := proto.Clone(device).(*gmproto.Device)
	lowercaseDevice.SourceID = strings.ToLower(device.SourceID)
	c.AuthData.Mobile = lowercaseDevice
	c.AuthData.Browser = device
	return resp, nil
}

type PairingSession struct {
	UUID          uuid.UUID
	Start         time.Time
	PairingKeyDSA *ecdsa.PrivateKey
	InitPayload   []byte
	NextKey       []byte
}

func NewPairingSession() PairingSession {
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	return PairingSession{
		UUID:          uuid.New(),
		Start:         time.Now(),
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

func doHKDF(key, salt, info []byte) []byte {
	h := hkdf.New(sha256.New, key, salt, info)
	out := make([]byte, 32)
	_, err := io.ReadFull(h, out)
	if err != nil {
		panic(err)
	}
	return out
}

var encryptionKeyInfo = []byte{130, 170, 85, 160, 211, 151, 248, 131, 70, 202, 28, 238, 141, 57, 9, 185, 95, 19, 250, 125, 235, 29, 74, 179, 131, 118, 184, 37, 109, 168, 85, 16}
var pairingEmojisV0 = []string{"😁", "😅", "🤣", "🫠", "🥰", "😇", "🤩", "😘", "😜", "🤗", "🤔", "🤐", "😴", "🥶", "🤯", "🤠", "🥳", "🥸", "😎", "🤓", "🧐", "🥹", "😭", "😱", "😖", "🥱", "😮\u200d💨", "🤡", "💩", "👻", "👽", "🤖", "😻", "💌", "💘", "💕", "❤", "💢", "💥", "💫", "💬", "🗯", "💤", "👋", "🙌", "🙏", "✍", "🦶", "👂", "🧠", "🦴", "👀", "🧑", "🧚", "🧍", "👣", "🐵", "🐶", "🐺", "🦊", "🦁", "🐯", "🦓", "🦄", "🐑", "🐮", "🐷", "🐿", "🐰", "🦇", "🐻", "🐨", "🐼", "🦥", "🐾", "🐔", "🐥", "🐦", "🕊", "🦆", "🦉", "🪶", "🦩", "🐸", "🐢", "🦎", "🐍", "🐳", "🐬", "🦭", "🐠", "🐡", "🦈", "🪸", "🐌", "🦋", "🐛", "🐝", "🐞", "🪱", "💐", "🌸", "🌹", "🌻", "🌱", "🌲", "🌴", "🌵", "🌾", "☘", "🍁", "🍂", "🍄", "🪺", "🍇", "🍈", "🍉", "🍋", "🍌", "🍍", "🍎", "🍐", "🍒", "🍓", "🥝", "🥥", "🥑", "🥕", "🌽", "🌶", "🫑", "🥦", "🥜", "🍞", "🥐", "🥨", "🧀", "🍗", "🍔", "🍟", "🍕", "🌭", "🌮", "🥗", "🥣", "🍿", "🦀", "🦑", "🍦", "🍩", "🍪", "🍫", "🍰", "🍬", "🍭", "☕", "🫖", "🍹", "🥤", "🧊", "🥢", "🍽", "🥄", "🧭", "🏔", "🌋", "🏕", "🏖", "🪵", "🏗", "🏡", "🏰", "🛝", "🚂", "🛵", "🛴", "🛼", "🚥", "⚓", "🛟", "⛵", "✈", "🚀", "🛸", "🧳", "⏰", "🌙", "🌡", "🌞", "🪐", "🌠", "🌧", "🌀", "🌈", "☂", "⚡", "❄", "⛄", "🔥", "🎇", "🧨", "✨", "🎈", "🎉", "🎁", "🏆", "🏅", "⚽", "⚾", "🏀", "🏐", "🏈", "🎾", "🎳", "🏓", "🥊", "⛳", "⛸", "🎯", "🪁", "🔮", "🎮", "🧩", "🧸", "🪩", "🖼", "🎨", "🧵", "🧶", "🦺", "🧣", "🧤", "🧦", "🎒", "🩴", "👟", "👑", "👒", "🎩", "🧢", "💎", "🔔", "🎤", "📻", "🎷", "🪗", "🎸", "🎺", "🎻", "🥁", "📺", "🔋", "💻", "💿", "☎", "🕯", "💡", "📖", "📚", "📬", "✏", "✒", "🖌", "🖍", "📝", "💼", "📋", "📌", "📎", "🔑", "🔧", "🧲", "🪜", "🧬", "🔭", "🩹", "🩺", "🪞", "🛋", "🪑", "🛁", "🧹", "🧺", "🔱", "🏁", "🐪", "🐘", "🦃", "🍞", "🍜", "🍠", "🚘", "🤿", "🃏", "👕", "📸", "🏷", "✂", "🧪", "🚪", "🧴", "🧻", "🪣", "🧽", "🚸"}
var pairingEmojisV1 []string

func init() {
	pairingEmojisAddedV1 := []string{"🍋‍🟩", "🐦‍🔥", "🐲", "🪅", "🦜", "🏺", "🗿", "🫐", "⛽", "🍱", "🥡", "🧋", "🍼", "📐"}
	pairingEmojisRemovedV1 := exsync.NewSetWithItems([]string{"💻", "🤗", "💬", "👋", "😁", "😎", "😇", "🥰", "🤓", "🤩"})

	pairingEmojisV1 = append([]string{}, pairingEmojisV0...)
	pairingEmojisV1 = append(pairingEmojisV1, pairingEmojisAddedV1...)
	pairingEmojisV1 = slices.DeleteFunc(pairingEmojisV1, func(s string) bool {
		return pairingEmojisRemovedV1.Has(s)
	})
}

const emojiSVGTemplate = "https://fonts.gstatic.com/s/e/notoemoji/latest/%s/emoji.svg"

func GetEmojiSVG(emoji string) string {
	x := []rune(emoji)
	hexes := make([]string, len(x))
	for i, r := range x {
		hexes[i] = strings.TrimLeft(strconv.FormatInt(int64(r), 16), "0")
	}
	return fmt.Sprintf(emojiSVGTemplate, strings.Join(hexes, "_"))
}

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
	var pairingEmoji string
	switch msg.GetConfirmedVerificationCodeVersion() {
	case 0:
		pairingEmoji = pairingEmojisV0[int(authNumber)%len(pairingEmojisV0)]
	case 1:
		pairingEmoji = pairingEmojisV1[int(authNumber)%len(pairingEmojisV1)]
	default:
		return "", fmt.Errorf("unsupported verification code version %d", msg.GetConfirmedVerificationCodeVersion())
	}
	return pairingEmoji, nil
}

var (
	ErrNoCookies          = errors.New("gaia pairing requires cookies")
	ErrNoDevicesFound     = errors.New("no devices found for gaia pairing")
	ErrIncorrectEmoji     = errors.New("user chose incorrect emoji on phone")
	ErrPairingCancelled   = errors.New("user cancelled pairing on phone")
	ErrPairingTimeout     = errors.New("pairing timed out")
	ErrPairingInitTimeout = errors.New("client init timed out")
	ErrHadMultipleDevices = errors.New("had multiple primary-looking devices")
)

const GaiaInitTimeout = 20 * time.Second

type primaryDeviceID struct {
	RegID      string
	UnknownInt uint64
	LastSeen   time.Time
}

func (c *Client) DoGaiaPairing(ctx context.Context, emojiCallback func(string)) error {
	if !c.AuthData.HasCookies() {
		return ErrNoCookies
	}
	sigResp, err := c.signInGaiaGetToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to prepare gaia pairing: %w", err)
	}
	// Don't log the whole object as it also contains the tachyon token
	zerolog.Ctx(ctx).Debug().
		Any("header", sigResp.Header).
		Str("maybe_browser_uuid", sigResp.MaybeBrowserUUID).
		Any("device_data", sigResp.DeviceData).
		Msg("Gaia devices response")
	var primaryDevices []*primaryDeviceID
	primaryDeviceMap := make(map[string]*primaryDeviceID)
	for _, dev := range sigResp.GetDeviceData().GetUnknownItems2() {
		if dev.GetUnknownInt4() == 1 {
			pd := &primaryDeviceID{
				RegID:      dev.GetDestOrSourceUUID(),
				UnknownInt: dev.GetUnknownBigInt7(),
			}
			primaryDeviceMap[pd.RegID] = pd
			primaryDevices = append(primaryDevices, pd)
		}
	}
	for _, dev := range sigResp.GetDeviceData().GetUnknownItems3() {
		if pd, ok := primaryDeviceMap[dev.GetDestOrSourceUUID()]; ok {
			pd.LastSeen = time.UnixMicro(dev.GetUnknownTimestampMicroseconds())
		}
	}
	if len(primaryDevices) == 0 {
		return ErrNoDevicesFound
	} else if len(primaryDevices) > 1 {
		// Sort by last seen time, newest first
		slices.SortFunc(primaryDevices, func(a, b *primaryDeviceID) int {
			return b.LastSeen.Compare(a.LastSeen)
		})
		zerolog.Ctx(ctx).Warn().
			Any("devices", primaryDevices).
			Int("hacky_device_switcher", c.GaiaHackyDeviceSwitcher).
			Msg("Found multiple primary-looking devices for gaia pairing")
	}
	destRegDev := primaryDevices[c.GaiaHackyDeviceSwitcher%len(primaryDevices)]
	zerolog.Ctx(ctx).Debug().
		Str("dest_reg_uuid", destRegDev.RegID).
		Uint64("dest_reg_unknown_int", destRegDev.UnknownInt).
		Time("dest_reg_last_seen", destRegDev.LastSeen).
		Msg("Found UUID to use for gaia pairing")
	destRegUUID, err := uuid.Parse(destRegDev.RegID)
	if err != nil {
		return fmt.Errorf("failed to parse destination UUID: %w", err)
	}
	c.AuthData.DestRegID = destRegUUID
	var longPollConnectWait sync.WaitGroup
	longPollConnectWait.Add(1)
	go c.doLongPoll(false, longPollConnectWait.Done)
	longPollConnectWait.Wait()
	ps := NewPairingSession()
	clientInit, clientFinish, err := ps.PreparePayloads()
	if err != nil {
		return fmt.Errorf("failed to prepare pairing payloads: %w", err)
	}
	initCtx, cancel := context.WithTimeout(ctx, GaiaInitTimeout)
	serverInit, err := c.sendGaiaPairingMessage(initCtx, ps, gmproto.ActionType_CREATE_GAIA_PAIRING_CLIENT_INIT, clientInit)
	cancel()
	if err != nil {
		cancelErr := c.cancelGaiaPairing(ps)
		if cancelErr != nil {
			zerolog.Ctx(ctx).Warn().Err(err).Msg("Failed to send gaia pairing cancel request after init timeout")
		}
		if errors.Is(err, context.DeadlineExceeded) {
			err = ErrPairingInitTimeout
			if len(primaryDevices) > 1 {
				err = fmt.Errorf("%w (%w)", ErrPairingInitTimeout, ErrHadMultipleDevices)
			}
			return err
		}
		return fmt.Errorf("failed to send client init: %w", err)
	}
	zerolog.Ctx(ctx).Debug().
		Int32("key_derivation_version", serverInit.GetConfirmedKeyDerivationVersion()).
		Int32("verification_code_version", serverInit.GetConfirmedVerificationCodeVersion()).
		Msg("Received server init")
	pairingEmoji, err := ps.ProcessServerInit(serverInit)
	if err != nil {
		cancelErr := c.cancelGaiaPairing(ps)
		if cancelErr != nil {
			zerolog.Ctx(ctx).Warn().Err(err).Msg("Failed to send gaia pairing cancel request after error processing server init")
		}
		return fmt.Errorf("error processing server init: %w", err)
	}
	emojiCallback(pairingEmoji)
	finishResp, err := c.sendGaiaPairingMessage(ctx, ps, gmproto.ActionType_CREATE_GAIA_PAIRING_CLIENT_FINISHED, clientFinish)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			zerolog.Ctx(ctx).Debug().Msg("Sending gaia pairing cancel after context was canceled")
			cancelErr := c.cancelGaiaPairing(ps)
			if cancelErr != nil {
				zerolog.Ctx(ctx).Warn().Err(err).Msg("Failed to send gaia pairing cancel request after context was canceled")
			}
		}
		return fmt.Errorf("failed to send client finish: %w", err)
	}
	if finishResp.GetFinishErrorType() != 0 {
		switch finishResp.GetFinishErrorCode() {
		case 5:
			return ErrIncorrectEmoji
		case 7:
			return ErrPairingCancelled
		case 6, 2, 3:
			return fmt.Errorf("%w (code: %d/%d)", ErrPairingTimeout, finishResp.GetFinishErrorType(), finishResp.GetFinishErrorCode())
		case 10:
			if finishResp.GetFinishErrorCode() == 27 {
				return fmt.Errorf("%w (user chose 'this is not me' option)", ErrPairingCancelled)
			}
			fallthrough
		default:
			return fmt.Errorf("unknown error pairing: %d/%d", finishResp.GetFinishErrorType(), finishResp.GetFinishErrorCode())
		}
	}
	ukey2ClientKey := doHKDF(ps.NextKey, encryptionKeyInfo, []byte("client"))
	ukey2ServerKey := doHKDF(ps.NextKey, encryptionKeyInfo, []byte("server"))
	switch serverInit.GetConfirmedKeyDerivationVersion() {
	case 0:
		c.AuthData.RequestCrypto.AESKey = ukey2ClientKey
		c.AuthData.RequestCrypto.HMACKey = ukey2ServerKey
	case 1:
		concattedUkeys := make([]byte, 3*32)
		copy(concattedUkeys[0:32], encryptionKeyInfo)
		if byteHash(ukey2ClientKey) < byteHash(ukey2ServerKey) {
			copy(concattedUkeys[32:64], ukey2ClientKey)
			copy(concattedUkeys[64:96], ukey2ServerKey)
		} else {
			copy(concattedUkeys[32:64], ukey2ServerKey)
			copy(concattedUkeys[64:96], ukey2ClientKey)
		}
		concattedHash := sha256.Sum256(concattedUkeys)
		c.AuthData.RequestCrypto.AESKey = doHKDF(concattedHash[:], []byte("Ditto salt 1"), []byte("Ditto info 1"))
		c.AuthData.RequestCrypto.HMACKey = doHKDF(concattedHash[:], []byte("Ditto salt 2"), []byte("Ditto info 2"))
	default:
		return fmt.Errorf("unsupported key derivation version %d", serverInit.GetConfirmedKeyDerivationVersion())
	}
	c.AuthData.PairingID = ps.UUID
	c.triggerEvent(&events.PairSuccessful{PhoneID: fmt.Sprintf("%s/%d", c.AuthData.Mobile.GetSourceID(), destRegDev.UnknownInt)})

	go func() {
		err := c.Reconnect()
		if err != nil {
			c.triggerEvent(&events.ListenFatalError{Error: fmt.Errorf("failed to reconnect after pair success: %w", err)})
		}
	}()
	return nil
}

func byteHash(bytes []byte) (out int32) {
	out = 1
	for _, b := range bytes {
		out = 31*out + int32(int8(b))
	}
	return out
}

func (c *Client) cancelGaiaPairing(sess PairingSession) error {
	return c.sessionHandler.sendMessageNoResponse(SendMessageParams{
		Action:      gmproto.ActionType_CANCEL_GAIA_PAIRING,
		RequestID:   sess.UUID.String(),
		DontEncrypt: true,
		CustomTTL:   (300 * time.Second).Microseconds(),
		MessageType: gmproto.MessageType_GAIA_2,
	})
}

func (c *Client) sendGaiaPairingMessage(ctx context.Context, sess PairingSession, action gmproto.ActionType, msg []byte) (*gmproto.GaiaPairingResponseContainer, error) {
	reqContainer := &gmproto.GaiaPairingRequestContainer{
		PairingAttemptID: sess.UUID.String(),
		BrowserDetails:   util.BrowserDetailsMessage,
		StartTimestamp:   sess.Start.UnixMilli(),
		Data:             msg,
	}
	msgType := gmproto.MessageType_GAIA_2
	if action == gmproto.ActionType_CREATE_GAIA_PAIRING_CLIENT_FINISHED {
		msgType = gmproto.MessageType_BUGLE_MESSAGE
	} else {
		reqContainer.ProposedVerificationCodeVersion = 1
		reqContainer.ProposedKeyDerivationVersion = 1
	}
	respCh, err := c.sessionHandler.sendAsyncMessage(SendMessageParams{
		Action:      action,
		Data:        reqContainer,
		DontEncrypt: true,
		CustomTTL:   (300 * time.Second).Microseconds(),
		MessageType: msgType,
	})
	if err != nil {
		return nil, err
	}
	select {
	case resp := <-respCh:
		var respDat gmproto.GaiaPairingResponseContainer
		err = proto.Unmarshal(resp.Message.UnencryptedData, &respDat)
		if err != nil {
			return nil, err
		}
		return &respDat, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *Client) UnpairGaia() error {
	return c.sessionHandler.sendMessageNoResponse(SendMessageParams{
		Action: gmproto.ActionType_UNPAIR_GAIA_PAIRING,
		Data: &gmproto.RevokeGaiaPairingRequest{
			PairingAttemptID: c.AuthData.PairingID.String(),
		},
	})
}
