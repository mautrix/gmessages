package libgm

import (
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-gmessages/libgm/events"
	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

func (c *Client) StartLogin() (string, error) {
	registered, err := c.RegisterPhoneRelay()
	if err != nil {
		return "", err
	}
	c.updateTachyonAuthToken(registered.GetAuthKeyData())
	go c.doLongPoll(false)
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

func (c *Client) handlePairingEvent(msg *IncomingRPCMessage) {
	switch evt := msg.Pair.Event.(type) {
	case *gmproto.RPCPairData_Paired:
		c.completePairing(evt.Paired)
	case *gmproto.RPCPairData_Revoked:
		c.triggerEvent(evt.Revoked)
	default:
		c.Logger.Debug().Any("evt", evt).Msg("Unknown pair event type")
	}
}

func (c *Client) completePairing(data *gmproto.PairedData) {
	c.updateTachyonAuthToken(data.GetTokenData())
	c.AuthData.Mobile = data.Mobile
	c.AuthData.Browser = data.Browser

	c.triggerEvent(&events.PairSuccessful{PairedData: data})

	go func() {
		// Sleep for a bit to let the phone save the pair data. If we reconnect too quickly,
		// the phone won't recognize the session the bridge will get unpaired.
		time.Sleep(2 * time.Second)

		err := c.Reconnect()
		if err != nil {
			c.triggerEvent(&events.ListenFatalError{Error: fmt.Errorf("failed to reconnect after pair success: %w", err)})
		}
	}()
}

func (c *Client) RegisterPhoneRelay() (*gmproto.RegisterPhoneRelayResponse, error) {
	key, err := x509.MarshalPKIXPublicKey(c.AuthData.RefreshKey.GetPublicKey())
	if err != nil {
		return nil, err
	}

	payload := &gmproto.AuthenticationContainer{
		AuthMessage: &gmproto.AuthMessage{
			RequestID:     uuid.NewString(),
			Network:       util.QRNetwork,
			ConfigVersion: util.ConfigMessage,
		},
		BrowserDetails: util.BrowserDetailsMessage,
		Data: &gmproto.AuthenticationContainer_KeyData{
			KeyData: &gmproto.KeyData{
				EcdsaKeys: &gmproto.ECDSAKeys{
					Field1:        2,
					EncryptedKeys: key,
				},
			},
		},
	}
	return typedHTTPResponse[*gmproto.RegisterPhoneRelayResponse](
		c.makeProtobufHTTPRequest(util.RegisterPhoneRelayURL, payload, ContentTypeProtobuf),
	)
}

func (c *Client) RefreshPhoneRelay() (string, error) {
	payload := &gmproto.AuthenticationContainer{
		AuthMessage: &gmproto.AuthMessage{
			RequestID:        uuid.NewString(),
			Network:          util.QRNetwork,
			TachyonAuthToken: c.AuthData.TachyonAuthToken,
			ConfigVersion:    util.ConfigMessage,
		},
	}
	res, err := typedHTTPResponse[*gmproto.RefreshPhoneRelayResponse](
		c.makeProtobufHTTPRequest(util.RefreshPhoneRelayURL, payload, ContentTypeProtobuf),
	)
	if err != nil {
		return "", err
	}
	qr, err := c.GenerateQRCodeData(res.GetPairKey())
	if err != nil {
		return "", err
	}
	return qr, nil
}

func (c *Client) GetWebEncryptionKey() (*gmproto.WebEncryptionKeyResponse, error) {
	payload := &gmproto.AuthenticationContainer{
		AuthMessage: &gmproto.AuthMessage{
			RequestID:        uuid.NewString(),
			TachyonAuthToken: c.AuthData.TachyonAuthToken,
			ConfigVersion:    util.ConfigMessage,
		},
	}
	return typedHTTPResponse[*gmproto.WebEncryptionKeyResponse](
		c.makeProtobufHTTPRequest(util.GetWebEncryptionKeyURL, payload, ContentTypeProtobuf),
	)
}

func (c *Client) UnpairBugle() (*gmproto.RevokeRelayPairingResponse, error) {
	if c.AuthData.TachyonAuthToken == nil || c.AuthData.Browser == nil {
		return nil, nil
	}
	payload := &gmproto.RevokeRelayPairingRequest{
		AuthMessage: &gmproto.AuthMessage{
			RequestID:        uuid.NewString(),
			TachyonAuthToken: c.AuthData.TachyonAuthToken,
			ConfigVersion:    util.ConfigMessage,
		},
		Browser: c.AuthData.Browser,
	}
	return typedHTTPResponse[*gmproto.RevokeRelayPairingResponse](
		c.makeProtobufHTTPRequest(util.RevokeRelayPairingURL, payload, ContentTypeProtobuf),
	)
}

func (c *Client) Unpair() (err error) {
	if c.AuthData.Cookies != nil {
		err = c.UnpairGaia()
	} else {
		_, err = c.UnpairBugle()
	}
	return
}
