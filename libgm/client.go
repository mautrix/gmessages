package libgm

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-gmessages/libgm/crypto"
	"go.mau.fi/mautrix-gmessages/libgm/events"
	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
	"go.mau.fi/mautrix-gmessages/libgm/pblite"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

type AuthData struct {
	// Keys used to encrypt communication with the phone
	RequestCrypto *crypto.AESCTRHelper `json:"request_crypto,omitempty"`
	// Key used to sign requests to refresh the tachyon auth token from the server
	RefreshKey *crypto.JWK `json:"refresh_key,omitempty"`
	// Identity of the paired phone and browser
	Browser *gmproto.Device `json:"browser,omitempty"`
	Mobile  *gmproto.Device `json:"mobile,omitempty"`
	// Key used to authenticate with the server
	TachyonAuthToken []byte    `json:"tachyon_token,omitempty"`
	TachyonExpiry    time.Time `json:"tachyon_expiry,omitempty"`
	TachyonTTL       int64     `json:"tachyon_ttl,omitempty"`
	// Unknown encryption key, not used for anything
	WebEncryptionKey []byte `json:"web_encryption_key,omitempty"`
}

const RefreshTachyonBuffer = 1 * time.Hour

type Proxy func(*http.Request) (*url.URL, error)
type EventHandler func(evt any)

type Client struct {
	Logger         zerolog.Logger
	rpc            *RPC
	evHandler      EventHandler
	sessionHandler *SessionHandler

	AuthData *AuthData

	proxy Proxy
	http  *http.Client
}

func NewAuthData() *AuthData {
	return &AuthData{
		RequestCrypto: crypto.NewAESCTRHelper(),
		RefreshKey:    crypto.GenerateECDSAKey(),
	}
}

func NewClient(authData *AuthData, logger zerolog.Logger) *Client {
	sessionHandler := &SessionHandler{
		responseWaiters: make(map[string]chan<- *IncomingRPCMessage),
		responseTimeout: time.Duration(5000) * time.Millisecond,
	}
	cli := &Client{
		AuthData:       authData,
		Logger:         logger,
		sessionHandler: sessionHandler,
		http:           &http.Client{},
	}
	sessionHandler.client = cli
	rpc := &RPC{client: cli, http: &http.Client{Transport: &http.Transport{Proxy: cli.proxy}}}
	cli.rpc = rpc
	cli.FetchConfigVersion()
	return cli
}

func (c *Client) SetEventHandler(eventHandler EventHandler) {
	c.evHandler = eventHandler
}

func (c *Client) SetProxy(proxy string) error {
	proxyParsed, err := url.Parse(proxy)
	if err != nil {
		c.Logger.Fatal().Err(err).Msg("Failed to set proxy")
	}
	proxyUrl := http.ProxyURL(proxyParsed)
	c.http.Transport = &http.Transport{
		Proxy: proxyUrl,
	}
	c.proxy = proxyUrl
	c.Logger.Debug().Any("proxy", proxyParsed.Host).Msg("SetProxy")
	return nil
}

func (c *Client) Connect() error {
	if c.AuthData.TachyonAuthToken == nil {
		return fmt.Errorf("no auth token")
	} else if c.AuthData.Browser == nil {
		return fmt.Errorf("not logged in")
	}

	err := c.refreshAuthToken()
	if err != nil {
		return fmt.Errorf("failed to refresh auth token: %w", err)
	}

	webEncryptionKeyResponse, err := c.GetWebEncryptionKey()
	if err != nil {
		return fmt.Errorf("failed to get web encryption key: %w", err)
	}
	c.updateWebEncryptionKey(webEncryptionKeyResponse.GetKey())
	go c.rpc.ListenReceiveMessages()
	c.sessionHandler.startAckInterval()

	bugleRes, bugleErr := c.IsBugleDefault()
	if bugleErr != nil {
		return fmt.Errorf("failed to check bugle default: %w", err)
	}
	c.Logger.Debug().Bool("bugle_default", bugleRes.Success).Msg("Got is bugle default response on connect")
	sessionErr := c.SetActiveSession()
	if sessionErr != nil {
		return fmt.Errorf("failed to set active session: %w", err)
	}
	return nil
}

