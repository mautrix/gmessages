package libgm

import (
	"encoding/json"
	"fmt"
	"time"

	"golang.org/x/exp/slices"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/crypto"
	"go.mau.fi/mautrix-gmessages/libgm/payload"
	"go.mau.fi/mautrix-gmessages/libgm/pblite"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

type Response struct {
	client        *Client
	ResponseID    string
	RoutingOpCode int64
	Data          *binary.EncodedResponse // base64 encoded (decode -> protomessage)

	StartExecute  string
	FinishExecute string
	DevicePair    *DevicePair
}

type SessionHandler struct {
	client   *Client
	requests map[string]map[int64]*ResponseChan

	ackMap    []string
	ackTicker *time.Ticker

	sessionID string

	responseTimeout time.Duration
}

func (s *SessionHandler) SetResponseTimeout(milliSeconds int) {
	s.responseTimeout = time.Duration(milliSeconds) * time.Millisecond
}

func (s *SessionHandler) ResetSessionID() {
	s.sessionID = util.RandomUUIDv4()
}

func (c *Client) createAndSendRequest(instructionId int64, ttl int64, newSession bool, encryptedProtoMessage protoreflect.Message) (string, error) {
	requestId := util.RandomUUIDv4()
	instruction, ok := c.instructions.GetInstruction(instructionId)
	if !ok {
		return "", fmt.Errorf("failed to get instruction: %v does not exist", instructionId)
	}

	if newSession {
		requestId = c.sessionHandler.sessionID
	}

	var encryptedData []byte
	var encryptErr error
	if encryptedProtoMessage != nil {
		encryptedData, encryptErr = c.EncryptPayloadData(encryptedProtoMessage)
		if encryptErr != nil {
			return "", fmt.Errorf("failed to encrypt payload data for opcode: %v", instructionId)
		}
		c.Logger.Info().Any("encryptedData", encryptedData).Msg("Sending request with encrypted data")
	}

	encodedData := payload.NewEncodedPayload(requestId, instruction.Opcode, encryptedData, c.sessionHandler.sessionID)
	encodedStr, encodeErr := crypto.EncodeProtoB64(encodedData)
	if encodeErr != nil {
		panic(fmt.Errorf("Failed to encode data: %w", encodeErr))
	}
	messageData := payload.NewMessageData(requestId, encodedStr, instruction.RoutingOpCode, instruction.MsgType)
	authMessage := payload.NewAuthData(requestId, c.rpcKey, &binary.Date{Year: 2023, Seq1: 6, Seq2: 8, Seq3: 4, Seq4: 6})
	sendMessage := payload.NewSendMessage(c.devicePair.Mobile, messageData, authMessage, ttl)

	sentRequestID, reqErr := c.sessionHandler.completeSendMessage(encodedData.RequestID, instruction.Opcode, sendMessage)
	if reqErr != nil {
		return "", fmt.Errorf("failed to send message request for opcode: %v", instructionId)
	}
	return sentRequestID, nil
}

func (s *SessionHandler) completeSendMessage(requestId string, opCode int64, msg *binary.SendMessage) (string, error) {
	jsonData, err := s.toJSON(msg.ProtoReflect())
	if err != nil {
		return "", err
	}
	//s.client.Logger.Debug().Any("payload", string(jsonData)).Msg("Sending message request")
	s.addRequestToChannel(requestId, opCode)
	_, reqErr := s.client.rpc.sendMessageRequest(util.SEND_MESSAGE, jsonData)
	if reqErr != nil {
		return "", reqErr
	}
	return requestId, nil
}

func (s *SessionHandler) toJSON(message protoreflect.Message) ([]byte, error) {
	interfaceArr, err := pblite.Serialize(message)
	if err != nil {
		return nil, err
	}
	jsonData, jsonErr := json.Marshal(interfaceArr)
	if jsonErr != nil {
		return nil, jsonErr
	}
	return jsonData, nil
}

func (s *SessionHandler) addResponseAck(responseId string) {
	hasResponseId := slices.Contains(s.ackMap, responseId)
	if !hasResponseId {
		s.ackMap = append(s.ackMap, responseId)
	}
}

func (s *SessionHandler) startAckInterval() {
	if s.ackTicker != nil {
		s.ackTicker.Stop()
	}
	ticker := time.NewTicker(5 * time.Second)
	s.ackTicker = ticker
	go func() {
		for range ticker.C {
			s.sendAckRequest()
		}
	}()
}

func (s *SessionHandler) sendAckRequest() {
	if len(s.ackMap) <= 0 {
		return
	}
	reqId := util.RandomUUIDv4()
	ackMessagePayload := &binary.AckMessagePayload{
		AuthData: &binary.AuthMessage{
			RequestID: reqId,
			RpcKey:    s.client.rpcKey,
			Date:      &binary.Date{Year: 2023, Seq1: 6, Seq2: 8, Seq3: 4, Seq4: 6},
		},
		EmptyArr: &binary.EmptyArr{},
		NoClue:   nil,
	}
	dataArray, err := pblite.Serialize(ackMessagePayload.ProtoReflect())
	if err != nil {
		panic(err)
	}
	ackMessages := make([][]interface{}, 0)
	for _, reqId := range s.ackMap {
		ackMessageData := &binary.AckMessageData{RequestID: reqId, Device: s.client.devicePair.Browser}
		ackMessageDataArr, err := pblite.Serialize(ackMessageData.ProtoReflect())
		if err != nil {
			panic(err)
		}
		ackMessages = append(ackMessages, ackMessageDataArr)
		s.ackMap = util.RemoveFromSlice(s.ackMap, reqId)
	}
	dataArray = append(dataArray, ackMessages)
	jsonData, jsonErr := json.Marshal(dataArray)
	if jsonErr != nil {
		panic(err)
	}
	_, err = s.client.rpc.sendMessageRequest(util.ACK_MESSAGES, jsonData)
	if err != nil {
		panic(err)
	}
	s.client.Logger.Debug().Msg("[ACK] Sent Request")
}

func (s *SessionHandler) NewResponse(response *binary.RPCResponse) (*Response, error) {
	//s.client.Logger.Debug().Any("rpcResponse", response).Msg("Raw rpc response")
	decodedData, err := crypto.DecodeEncodedResponse(response.Data.EncodedData)
	if err != nil {
		panic(err)
		return nil, err
	}
	return &Response{
		client:        s.client,
		ResponseID:    response.Data.RequestID,
		RoutingOpCode: response.Data.RoutingOpCode,
		StartExecute:  response.Data.Ts1,
		FinishExecute: response.Data.Ts2,
		DevicePair: &DevicePair{
			Mobile:  response.Data.Mobile,
			Browser: response.Data.Browser,
		},
		Data: decodedData,
	}, nil
}

func (r *Response) decryptData() (proto.Message, error) {
	if r.Data.EncryptedData != nil {
		instruction, ok := r.client.instructions.GetInstruction(r.Data.Opcode)
		if !ok {
			return nil, fmt.Errorf("failed to decrypt data for unknown opcode: %v", r.Data.Opcode)
		}
		decryptedBytes, errDecrypt := instruction.cryptor.Decrypt(r.Data.EncryptedData)
		if errDecrypt != nil {
			return nil, errDecrypt
		}
		//os.WriteFile("opcode_"+strconv.Itoa(int(instruction.Opcode))+".bin", decryptedBytes, os.ModePerm)

		protoMessageData := instruction.DecryptedProtoMessage.ProtoReflect().Type().New().Interface()
		decodeProtoErr := binary.DecodeProtoMessage(decryptedBytes, protoMessageData)
		if decodeProtoErr != nil {
			return nil, decodeProtoErr
		}

		return protoMessageData, nil
	}
	return nil, fmt.Errorf("no encrypted data to decrypt for requestID: %s", r.Data.RequestID)
}
