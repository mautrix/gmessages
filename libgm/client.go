package libgm

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/rs/zerolog"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/crypto"
	"go.mau.fi/mautrix-gmessages/libgm/events"
	"go.mau.fi/mautrix-gmessages/libgm/payload"
	"go.mau.fi/mautrix-gmessages/libgm/pblite"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

type AuthData struct {
	TachyonAuthToken []byte             `json:"tachyon_token,omitempty"`
	TTL              int64              `json:"ttl,omitempty"`
	AuthenticatedAt  *time.Time         `json:"authenticated_at,omitempty"`
	DevicePair       *pblite.DevicePair `json:"device_pair,omitempty"`
	Cryptor          *crypto.Cryptor    `json:"crypto,omitempty"`
	WebEncryptionKey []byte             `json:"web_encryption_key,omitempty"`
	JWK              *crypto.JWK        `json:"jwk,omitempty"`
}
type Proxy func(*http.Request) (*url.URL, error)
type EventHandler func(evt interface{})
type Client struct {
	Logger         zerolog.Logger
	Conversations  *Conversations
	Session        *Session
	Messages       *Messages
	rpc            *RPC
	pairer         *Pairer
	evHandler      EventHandler
	sessionHandler *SessionHandler

	imageCryptor *crypto.ImageCryptor
	authData     *AuthData

	proxy Proxy
	http  *http.Client
}

func NewClient(authData *AuthData, logger zerolog.Logger) *Client {
	sessionHandler := &SessionHandler{
		requests:        make(map[string]map[binary.ActionType]*ResponseChan),
		responseTimeout: time.Duration(5000) * time.Millisecond,
	}
	if authData == nil {
		authData = &AuthData{}
	}
	if authData.Cryptor == nil {
		authData.Cryptor = crypto.NewCryptor(nil, nil)
	}
	cli := &Client{
		authData:       authData,
		Logger:         logger,
		imageCryptor:   &crypto.ImageCryptor{},
		sessionHandler: sessionHandler,
		http:           &http.Client{},
	}
	sessionHandler.client = cli
	rpc := &RPC{client: cli, http: &http.Client{Transport: &http.Transport{Proxy: cli.proxy}}}
	cli.rpc = rpc
	cli.Logger.Debug().Any("data", cli.authData.Cryptor).Msg("Cryptor")
	cli.setApiMethods()
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
	if c.authData.TachyonAuthToken != nil {

		hasExpired, authenticatedAtSeconds := c.hasTachyonTokenExpired()
		if hasExpired {
			c.Logger.Error().Any("expired", hasExpired).Any("secondsSince", authenticatedAtSeconds).Msg("TachyonToken has expired! attempting to refresh")
			refreshErr := c.refreshAuthToken()
			if refreshErr != nil {
				panic(refreshErr)
			}
		}
		c.Logger.Info().Any("secondsSince", authenticatedAtSeconds).Any("token", c.authData.TachyonAuthToken).Msg("TachyonToken has not expired, attempting to connect...")

		webEncryptionKeyResponse, webEncryptionKeyErr := c.GetWebEncryptionKey()
		if webEncryptionKeyErr != nil {
			c.Logger.Err(webEncryptionKeyErr).Any("response", webEncryptionKeyResponse).Msg("GetWebEncryptionKey request failed")
			return webEncryptionKeyErr
		}
		c.updateWebEncryptionKey(webEncryptionKeyResponse.GetKey())
		rpcPayload, receiveMessageSessionId, err := payload.ReceiveMessages(c.authData.TachyonAuthToken)
		if err != nil {
			panic(err)
			return err
		}
		c.rpc.rpcSessionId = receiveMessageSessionId
		go c.rpc.ListenReceiveMessages(rpcPayload)
		c.sessionHandler.startAckInterval()

		bugleRes, bugleErr := c.Session.IsBugleDefault()
		if bugleErr != nil {
			panic(bugleErr)
		}
		c.Logger.Info().Any("isBugle", bugleRes.Success).Msg("IsBugleDefault")
		sessionErr := c.Session.SetActiveSession()
		if sessionErr != nil {
			panic(sessionErr)
		}
		//c.Logger.Debug().Any("tachyonAuthToken", c.authData.TachyonAuthToken).Msg("Successfully connected to server")
		return nil
	} else {
		pairer, err := c.NewPairer(nil, 20)
		if err != nil {
			panic(err)
		}
		c.pairer = pairer
		registered, err2 := c.pairer.RegisterPhoneRelay()
		if err2 != nil {
			return err2
		}
		c.authData.TachyonAuthToken = registered.AuthKeyData.TachyonAuthToken
		rpcPayload, receiveMessageSessionId, err := payload.ReceiveMessages(c.authData.TachyonAuthToken)
		if err != nil {
			panic(err)
			return err
		}
		c.rpc.rpcSessionId = receiveMessageSessionId
		go c.rpc.ListenReceiveMessages(rpcPayload)
		return nil
	}
}

