package libgm

import (
	"encoding/base64"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
)

func (c *Client) handleSeperateOpCode(msgData *binary.MessageData) {
	decodedBytes, err := base64.StdEncoding.DecodeString(msgData.EncodedData)
	if err != nil {
		panic(err)
	}
	switch msgData.RoutingOpCode {
	case 14: // paired successful
		decodedData := &binary.Container{}
		err = binary.DecodeProtoMessage(decodedBytes, decodedData)
		if err != nil {
			panic(err)
		}
		if decodedData.UnpairDeviceData != nil {
			c.Logger.Warn().Any("data", decodedData).Msg("Unpaired?")
			return
		}
		// TODO unpairing
		c.Logger.Debug().Any("data", decodedData).Msg("Paired device decoded data")
		if c.pairer != nil {
			c.pairer.pairCallback(decodedData)
		} else {
			c.Logger.Warn().Msg("No pairer to receive callback")
		}
	default:
		decodedData := &binary.EncodedResponse{}
		err = binary.DecodeProtoMessage(decodedBytes, decodedData)
		if err != nil {
			panic(err)
		}
		if (decodedData.Sub && decodedData.Third != 0) && decodedData.EncryptedData != nil {
			bugleData := &binary.BugleBackendService{}
			err = c.cryptor.DecryptAndDecodeData(decodedData.EncryptedData, bugleData)
			if err != nil {
				panic(err)
			}
			c.handleBugleOpCode(bugleData)
		}
	}
}
