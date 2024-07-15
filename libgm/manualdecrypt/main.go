package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"

	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-gmessages/libgm/crypto"
	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
	"go.mau.fi/mautrix-gmessages/libgm/pblite"
)

func must[T any](t T, err error) T {
	if err != nil {
		panic(err)
	}
	return t
}

func mustNoReturn(err error) {
	if err != nil {
		panic(err)
	}
}

var requestType = map[gmproto.ActionType]proto.Message{
	gmproto.ActionType_LIST_CONVERSATIONS:         &gmproto.ListConversationsRequest{},
	gmproto.ActionType_NOTIFY_DITTO_ACTIVITY:      &gmproto.NotifyDittoActivityRequest{},
	gmproto.ActionType_GET_CONVERSATION_TYPE:      &gmproto.GetConversationTypeRequest{},
	gmproto.ActionType_GET_CONVERSATION:           &gmproto.GetConversationRequest{},
	gmproto.ActionType_LIST_MESSAGES:              &gmproto.ListMessagesRequest{},
	gmproto.ActionType_SEND_MESSAGE:               &gmproto.SendMessageRequest{},
	gmproto.ActionType_SEND_REACTION:              &gmproto.SendReactionRequest{},
	gmproto.ActionType_DELETE_MESSAGE:             &gmproto.DeleteMessageRequest{},
	gmproto.ActionType_GET_PARTICIPANTS_THUMBNAIL: &gmproto.GetThumbnailRequest{},
	gmproto.ActionType_GET_CONTACTS_THUMBNAIL:     &gmproto.GetThumbnailRequest{},
	gmproto.ActionType_LIST_CONTACTS:              &gmproto.ListContactsRequest{},
	gmproto.ActionType_LIST_TOP_CONTACTS:          &gmproto.ListTopContactsRequest{},
	gmproto.ActionType_GET_OR_CREATE_CONVERSATION: &gmproto.GetOrCreateConversationRequest{},
	gmproto.ActionType_UPDATE_CONVERSATION:        &gmproto.UpdateConversationRequest{},
	gmproto.ActionType_RESEND_MESSAGE:             &gmproto.ResendMessageRequest{},
	gmproto.ActionType_TYPING_UPDATES:             &gmproto.TypingUpdateRequest{},
	gmproto.ActionType_GET_FULL_SIZE_IMAGE:        &gmproto.GetFullSizeImageRequest{},

	gmproto.ActionType_CREATE_GAIA_PAIRING_CLIENT_INIT:     &gmproto.GaiaPairingRequestContainer{},
	gmproto.ActionType_CREATE_GAIA_PAIRING_CLIENT_FINISHED: &gmproto.GaiaPairingRequestContainer{},
}

