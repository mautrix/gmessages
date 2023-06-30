package libgm

import (
	"encoding/json"
	"fmt"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/pblite"
)

func (r *RPC) HandleRPCMsg(msgArr []interface{}) {
	/*
		if data[0] == 44 {  // ','
			data = data[1:]
		}

		var msgArr []interface{}
		err := r.tryUnmarshalJSON(data, &msgArr)
		if err != nil {
			r.client.Logger.Error().Err(fmt.Errorf("got invalid json string %s", string(data))).Msg("rpc msg err")
			r.HandleByLength(data)
			return
		}
	*/
	response := &binary.RPCResponse{}
	deserializeErr := pblite.Deserialize(msgArr, response.ProtoReflect())
	if deserializeErr != nil {
		r.client.Logger.Error().Err(deserializeErr).Msg("meow")
		r.client.Logger.Error().Err(fmt.Errorf("failed to deserialize response %s", msgArr)).Msg("rpc deserialize msg err")
		return
	}
	//r.client.Logger.Debug().Any("byteLength", len(data)).Any("unmarshaled", response).Any("raw", string(data)).Msg("RPC Msg")
	if response.Data == nil {
		r.client.Logger.Error().Err(fmt.Errorf("Response data was nil %s", msgArr)).Msg("rpc msg data err")
		return
	}
	if response.Data.RoutingOpCode == 19 {
		parsedResponse, failedParse := r.client.sessionHandler.NewResponse(response)
		if failedParse != nil {
			panic(failedParse)
		}
		//hasBody := parsedResponse.Data.EncryptedData == nil
		//r.client.Logger.Info().Any("msgData", parsedResponse).Msg("Got event!")
		r.client.sessionHandler.addResponseAck(parsedResponse.ResponseId)
		_, waitingForResponse := r.client.sessionHandler.requests[parsedResponse.Data.RequestId]
		//log.Println(fmt.Sprintf("%v %v %v %v %v %v %v", parsedResponse.RoutingOpCode, parsedResponse.Data.Opcode, parsedResponse.Data.Sub, parsedResponse.Data.Third, parsedResponse.Data.Field9, hasBody, waitingForResponse))
		//r.client.Logger.Debug().Any("waitingForResponse?", waitingForResponse).Msg("Got rpc response from server")
		if parsedResponse.Data.Opcode == 16 || waitingForResponse {
			if waitingForResponse {
				r.client.sessionHandler.respondToRequestChannel(parsedResponse)
				return
			}
			if parsedResponse.Data.Opcode == 16 {
				r.client.handleEventOpCode(parsedResponse)
			}
		} else {

		}
	} else {
		r.client.handleSeperateOpCode(response.Data)
	}
}

func (r *RPC) tryUnmarshalJSON(jsonData []byte, msgArr *[]interface{}) error {
	err := json.Unmarshal(jsonData, &msgArr)
	return err
}

func (r *RPC) HandleByLength(data []byte) {
	r.client.Logger.Debug().Any("byteLength", len(data)).Any("corrupt raw", string(data)).Msg("RPC Corrupt json")
}
