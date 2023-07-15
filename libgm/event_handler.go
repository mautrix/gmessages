package libgm

import (
	"crypto/sha256"
	"encoding/base64"

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

func (r *RPC) logContent(res *pblite.Response) {
	if r.client.Logger.Trace().Enabled() && res.Data.Decrypted != nil {
		r.client.Logger.Trace().
			Str("proto_name", string(res.Data.Decrypted.ProtoReflect().Descriptor().FullName())).
			Str("data", base64.StdEncoding.EncodeToString(res.Data.RawDecrypted)).
			Msg("Got event")
	}
}

func (r *RPC) deduplicateUpdate(response *pblite.Response) bool {
	if response.Data.RawDecrypted != nil {
		contentHash := sha256.Sum256(response.Data.RawDecrypted)
		if r.deduplicateHash(contentHash) {
			r.client.Logger.Trace().Hex("data_hash", contentHash[:]).Msg("Ignoring duplicate update")
			return true
		}
		r.logContent(response)
	}
	return false
}

func (r *RPC) HandleRPCMsg(msg *binary.InternalMessage) {
	response, decodeErr := pblite.DecryptInternalMessage(msg, r.client.authData.Cryptor)
	if decodeErr != nil {
		r.client.Logger.Error().Err(decodeErr).Msg("rpc decrypt msg err")
		return
	}
	if response == nil {
		r.client.Logger.Error().Msg("nil response in rpc handler")
		return
	}
	_, waitingForResponse := r.client.sessionHandler.requests[response.Data.RequestId]

	r.client.sessionHandler.addResponseAck(response.ResponseId)
	if waitingForResponse {
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

func (r *RPC) HandleByLength(data []byte) {
	r.client.Logger.Debug().Any("byteLength", len(data)).Any("corrupt raw", string(data)).Msg("RPC Corrupt json")
}
