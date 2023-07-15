package libgm

import (
	"io"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/crypto"
	"go.mau.fi/mautrix-gmessages/libgm/events"
	"go.mau.fi/mautrix-gmessages/libgm/payload"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

type Pairer struct {
	client     *Client
	KeyData    *crypto.JWK
	ticker     *time.Ticker
	tickerTime time.Duration
	pairingKey []byte
}

/*
refreshQrCodeTime is the interval to refresh the qr code in seconds, this is usually 20 seconds.
*/
func (c *Client) NewPairer(keyData *crypto.JWK, refreshQrCodeTime int) (*Pairer, error) {
	if keyData == nil {
		var err error
		keyData, err = crypto.GenerateECDSAKey()
		c.updateJWK(keyData)
		if err != nil {
			c.Logger.Error().Any("data", keyData).Msg(err.Error())
			return nil, err
		}
	}
	p := &Pairer{
		client:     c,
		KeyData:    keyData,
		tickerTime: time.Duration(refreshQrCodeTime) * time.Second,
	}
	c.pairer = p
	return p, nil
}

func (p *Pairer) RegisterPhoneRelay() (*binary.RegisterPhoneRelayResponse, error) {
	body, _, err := payload.RegisterPhoneRelay(p.KeyData)
	if err != nil {
		p.client.Logger.Err(err)
		return &binary.RegisterPhoneRelayResponse{}, err
	}
	relayResponse, reqErr := p.client.MakeRelayRequest(util.REGISTER_PHONE_RELAY, body)
	if reqErr != nil {
		p.client.Logger.Err(reqErr)
		return nil, err
	}
	responseBody, err2 := io.ReadAll(relayResponse.Body)
	if err2 != nil {
		return nil, err2
	}
	relayResponse.Body.Close()
	res := &binary.RegisterPhoneRelayResponse{}
	err3 := proto.Unmarshal(responseBody, res)
	if err3 != nil {
		return nil, err3
	}
	p.pairingKey = res.GetPairingKey()
	p.client.Logger.Debug().Any("response", res).Msg("Registerphonerelay response")
	url, qrErr := p.GenerateQRCodeData()
	if qrErr != nil {
		return nil, qrErr
	}
	p.client.triggerEvent(&events.QR{URL: url})
	p.startRefreshRelayTask()
	return res, err
}

func (p *Pairer) startRefreshRelayTask() {
	if p.ticker != nil {
		p.ticker.Stop()
	}
	ticker := time.NewTicker(30 * time.Second)
	p.ticker = ticker
	go func() {
		for range ticker.C {
			p.RefreshPhoneRelay()
		}
	}()
}

func (p *Pairer) RefreshPhoneRelay() {
	body, err := proto.Marshal(&binary.AuthenticationContainer{
		AuthMessage: &binary.AuthMessage{
			RequestID:        util.RandomUUIDv4(),
			Network:          &payload.Network,
			TachyonAuthToken: p.client.authData.TachyonAuthToken,
			ConfigVersion:    payload.ConfigMessage,
		},
	})
	if err != nil {
		p.client.Logger.Err(err).Msg("refresh phone relay err")
		return
	}
	relayResponse, reqErr := p.client.MakeRelayRequest(util.REFRESH_PHONE_RELAY, body)
	if reqErr != nil {
		p.client.Logger.Err(reqErr).Msg("refresh phone relay err")
	}
	responseBody, err2 := io.ReadAll(relayResponse.Body)
	defer relayResponse.Body.Close()
	if err2 != nil {
		p.client.Logger.Err(err2).Msg("refresh phone relay err")
	}
	p.client.Logger.Debug().Any("responseLength", len(responseBody)).Msg("Response Body Length")
	res := &binary.RefreshPhoneRelayResponse{}
	err3 := proto.Unmarshal(responseBody, res)
	if err3 != nil {
		p.client.Logger.Err(err3)
	}
	p.pairingKey = res.GetPairKey()
	p.client.Logger.Debug().Any("res", res).Msg("RefreshPhoneRelayResponse")
	url, qrErr := p.GenerateQRCodeData()
	if qrErr != nil {
		panic(qrErr)
	}
	p.client.triggerEvent(&events.QR{URL: url})
}

func (c *Client) GetWebEncryptionKey() (*binary.WebEncryptionKeyResponse, error) {
	body, err := proto.Marshal(&binary.AuthenticationContainer{
		AuthMessage: &binary.AuthMessage{
			RequestID:        uuid.NewString(),
			TachyonAuthToken: c.authData.TachyonAuthToken,
			ConfigVersion:    payload.ConfigMessage,
		},
	})
	if err != nil {
		return nil, err
	}
	webKeyResponse, err := c.MakeRelayRequest(util.GET_WEB_ENCRYPTION_KEY, body)
	if err != nil {
		return nil, err
	}
	responseBody, err := io.ReadAll(webKeyResponse.Body)
	defer webKeyResponse.Body.Close()
	if err != nil {
		return nil, err
	}
	parsedResponse := &binary.WebEncryptionKeyResponse{}
	err = proto.Unmarshal(responseBody, parsedResponse)
	if err != nil {
		return nil, err
	}
	if c.pairer != nil {
		if c.pairer.ticker != nil {
			c.pairer.ticker.Stop()
		}
	}
	return parsedResponse, nil
}

func (c *Client) Unpair() (*binary.RevokeRelayPairingResponse, error) {
	if c.authData.TachyonAuthToken == nil || c.authData.DevicePair == nil || c.authData.DevicePair.Browser == nil {
		return nil, nil
	}
	payload, err := proto.Marshal(&binary.RevokeRelayPairing{
		AuthMessage: &binary.AuthMessage{
			RequestID:        uuid.NewString(),
			TachyonAuthToken: c.authData.TachyonAuthToken,
			ConfigVersion:    payload.ConfigMessage,
		},
		Browser: c.authData.DevicePair.Browser,
	})
	if err != nil {
		return nil, err
	}
	revokeResp, err := c.MakeRelayRequest(util.REVOKE_RELAY_PAIRING, payload)
	if err != nil {
		return nil, err
	}
	responseBody, err := io.ReadAll(revokeResp.Body)
	defer revokeResp.Body.Close()
	if err != nil {
		return nil, err
	}
	parsedResponse := &binary.RevokeRelayPairingResponse{}
	err = proto.Unmarshal(responseBody, parsedResponse)
	if err != nil {
		return nil, err
	}
	return parsedResponse, nil
}
