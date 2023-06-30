package textgapi

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/crypto"
)

const (
	ROUTING_OPCODE   = 19
	MSG_TYPE_TWO     = 2
	MSG_TYPE_SIXTEEN = 16

	/*
		Session
	*/
	PREPARE_NEW_SESSION_OPCODE = 31
	NEW_SESSION_OPCODE         = 16

	/*
		Conversation
	*/
	LIST_CONVERSATIONS          = 1
	SET_ACTIVE_CONVERSATION     = 22
	OPEN_CONVERSATION           = 21
	FETCH_MESSAGES_CONVERSATION = 2
	SEND_TEXT_MESSAGE           = 3
)

type Instruction struct {
	cryptor               *crypto.Cryptor
	RoutingOpCode         int64
	Opcode                int64
	MsgType               int64
	EncryptedData         []byte
	DecryptedProtoMessage proto.Message
	ExpectedResponses     int64                                            // count expected responses
	ProcessResponses      func(responses []*Response) (interface{}, error) // function that decodes & decrypts the slice into appropriate response
}

func (c *Client) EncryptPayloadData(message protoreflect.Message) ([]byte, error) {
	protoBytes, err1 := binary.EncodeProtoMessage(message.Interface())
	if err1 != nil {
		return nil, err1
	}
	encryptedBytes, err := c.cryptor.Encrypt(protoBytes)
	if err != nil {
		return nil, err
	}
	return encryptedBytes, nil
}

type Instructions struct {
	data map[int64]*Instruction
}

func NewInstructions(cryptor *crypto.Cryptor) *Instructions {
	return &Instructions{
		data: map[int64]*Instruction{
			PREPARE_NEW_SESSION_OPCODE: {cryptor, ROUTING_OPCODE, PREPARE_NEW_SESSION_OPCODE, MSG_TYPE_TWO, nil, &binary.PrepareNewSession{}, 1, nil},
			NEW_SESSION_OPCODE:         {cryptor, ROUTING_OPCODE, NEW_SESSION_OPCODE, MSG_TYPE_TWO, nil, &binary.NewSession{}, 2, nil},        // create new session
			LIST_CONVERSATIONS:         {cryptor, ROUTING_OPCODE, LIST_CONVERSATIONS, MSG_TYPE_SIXTEEN, nil, &binary.Conversations{}, 1, nil}, // list conversations

			//22: {cryptor,19,22,2,nil,nil}, // SET ACTIVE SESSION WINDOW
			OPEN_CONVERSATION:           {cryptor, ROUTING_OPCODE, OPEN_CONVERSATION, MSG_TYPE_TWO, nil, nil, 2, nil},                                       // open conversation
			FETCH_MESSAGES_CONVERSATION: {cryptor, ROUTING_OPCODE, FETCH_MESSAGES_CONVERSATION, MSG_TYPE_TWO, nil, &binary.FetchMessagesResponse{}, 1, nil}, // fetch messages in convo
			SEND_TEXT_MESSAGE:           {cryptor, ROUTING_OPCODE, SEND_TEXT_MESSAGE, MSG_TYPE_TWO, nil, &binary.SendMessageResponse{}, 1, nil},
			//3: {cryptor,19,3,2,nil,&binary.SendMessageResponse{}}, // send text message
		},
	}
}

func (i *Instructions) GetInstruction(key int64) (*Instruction, bool) {
	instruction, ok := i.data[key]
	return instruction, ok
}