func (c *Client) Disconnect() {
	c.rpc.CloseConnection()
	c.http.CloseIdleConnections()
}

func (c *Client) IsConnected() bool {
	return c.rpc != nil
}

func (c *Client) IsLoggedIn() bool {
	return c.authData != nil && c.authData.DevicePair != nil
}

func (c *Client) hasTachyonTokenExpired() (bool, string) {
	if c.authData.TachyonAuthToken == nil || c.authData.AuthenticatedAt == nil {
		return true, ""
	} else {
		duration := time.Since(*c.authData.AuthenticatedAt)
		seconds := fmt.Sprintf("%.3f", duration.Seconds())
		if duration.Microseconds() > 86400000000 {
			return true, seconds
		}
		return false, seconds
	}
}

func (c *Client) Reconnect() error {
	c.rpc.CloseConnection()
	for c.rpc.conn != nil {
		time.Sleep(time.Millisecond * 100)
	}
	err := c.Connect()
	if err != nil {
		c.Logger.Err(err).Any("tachyonAuthToken", c.authData.TachyonAuthToken).Msg("Failed to reconnect")
		return err
	}
	c.Logger.Debug().Any("tachyonAuthToken", c.authData.TachyonAuthToken).Msg("Successfully reconnected to server")
	return nil
}

func (c *Client) triggerEvent(evt interface{}) {
	if c.evHandler != nil {
		c.evHandler(evt)
	}
}

func (c *Client) setApiMethods() {
	c.Conversations = &Conversations{client: c}
	c.Session = &Session{client: c}
	c.Messages = &Messages{client: c}
}

func (c *Client) DownloadMedia(mediaID string, key []byte) ([]byte, error) {
	reqId := util.RandomUUIDv4()
	download_metadata := &binary.UploadImagePayload{
		MetaData: &binary.ImageMetaData{
			ImageID:   mediaID,
			Encrypted: true,
		},
		AuthData: &binary.AuthMessage{
			RequestID:        reqId,
			TachyonAuthToken: c.authData.TachyonAuthToken,
			ConfigVersion:    payload.ConfigMessage,
		},
	}
	download_metadata_bytes, err2 := binary.EncodeProtoMessage(download_metadata)
	if err2 != nil {
		return nil, err2
	}
	download_metadata_b64 := base64.StdEncoding.EncodeToString(download_metadata_bytes)
	req, err := http.NewRequest("GET", util.UPLOAD_MEDIA, nil)
	if err != nil {
		return nil, err
	}
	util.BuildUploadHeaders(req, download_metadata_b64)
	res, reqErr := c.http.Do(req)
	if reqErr != nil {
		return nil, reqErr
	}
	c.Logger.Info().Any("url", util.UPLOAD_MEDIA).Any("headers", res.Request.Header).Msg("Decrypt Image Headers")
	defer res.Body.Close()
	encryptedBuffImg, err3 := io.ReadAll(res.Body)
	if err3 != nil {
		return nil, err3
	}
	c.Logger.Debug().Any("key", key).Any("encryptedLength", len(encryptedBuffImg)).Msg("Attempting to decrypt image")
	c.imageCryptor.UpdateDecryptionKey(key)
	decryptedImageBytes, decryptionErr := c.imageCryptor.DecryptData(encryptedBuffImg)
	if decryptionErr != nil {
		return nil, decryptionErr
	}
	return decryptedImageBytes, nil
}

