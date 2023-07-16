package payload

import (
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/crypto"
)

func RegisterPhoneRelay(jwk *crypto.JWK) ([]byte, *binary.AuthenticationContainer, error) {
	key, err := jwk.MarshalX509PublicKey()
	if err != nil {
		return nil, nil, err
	}

	payloadData := &binary.AuthenticationContainer{
		AuthMessage: &binary.AuthMessage{
			RequestID:     uuid.NewString(),
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
