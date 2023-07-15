package payload

import (
	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/crypto"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

func RegisterPhoneRelay(jwk *crypto.JWK) ([]byte, *binary.AuthenticationContainer, error) {
	id := util.RandomUUIDv4()

	key, err := jwk.MarshalX509PublicKey()
	if err != nil {
		return nil, nil, err
	}

	payloadData := &binary.AuthenticationContainer{
		AuthMessage: &binary.AuthMessage{
			RequestID:     id,
			Network:       &Network,
			ConfigVersion: ConfigMessage,
		},
		BrowserDetails: BrowserDetailsMessage,
		Data: &binary.AuthenticationContainer_KeyData{
			KeyData: &binary.KeyData{
				EcdsaKeys: &binary.ECDSAKeys{
					Field1:        2,
					EncryptedKeys: key,
				},
			},
		},
	}
	encoded, err4 := proto.Marshal(payloadData)
	if err4 != nil {
		return nil, payloadData, err4
	}
	return encoded, payloadData, nil
}
