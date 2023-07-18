package libgm

import (
	"bytes"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"

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
	c.AuthData.TachyonAuthToken = registered.AuthKeyData.TachyonAuthToken
	go c.rpc.ListenReceiveMessages(false)
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
	c.updateTachyonAuthToken(data.GetTokenData().GetTachyonAuthToken(), data.GetTokenData().GetTTL())
	c.AuthData.Mobile = data.Mobile
	c.AuthData.Browser = data.Browser

	c.triggerEvent(&events.PairSuccessful{PairedData: data})

	err := c.Reconnect()
	if err != nil {
		c.triggerEvent(&events.ListenFatalError{Error: fmt.Errorf("failed to reconnect after pair success: %w", err)})
	}
}

func (c *Client) makeRelayRequest(url string, body []byte) (*http.Response, error) {
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	util.BuildRelayHeaders(req, "application/x-protobuf", "*/*")
	res, reqErr := c.http.Do(req)
	if reqErr != nil {
		return res, reqErr
	}
	return res, nil
}

func (c *Client) RegisterPhoneRelay() (*gmproto.RegisterPhoneRelayResponse, error) {
	key, err := x509.MarshalPKIXPublicKey(c.AuthData.RefreshKey.GetPublicKey())
	if err != nil {
		return nil, err
	}

	body, err := proto.Marshal(&gmproto.AuthenticationContainer{
		AuthMessage: &gmproto.AuthMessage{
			RequestID:     uuid.NewString(),
			Network:       &util.Network,
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
	})
	if err != nil {
		return nil, err
	}
	relayResponse, reqErr := c.makeRelayRequest(util.RegisterPhoneRelayURL, body)
	if reqErr != nil {
		return nil, err
	}
	responseBody, err := io.ReadAll(relayResponse.Body)
	if err != nil {
		return nil, err
	}
	relayResponse.Body.Close()
	res := &gmproto.RegisterPhoneRelayResponse{}
	err = proto.Unmarshal(responseBody, res)
	if err != nil {
		return nil, err
	}
	return res, err
}

func (c *Client) RefreshPhoneRelay() (string, error) {
	body, err := proto.Marshal(&gmproto.AuthenticationContainer{
		AuthMessage: &gmproto.AuthMessage{
			RequestID:        uuid.NewString(),
			Network:          &util.Network,
			TachyonAuthToken: c.AuthData.TachyonAuthToken,
			ConfigVersion:    util.ConfigMessage,
		},
	})
	if err != nil {
		return "", err
	}
	relayResponse, err := c.makeRelayRequest(util.RefreshPhoneRelayURL, body)
	if err != nil {
		return "", err
	}
	responseBody, err := io.ReadAll(relayResponse.Body)
	defer relayResponse.Body.Close()
	if err != nil {
		return "", err
	}
	res := &gmproto.RefreshPhoneRelayResponse{}
	err = proto.Unmarshal(responseBody, res)
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
	body, err := proto.Marshal(&gmproto.AuthenticationContainer{
		AuthMessage: &gmproto.AuthMessage{
			RequestID:        uuid.NewString(),
			TachyonAuthToken: c.AuthData.TachyonAuthToken,
			ConfigVersion:    util.ConfigMessage,
		},
	})
	if err != nil {
		return nil, err
	}
	webKeyResponse, err := c.makeRelayRequest(util.GetWebEncryptionKeyURL, body)
	if err != nil {
		return nil, err
	}
	responseBody, err := io.ReadAll(webKeyResponse.Body)
	defer webKeyResponse.Body.Close()
	if err != nil {
		return nil, err
	}
	parsedResponse := &gmproto.WebEncryptionKeyResponse{}
	err = proto.Unmarshal(responseBody, parsedResponse)
	if err != nil {
		return nil, err
	}
	return parsedResponse, nil
}

func (c *Client) Unpair() (*gmproto.RevokeRelayPairingResponse, error) {
	if c.AuthData.TachyonAuthToken == nil || c.AuthData.Browser == nil {
		return nil, nil
	}
	payload, err := proto.Marshal(&gmproto.RevokeRelayPairingRequest{
		AuthMessage: &gmproto.AuthMessage{
			RequestID:        uuid.NewString(),
			TachyonAuthToken: c.AuthData.TachyonAuthToken,
			ConfigVersion:    util.ConfigMessage,
		},
		Browser: c.AuthData.Browser,
	})
	if err != nil {
		return nil, err
	}
	revokeResp, err := c.makeRelayRequest(util.RevokeRelayPairingURL, payload)
	if err != nil {
		return nil, err
	}
	responseBody, err := io.ReadAll(revokeResp.Body)
	defer revokeResp.Body.Close()
	if err != nil {
		return nil, err
	}
	parsedResponse := &gmproto.RevokeRelayPairingResponse{}
	err = proto.Unmarshal(responseBody, parsedResponse)
	if err != nil {
		return nil, err
	}
	return parsedResponse, nil
}