func (c *Client) StartLogin() (string, error) {
	registered, err := c.RegisterPhoneRelay()
	if err != nil {
		return "", err
	}
	c.AuthData.TachyonAuthToken = registered.AuthKeyData.TachyonAuthToken
	go c.rpc.ListenReceiveMessages()
	qr, err := c.GenerateQRCodeData(registered.GetPairingKey())
	if err != nil {
		return "", fmt.Errorf("failed to generate QR code: %w", err)
	}
	return qr, nil
}

func (c *Client) GenerateQRCodeData(pairingKey []byte) (string, error) {
	urlData := &gmproto.URLData{
		PairingKey: pairingKey,
		AESKey:     c.AuthData.RequestCrypto.AESKey,
		HMACKey:    c.AuthData.RequestCrypto.HMACKey,
	}
	encodedURLData, err := proto.Marshal(urlData)
	if err != nil {
		return "", err
	}
	cData := base64.StdEncoding.EncodeToString(encodedURLData)
	return util.QRCodeURLBase + cData, nil
}

func (c *Client) Disconnect() {
	c.rpc.CloseConnection()
	c.http.CloseIdleConnections()
}

func (c *Client) IsConnected() bool {
	return c.rpc != nil
}

func (c *Client) IsLoggedIn() bool {
	return c.AuthData != nil && c.AuthData.Browser != nil
}

func (c *Client) Reconnect() error {
	c.rpc.CloseConnection()
	for c.rpc.conn != nil {
		time.Sleep(time.Millisecond * 100)
	}
	err := c.Connect()
	if err != nil {
		c.Logger.Err(err).Msg("Failed to reconnect")
		return err
	}
	c.Logger.Debug().Msg("Successfully reconnected to server")
	return nil
}

func (c *Client) triggerEvent(evt interface{}) {
	if c.evHandler != nil {
		c.evHandler(evt)
	}
}

func (c *Client) DownloadMedia(mediaID string, key []byte) ([]byte, error) {
	downloadMetadata := &gmproto.UploadImagePayload{
		MetaData: &gmproto.ImageMetaData{
			ImageID:   mediaID,
			Encrypted: true,
		},
		AuthData: &gmproto.AuthMessage{
			RequestID:        uuid.NewString(),
			TachyonAuthToken: c.AuthData.TachyonAuthToken,
			ConfigVersion:    util.ConfigMessage,
		},
	}
	downloadMetadataBytes, err := proto.Marshal(downloadMetadata)
	if err != nil {
		return nil, err
	}
	downloadMetadataEncoded := base64.StdEncoding.EncodeToString(downloadMetadataBytes)
	req, err := http.NewRequest("GET", util.UploadMediaURL, nil)
	if err != nil {
		return nil, err
	}
	util.BuildUploadHeaders(req, downloadMetadataEncoded)
	res, reqErr := c.http.Do(req)
	if reqErr != nil {
		return nil, reqErr
	}
	defer res.Body.Close()
	encryptedBuffImg, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	cryptor, err := crypto.NewAESGCMHelper(key)
	if err != nil {
		return nil, err
	}
	decryptedImageBytes, decryptionErr := cryptor.DecryptData(encryptedBuffImg)
	if decryptionErr != nil {
		return nil, decryptionErr
	}
	return decryptedImageBytes, nil
}

func (c *Client) FetchConfigVersion() {
	req, bErr := http.NewRequest("GET", util.ConfigUrl, nil)
	if bErr != nil {
		panic(bErr)
	}

	configRes, requestErr := c.http.Do(req)
	if requestErr != nil {
		panic(requestErr)
	}

	responseBody, readErr := io.ReadAll(configRes.Body)
	if readErr != nil {
		panic(readErr)
	}

	version, parseErr := util.ParseConfigVersion(responseBody)
	if parseErr != nil {
		panic(parseErr)
	}

	currVersion := util.ConfigMessage
	if version.Year != currVersion.Year || version.Month != currVersion.Month || version.Day != currVersion.Day {
		toLog := c.diffVersionFormat(currVersion, version)
		c.Logger.Info().Any("version", toLog).Msg("There's a new version available!")
	} else {
		c.Logger.Info().Any("version", currVersion).Msg("You are running on the latest version.")
	}
}

