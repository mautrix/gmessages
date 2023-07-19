package libgm

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"go.mau.fi/mautrix-gmessages/libgm/crypto"
	"go.mau.fi/mautrix-gmessages/libgm/events"
	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
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

type updateDedupItem struct {
	id   string
	hash [32]byte
}

type Client struct {
	Logger         zerolog.Logger
	evHandler      EventHandler
	sessionHandler *SessionHandler

	longPollingConn io.Closer
	listenID        int
	skipCount       int
	disconnecting   bool

	pingShortCircuit chan struct{}

	recentUpdates    [8]updateDedupItem
	recentUpdatesPtr int

	conversationsFetchedOnce bool

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
	}
	cli := &Client{
		AuthData:       authData,
		Logger:         logger,
		sessionHandler: sessionHandler,
		http:           &http.Client{},

		pingShortCircuit: make(chan struct{}),
	}
	sessionHandler.client = cli
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
	go c.doLongPoll(true)
	c.sessionHandler.startAckInterval()
	go c.postConnect()
	return nil
}

func (c *Client) postConnect() {
	err := c.SetActiveSession()
	if err != nil {
		c.Logger.Err(err).Msg("Failed to set active session")
		return
	}

	doneChan := make(chan struct{})
	go func() {
		select {
		case <-time.After(5 * time.Second):
			c.Logger.Warn().Msg("Checking bugle default on connect is taking long")
		case <-doneChan:
		}
	}()
	bugleRes, err := c.IsBugleDefault()
	close(doneChan)
	if err != nil {
		c.Logger.Err(err).Msg("Failed to check bugle default")
		return
	}
	c.Logger.Debug().Bool("bugle_default", bugleRes.Success).Msg("Got is bugle default response on connect")

}

func (c *Client) Disconnect() {
	c.closeLongPolling()
	c.http.CloseIdleConnections()
}

func (c *Client) IsConnected() bool {
	// TODO add better check (longPollingConn is set to nil while the polling reconnects)
	return c.longPollingConn != nil
}

func (c *Client) IsLoggedIn() bool {
	return c.AuthData != nil && c.AuthData.Browser != nil
}

func (c *Client) Reconnect() error {
	c.closeLongPolling()
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

	payload := &gmproto.RegisterRefreshRequest{
		MessageAuth: &gmproto.AuthMessage{
			RequestID:        requestID,
			TachyonAuthToken: c.AuthData.TachyonAuthToken,
			ConfigVersion:    util.ConfigMessage,
		},
		CurrBrowserDevice: c.AuthData.Browser,
		UnixTimestamp:     timestamp,
		Signature:         sig,
		EmptyRefreshArr:   &gmproto.RegisterRefreshRequest_NestedEmptyArr{EmptyArr: &gmproto.EmptyArr{}},
		MessageType:       2, // hmm
	}

	resp, err := typedHTTPResponse[*gmproto.RegisterRefreshResponse](
		c.makeProtobufHTTPRequest(util.RegisterRefreshURL, payload, ContentTypePBLite),
	)
	if err != nil {
		return err
	}

	token := resp.GetTokenData().GetTachyonAuthToken()
	if token == nil {
		return fmt.Errorf("no tachyon auth token in refresh response")
	}

	validFor, _ := strconv.ParseInt(resp.GetTokenData().GetValidFor(), 10, 64)

	c.updateTachyonAuthToken(token, validFor)
	c.triggerEvent(&events.AuthTokenRefreshed{})
	return nil
}
