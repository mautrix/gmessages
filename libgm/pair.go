package libgm

import (
	"crypto/x509"
	"io"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

func (c *Client) RegisterPhoneRelay() (*binary.RegisterPhoneRelayResponse, error) {
	key, err := x509.MarshalPKIXPublicKey(c.AuthData.RefreshKey.GetPublicKey())
	if err != nil {
		return nil, err
	}

	body, err := proto.Marshal(&binary.AuthenticationContainer{
		AuthMessage: &binary.AuthMessage{
			RequestID:     uuid.NewString(),
			Network:       &util.Network,
			ConfigVersion: util.ConfigMessage,
		},
		BrowserDetails: util.BrowserDetailsMessage,
		Data: &binary.AuthenticationContainer_KeyData{
			KeyData: &binary.KeyData{
				EcdsaKeys: &binary.ECDSAKeys{
					Field1:        2,
					EncryptedKeys: key,
				},
			},
		},
	})
	if err != nil {
		return nil, err
	}
	relayResponse, reqErr := c.MakeRelayRequest(util.RegisterPhoneRelayURL, body)
	if reqErr != nil {
		return nil, err
	}
	responseBody, err := io.ReadAll(relayResponse.Body)
	if err != nil {
		return nil, err
	}
	relayResponse.Body.Close()
	res := &binary.RegisterPhoneRelayResponse{}
	err = proto.Unmarshal(responseBody, res)
	if err != nil {
		return nil, err
	}
	return res, err
}

func (c *Client) RefreshPhoneRelay() (string, error) {
	body, err := proto.Marshal(&binary.AuthenticationContainer{
		AuthMessage: &binary.AuthMessage{
			RequestID:        uuid.NewString(),
			Network:          &util.Network,
			TachyonAuthToken: c.AuthData.TachyonAuthToken,
			ConfigVersion:    util.ConfigMessage,
		},
	})
	if err != nil {
		return "", err
	}
	relayResponse, err := c.MakeRelayRequest(util.RefreshPhoneRelayURL, body)
	if err != nil {
		return "", err
	}
	responseBody, err := io.ReadAll(relayResponse.Body)
	defer relayResponse.Body.Close()
	if err != nil {
		return "", err
	}
	res := &binary.RefreshPhoneRelayResponse{}
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

func (c *Client) GetWebEncryptionKey() (*binary.WebEncryptionKeyResponse, error) {
	body, err := proto.Marshal(&binary.AuthenticationContainer{
		AuthMessage: &binary.AuthMessage{
			RequestID:        uuid.NewString(),
			TachyonAuthToken: c.AuthData.TachyonAuthToken,
			ConfigVersion:    util.ConfigMessage,
		},
	})
	if err != nil {
		return nil, err
	}
	webKeyResponse, err := c.MakeRelayRequest(util.GetWebEncryptionKeyURL, body)
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
	return parsedResponse, nil
}

func (c *Client) Unpair() (*binary.RevokeRelayPairingResponse, error) {
	if c.AuthData.TachyonAuthToken == nil || c.AuthData.Browser == nil {
		return nil, nil
	}
	payload, err := proto.Marshal(&binary.RevokeRelayPairing{
		AuthMessage: &binary.AuthMessage{
			RequestID:        uuid.NewString(),
			TachyonAuthToken: c.AuthData.TachyonAuthToken,
			ConfigVersion:    util.ConfigMessage,
		},
		Browser: c.AuthData.Browser,
	})
	if err != nil {
		return nil, err
	}
	revokeResp, err := c.MakeRelayRequest(util.RevokeRelayPairingURL, payload)
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