func (c *Client) FetchConfigVersion() {
	req, bErr := http.NewRequest("GET", util.CONFIG_URL, nil)
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

	currVersion := payload.ConfigMessage
	if version.V1 != currVersion.V1 || version.V2 != currVersion.V2 || version.V3 != currVersion.V3 {
		toLog := c.diffVersionFormat(currVersion, version)
		c.Logger.Info().Any("version", toLog).Msg("There's a new version available!")
	} else {
		c.Logger.Info().Any("version", currVersion).Msg("You are running on the latest version.")
	}
}

func (c *Client) diffVersionFormat(curr *binary.ConfigVersion, latest *binary.ConfigVersion) string {
	return fmt.Sprintf("%d.%d.%d -> %d.%d.%d", curr.V1, curr.V2, curr.V3, latest.V1, latest.V2, latest.V3)
}

func (c *Client) updateWebEncryptionKey(key []byte) {
	c.Logger.Debug().Any("key", key).Msg("Updated WebEncryptionKey")
	c.authData.WebEncryptionKey = key
}

func (c *Client) updateJWK(jwk *crypto.JWK) {
	c.Logger.Debug().Any("jwk", jwk).Msg("Updated JWK")
	c.authData.JWK = jwk
}

func (c *Client) updateTachyonAuthToken(t []byte) {
	authenticatedAt := util.TimestampNow()
	c.authData.TachyonAuthToken = t
	c.authData.AuthenticatedAt = &authenticatedAt
	c.Logger.Debug().Any("authenticatedAt", authenticatedAt).Any("tachyonAuthToken", t).Msg("Updated TachyonAuthToken")
}

func (c *Client) updateTTL(ttl int64) {
	c.authData.TTL = ttl
	c.Logger.Debug().Any("ttl", ttl).Msg("Updated TTL")
}

func (c *Client) updateDevicePair(devicePair *pblite.DevicePair) {
	c.authData.DevicePair = devicePair
	c.Logger.Debug().Any("devicePair", devicePair).Msg("Updated DevicePair")
}

func (c *Client) SaveAuthSession(path string) error {
	toSaveJson, jsonErr := json.Marshal(c.authData)
	if jsonErr != nil {
		return jsonErr
	}
	writeErr := os.WriteFile(path, toSaveJson, os.ModePerm)
	return writeErr
}

func LoadAuthSession(path string) (*AuthData, error) {
	jsonData, readErr := os.ReadFile(path)
	if readErr != nil {
		return nil, readErr
	}

	sessionData := &AuthData{}
	marshalErr := json.Unmarshal(jsonData, sessionData)
	if marshalErr != nil {
		return nil, marshalErr
	}

	return sessionData, nil
}

func (c *Client) RefreshAuthToken() error {
	return c.refreshAuthToken()
}

func (c *Client) refreshAuthToken() error {

	jwk := c.authData.JWK
	requestId := util.RandomUUIDv4()
	timestamp := time.Now().UnixMilli() * 1000

	sig, sigErr := jwk.SignRequest(requestId, int64(timestamp))
	if sigErr != nil {
		return sigErr
	}

	payloadMessage, messageErr := payload.RegisterRefresh(sig, requestId, int64(timestamp), c.authData.DevicePair.Browser, c.authData.TachyonAuthToken)
	if messageErr != nil {
		return messageErr
	}

	c.Logger.Info().Any("payload", string(payloadMessage)).Msg("Attempting to refresh auth token")

	refreshResponse, requestErr := c.rpc.sendMessageRequest(util.REGISTER_REFRESH, payloadMessage)
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

	resp := &binary.RegisterRefreshResponse{}
	deserializeErr := pblite.Unmarshal(responseBody, resp)
	if deserializeErr != nil {
		return deserializeErr
	}

	token := resp.GetTokenData().GetTachyonAuthToken()
	if token == nil {
		return fmt.Errorf("failed to refresh auth token: something happened")
	}

	c.updateTachyonAuthToken(token)
	c.triggerEvent(events.NewAuthTokenRefreshed(token))
	return nil
}