func main() {
	var x crypto.AESCTRHelper
	file, err := os.Open("config.json")
	if errors.Is(err, os.ErrNotExist) {
		_ = file.Close()
		_, _ = fmt.Fprintln(os.Stderr, "config.json doesn't exist")
		_, _ = fmt.Fprintln(os.Stderr, "Please find pr_crypto_msg_enc_key and pr_crypto_msg_hmac from localStorage")
		_, _ = fmt.Fprintln(os.Stderr, "(make sure not to confuse it with pr_crypto_hmac)")
		stdin := bufio.NewScanner(os.Stdin)
		_, _ = fmt.Fprint(os.Stderr, "AES key (pr_crypto_msg_enc_key): ")
		stdin.Scan()
		x.AESKey = must(base64.StdEncoding.DecodeString(stdin.Text()))
		if len(x.AESKey) != 32 {
			_, _ = fmt.Fprintln(os.Stderr, "AES key must be 32 bytes")
			return
		}
		_, _ = fmt.Fprint(os.Stderr, "HMAC key (pr_crypto_msg_hmac): ")
		stdin.Scan()
		x.HMACKey = must(base64.StdEncoding.DecodeString(stdin.Text()))
		if len(x.HMACKey) != 32 {
			_, _ = fmt.Fprintln(os.Stderr, "HMAC key must be 32 bytes")
			return
		}
		file, err = os.OpenFile("config.json", os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0600)
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "Failed to open config.json for writing")
			return
		}
		mustNoReturn(json.NewEncoder(file).Encode(&x))
		_, _ = fmt.Fprintln(os.Stderr, "Saved keys to config.json")
	} else {
		mustNoReturn(json.NewDecoder(file).Decode(&x))
	}
	_ = file.Close()
	_, _ = fmt.Fprintln(os.Stderr, "Please paste the request body, then press Ctrl+D to close stdin")
	d := must(io.ReadAll(os.Stdin))
	var decoded []byte
	var typ gmproto.MessageType
	if json.Valid(d) {
		var orm gmproto.OutgoingRPCMessage
		mustNoReturn(pblite.Unmarshal(d, &orm))
		decoded = orm.Data.MessageData
		typ = orm.Data.MessageTypeData.MessageType
		fmt.Println("DEST REGISTRATION IDS:", orm.DestRegistrationIDs)
		fmt.Println("MOBILE:", orm.GetMobile())
	} else {
		decoded = must(base64.StdEncoding.DecodeString(string(d)))
	}
	outgoing := true
	if outgoing {
		var ord gmproto.OutgoingRPCData
		mustNoReturn(proto.Unmarshal(decoded, &ord))
		_, _ = fmt.Fprintln(os.Stderr)
		_, _ = fmt.Fprintln(os.Stderr, "CHANNEL:", typ.String())
		_, _ = fmt.Fprintln(os.Stderr, "REQUEST TYPE:", ord.Action.String())
		_, _ = fmt.Fprintln(os.Stderr, "REQUEST ID:", ord.RequestID)
		var decrypted []byte

		if ord.EncryptedProtoData != nil {
			decrypted = must(x.Decrypt(ord.EncryptedProtoData))
		} else if ord.UnencryptedProtoData != nil {
			decrypted = ord.UnencryptedProtoData
		} else {
			_, _ = fmt.Fprintln(os.Stderr, "No encrypted data")
			return
		}
		_, _ = fmt.Fprintln(os.Stderr, "------------------------------ RAW DECRYPTED DATA ------------------------------")
		fmt.Println(base64.StdEncoding.EncodeToString(decrypted))
		_, _ = fmt.Fprintln(os.Stderr, "--------------------------------- DECODED DATA ---------------------------------")
		respType, ok := requestType[ord.Action]
		var cmd *exec.Cmd
		if ok {
			cmd = exec.Command("protoc", "--proto_path=../gmproto", "--decode", string(respType.ProtoReflect().Type().Descriptor().FullName()), "client.proto")
		} else {
			cmd = exec.Command("protoc", "--decode_raw")
		}
		cmd.Stdin = bytes.NewReader(decrypted)
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		mustNoReturn(cmd.Run())
		if ok {
			respData := respType.ProtoReflect().New().Interface()
			mustNoReturn(proto.Unmarshal(decrypted, respData))
			_, _ = fmt.Fprintln(os.Stderr, "------------------------------ PARSED STRUCT DATA ------------------------------")
			_, _ = fmt.Fprintf(os.Stderr, "%+v\n", respData)
		}
	} else {
		var ird gmproto.RPCMessageData
		mustNoReturn(proto.Unmarshal(decoded, &ird))
		decrypted := must(x.Decrypt(ird.EncryptedData))
		_, _ = fmt.Fprintln(os.Stderr)
		_, _ = fmt.Fprintln(os.Stderr, "CHANNEL:", typ.String())
		_, _ = fmt.Fprintln(os.Stderr, "REQUEST TYPE:", ird.Action.String())
		_, _ = fmt.Fprintln(os.Stderr, "REQUEST ID:", ird.SessionID)
		_, _ = fmt.Fprintln(os.Stderr, "------------------------------ RAW DECRYPTED DATA ------------------------------")
		fmt.Println(base64.StdEncoding.EncodeToString(decrypted))
	}
	_, _ = fmt.Fprintln(os.Stderr, "--------------------------------------------------------------------------------")
}
