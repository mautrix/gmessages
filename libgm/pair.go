package textgapi

import (
	"io"
	"log"
	"time"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/crypto"
	"go.mau.fi/mautrix-gmessages/libgm/payload"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

type Pairer struct {
	client     *Client
	KeyData    *crypto.JWK
	ticker     *time.Ticker
	tickerTime time.Duration
	qrCodePx   int
	pairingKey []byte
}

/*
refreshQrCodeTime is the interval to refresh the qr code in seconds, this is usually 20 seconds.
*/
func (c *Client) NewPairer(keyData *crypto.JWK, refreshQrCodeTime int) (*Pairer, error) {
	if keyData == nil {
		var err error
		keyData, err = crypto.GenerateECDSA_P256_JWK()
		if err != nil {
			c.Logger.Error().Any("data", keyData).Msg(err.Error())
			return nil, err
		}
	}
	p := &Pairer{
		client:     c,
		KeyData:    keyData,
		qrCodePx:   214,
		tickerTime: time.Duration(refreshQrCodeTime) * time.Second,
	}
	c.pairer = p
	return p, nil
}

func (p *Pairer) SetQRCodePx(pixels int) {
	p.qrCodePx = pixels
}

func (p *Pairer) RegisterPhoneRelay() (*binary.RegisterPhoneRelayResponse, error) {
	body, _, err := payload.RegisterPhoneRelay(p.KeyData)
	if err != nil {
		p.client.Logger.Err(err)
		return &binary.RegisterPhoneRelayResponse{}, err
	}
	//p.client.Logger.Debug().Any("keyByteLength", len(jsonPayload.EcdsaKeysContainer.EcdsaKeys.EncryptedKeys)).Any("json", jsonPayload).Any("base64", body).Msg("RegisterPhoneRelay Payload")
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
	err3 := binary.DecodeProtoMessage(responseBody, res)
	if err3 != nil {
		return nil, err3
	}
	p.pairingKey = res.GetPairingKey()
	qrCode, qrErr := p.GenerateQRCode(p.qrCodePx)
	if qrErr != nil {
		return nil, qrErr
	}
	p.client.triggerEvent(qrCode)
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
	body, _, err := payload.RefreshPhoneRelay(p.client.rpcKey)
	if err != nil {
		p.client.Logger.Err(err).Msg("refresh phone relay err")
		return
	}
	//p.client.Logger.Debug().Any("keyByteLength", len(jsonPayload.PhoneRelay.RpcKey)).Any("json", jsonPayload).Any("base64", body).Msg("RefreshPhoneRelay Payload")
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
	err3 := binary.DecodeProtoMessage(responseBody, res)
	if err3 != nil {
		p.client.Logger.Err(err3)
	}
	p.pairingKey = res.GetPairKey()
	p.client.Logger.Debug().Any("res", res).Msg("RefreshPhoneRelayResponse")
	qrCode, qrErr := p.GenerateQRCode(p.qrCodePx)
	if qrErr != nil {
		log.Fatal(qrErr)
	}
	p.client.triggerEvent(qrCode)
}

func (p *Pairer) GetWebEncryptionKey() {
	body, _, err2 := payload.GetWebEncryptionKey(p.client.rpc.webAuthKey)
	if err2 != nil {
		p.client.Logger.Err(err2).Msg("web encryption key err")
		return
	}
	//p.client.Logger.Debug().Any("keyByteLength", len(rawData.PhoneRelay.RpcKey)).Any("json", rawData).Any("base64", body).Msg("GetWebEncryptionKey Payload")
	webKeyResponse, reqErr := p.client.MakeRelayRequest(util.GET_WEB_ENCRYPTION_KEY, body)
	if reqErr != nil {
		p.client.Logger.Err(reqErr).Msg("Web encryption key request err")
	}
	responseBody, err2 := io.ReadAll(webKeyResponse.Body)
	defer webKeyResponse.Body.Close()
	if err2 != nil {
		p.client.Logger.Err(err2).Msg("Web encryption key read response err")
		return
	}
	//p.client.Logger.Debug().Any("responseLength", len(responseBody)).Any("raw", responseBody).Msg("Response Body Length")
	parsedResponse := &binary.WebEncryptionKeyResponse{}
	err2 = binary.DecodeProtoMessage(responseBody, parsedResponse)
	if err2 != nil {
		p.client.Logger.Err(err2).Msg("Parse webkeyresponse into proto struct error")
	}
	key := crypto.EncodeBase64Standard(p.client.rpc.webAuthKey)
	p.ticker.Stop()
	reconnectErr := p.client.Reconnect(key)
	if reconnectErr != nil {
		log.Fatal(reconnectErr)
	}
}

func (p *Pairer) pairCallback(pairData *binary.Container) {
	p.client.rpc.webAuthKey = pairData.PairDeviceData.WebAuthKeyData.WebAuthKey
	p.client.ttl = pairData.PairDeviceData.WebAuthKeyData.ValidFor
	p.client.devicePair = &DevicePair{Mobile: pairData.PairDeviceData.Mobile, Browser: pairData.PairDeviceData.Browser}
	p.client.pairer.GetWebEncryptionKey()
}