func (c *Client) diffVersionFormat(curr *gmproto.ConfigVersion, latest *gmproto.ConfigVersion) string {
	return fmt.Sprintf("%d.%d.%d -> %d.%d.%d", curr.Year, curr.Month, curr.Day, latest.Year, latest.Month, latest.Day)
}

func (c *Client) updateWebEncryptionKey(key []byte) {
	c.Logger.Debug().Msg("Updated WebEncryptionKey")
	c.AuthData.WebEncryptionKey = key
}

func (c *Client) updateTachyonAuthToken(t []byte, validFor int64) {
	c.AuthData.TachyonAuthToken = t
	validForDuration := time.Duration(validFor) * time.Microsecond
	if validForDuration == 0 {
		validForDuration = 24 * time.Hour
	}
	c.AuthData.TachyonExpiry = time.Now().UTC().Add(time.Microsecond * time.Duration(validFor))
	c.AuthData.TachyonTTL = validForDuration.Microseconds()
	c.Logger.Debug().
		Time("tachyon_expiry", c.AuthData.TachyonExpiry).
		Int64("valid_for", validFor).
		Msg("Updated tachyon token")
}

func (c *Client) refreshAuthToken() error {
	if c.AuthData.Browser == nil || time.Until(c.AuthData.TachyonExpiry) > RefreshTachyonBuffer {
		return nil
	}
	c.Logger.Debug().
		Time("tachyon_expiry", c.AuthData.TachyonExpiry).
		Msg("Refreshing auth token")
	jwk := c.AuthData.RefreshKey
	requestID := uuid.NewString()
	timestamp := time.Now().UnixMilli() * 1000

	signBytes := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", requestID, timestamp)))
	sig, err := ecdsa.SignASN1(rand.Reader, jwk.GetPrivateKey(), signBytes[:])
	if err != nil {
		return err
	}

	payload, err := pblite.Marshal(&gmproto.RegisterRefreshPayload{
		MessageAuth: &gmproto.AuthMessage{
			RequestID:        requestID,
			TachyonAuthToken: c.AuthData.TachyonAuthToken,
			ConfigVersion:    util.ConfigMessage,
		},
		CurrBrowserDevice: c.AuthData.Browser,
		UnixTimestamp:     timestamp,
		Signature:         sig,
		EmptyRefreshArr:   &gmproto.EmptyRefreshArr{EmptyArr: &gmproto.EmptyArr{}},
		MessageType:       2, // hmm
	})
	if err != nil {
		return err
	}

	refreshResponse, requestErr := c.rpc.sendMessageRequest(util.RegisterRefreshURL, payload)
	if requestErr != nil {
		return requestErr
	}

	if refreshResponse.StatusCode == 401 {
		return fmt.Errorf("failed to refresh auth token: unauthorized (try reauthenticating through qr code)")
	}

	if refreshResponse.StatusCode == 400 {
		return fmt.Errorf("failed to refresh auth token: signature failed")
	}
	responseBody, readErr := io.ReadAll(refreshResponse.Body)
	if readErr != nil {
		return readErr
	}

	resp := &gmproto.RegisterRefreshResponse{}
	deserializeErr := pblite.Unmarshal(responseBody, resp)
	if deserializeErr != nil {
		return deserializeErr
	}

	token := resp.GetTokenData().GetTachyonAuthToken()
	if token == nil {
		return fmt.Errorf("failed to refresh auth token: something happened")
	}

	validFor, _ := strconv.ParseInt(resp.GetTokenData().GetValidFor(), 10, 64)

	c.updateTachyonAuthToken(token, validFor)
	c.triggerEvent(&events.AuthTokenRefreshed{})
	return nil
}
