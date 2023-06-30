package payload

import (
	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/crypto"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

func RegisterPhoneRelay(jwk *crypto.JWK) ([]byte, *binary.Container, error) {
	id := util.RandomUUIDv4()
	decodedPrivateKey, err2 := jwk.PrivKeyB64Bytes()
	if err2 != nil {
		return nil, nil, err2
	}
	jwk.PrivateBytes = decodedPrivateKey
	uncompressedPublicKey, err3 := jwk.UncompressPubKey()
	if err3 != nil {
		return nil, nil, err3
	}
	var emptyByteArray []byte
	crypto.EncodeValues(&emptyByteArray, crypto.SequenceOne)
	crypto.EncodeValues(&emptyByteArray, crypto.SequenceTwo)

	var copiedByteArray []byte
	copiedByteArray = crypto.AppendByteSequence(copiedByteArray, emptyByteArray, uncompressedPublicKey)
	for _, value := range uncompressedPublicKey {
		copiedByteArray = crypto.HelperAppendBytes(copiedByteArray, value)
	}

	var emptyByteArray2 []byte
	emptyByteArray2 = crypto.AppendBytes(emptyByteArray2, copiedByteArray[0:])

	payloadData := &binary.Container{
		PhoneRelay: &binary.PhoneRelayBody{
			ID:    id,
			Bugle: "Bugle",
			Date: &binary.Date{
				Year: 2023,
				Seq1: 6,
				Seq2: 8,
				Seq3: 4,
				Seq4: 6,
			},
		},
		BrowserDetails: &binary.BrowserDetails{
			UserAgent: util.UserAgent,
			SomeInt:   2,
			SomeBool:  true,
			Os:        util.OS,
		},
		PairDeviceData: &binary.PairDeviceData{
			EcdsaKeys: &binary.ECDSAKeys{
				ProtoVersion:  2,
				EncryptedKeys: emptyByteArray2,
			},
		},
	}
	encoded, err4 := binary.EncodeProtoMessage(payloadData)
	if err4 != nil {
		return nil, payloadData, err4
	}
	return encoded, payloadData, nil
}
