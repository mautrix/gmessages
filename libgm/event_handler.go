package libgm

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"go.mau.fi/mautrix-gmessages/libgm/pblite"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
)

func (r *RPC) deduplicateHash(hash [32]byte) bool {
	const recentUpdatesLen = len(r.recentUpdates)
	for i := r.recentUpdatesPtr + recentUpdatesLen - 1; i >= r.recentUpdatesPtr; i-- {
		if r.recentUpdates[i%recentUpdatesLen] == hash {
			return true
		}
	}
	r.recentUpdates[r.recentUpdatesPtr] = hash
	r.recentUpdatesPtr = (r.recentUpdatesPtr + 1) % recentUpdatesLen
	return false
}

func (r *RPC) deduplicateUpdate(response *pblite.Response) bool {
	if response.Data.RawDecrypted != nil {
		contentHash := sha256.Sum256(response.Data.RawDecrypted)
		if r.deduplicateHash(contentHash) {
			r.client.Logger.Trace().Hex("data_hash", contentHash[:]).Msg("Ignoring duplicate update")
			return true
		}
		if r.client.Logger.Trace().Enabled() {
			r.client.Logger.Trace().
				Str("proto_name", string(response.Data.Decrypted.ProtoReflect().Descriptor().FullName())).
				Str("data", base64.StdEncoding.EncodeToString(response.Data.RawDecrypted)).
				Hex("data_hash", contentHash[:]).
				Msg("Got event")
		}
	}
	return false
}

func (r *RPC) HandleRPCMsg(msgArr []interface{}) {
	response, decodeErr := pblite.DecodeAndDecryptInternalMessage(msgArr, r.client.authData.Cryptor)
	if decodeErr != nil {
		r.client.Logger.Error().Err(fmt.Errorf("failed to deserialize response %s", msgArr)).Msg("rpc deserialize msg err")
		return
	}
	//r.client.Logger.Debug().Any("byteLength", len(data)).Any("unmarshaled", response).Any("raw", string(data)).Msg("RPC Msg")
	if response == nil {
		r.client.Logger.Error().Err(fmt.Errorf("response data was nil %s", msgArr)).Msg("rpc msg data err")
		return
	}
	//r.client.Logger.Debug().Any("response", response).Msg("decrypted & decoded response")
	_, waitingForResponse := r.client.sessionHandler.requests[response.Data.RequestId]

	//r.client.Logger.Info().Any("raw", msgArr).Msg("Got msg")
	//r.client.Logger.Debug().Any("waiting", waitingForResponse).Msg("got request! waiting?")
	r.client.sessionHandler.addResponseAck(response.ResponseId)
	if waitingForResponse {
		if response.Data.Decrypted != nil && r.client.Logger.Trace().Enabled() {
			r.client.Logger.Trace().
				Str("proto_name", string(response.Data.Decrypted.ProtoReflect().Descriptor().FullName())).
				Str("data", base64.StdEncoding.EncodeToString(response.Data.RawDecrypted)).
				Msg("Got response")
		}
		r.client.sessionHandler.respondToRequestChannel(response)
	} else {
		switch response.BugleRoute {
		case binary.BugleRoute_PairEvent:
			go r.client.handlePairingEvent(response)
		case binary.BugleRoute_DataEvent:
			if r.skipCount > 0 {
				r.skipCount--
				r.client.Logger.Debug().
					Any("action", response.Data.Action).
					Any("toSkip", r.skipCount).
					Msg("Skipped DataEvent")
				if response.Data.Decrypted != nil {
					r.client.Logger.Trace().
						Str("proto_name", string(response.Data.Decrypted.ProtoReflect().Descriptor().FullName())).
						Str("data", base64.StdEncoding.EncodeToString(response.Data.RawDecrypted)).
						Msg("Skipped event data")
				}
				return
			}
			r.client.handleUpdatesEvent(response)
		default:
			r.client.Logger.Debug().Any("res", response).Msg("Got unknown bugleroute")
		}
	}

}

func (r *RPC) tryUnmarshalJSON(jsonData []byte, msgArr *[]interface{}) error {
	err := json.Unmarshal(jsonData, &msgArr)
	return err
}

func (r *RPC) HandleByLength(data []byte) {
	r.client.Logger.Debug().Any("byteLength", len(data)).Any("corrupt raw", string(data)).Msg("RPC Corrupt json")
}
