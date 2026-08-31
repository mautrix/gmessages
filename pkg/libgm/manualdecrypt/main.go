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
	"slices"

	"go.mau.fi/util/pblite"
	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-gmessages/pkg/libgm"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/crypto"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
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
	gmproto.ActionType_SETTINGS_UPDATE:            &gmproto.SettingsUpdateRequest{},
	gmproto.ActionType_GET_UPDATES:                &gmproto.GetUpdatesRequest{},
	gmproto.ActionType_PRE_FETCH_CONTACTS:         &gmproto.PreFetchContactsRequest{},

	gmproto.ActionType_CREATE_GAIA_PAIRING_CLIENT_INIT:     &gmproto.GaiaPairingRequestContainer{},
	gmproto.ActionType_CREATE_GAIA_PAIRING_CLIENT_FINISHED: &gmproto.GaiaPairingRequestContainer{},
}

var responseType = libgm.GetResponseTypeMap()

func init() {
	responseType[gmproto.ActionType_CREATE_GAIA_PAIRING_CLIENT_INIT] = &gmproto.GaiaPairingResponseContainer{}
	responseType[gmproto.ActionType_CREATE_GAIA_PAIRING_CLIENT_FINISHED] = &gmproto.GaiaPairingResponseContainer{}
}

func main() {
	var x crypto.AESCTRHelper
	file, err := os.Open("config.json")
	if errors.Is(err, os.ErrNotExist) {
		_ = file.Close()
		_, _ = fmt.Fprintln(os.Stderr, "config.json doesn't exist")
		_, _ = fmt.Fprintln(os.Stderr, "Please find g_crypto_msg_enc_key and g_crypto_msg_hmac from localStorage")
		_, _ = fmt.Fprintln(os.Stderr, "(make sure not to confuse it with crypto_hmac)")
		stdin := bufio.NewScanner(os.Stdin)
		_, _ = fmt.Fprint(os.Stderr, "AES key (g_crypto_msg_enc_key): ")
		stdin.Scan()
		x.AESKey = must(base64.StdEncoding.DecodeString(stdin.Text()))
		if len(x.AESKey) != 32 {
			_, _ = fmt.Fprintln(os.Stderr, "AES key must be 32 bytes")
			return
		}
		_, _ = fmt.Fprint(os.Stderr, "HMAC key (g_crypto_msg_hmac): ")
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
	if !slices.Contains(os.Args, "--childprocess") {
		_, _ = fmt.Fprintln(os.Stderr, "Please paste the request body, then press Ctrl+D to close stdin")
	}
	d := must(io.ReadAll(os.Stdin))
	if slices.Contains(os.Args, "--receive-full") {
		var items [][]json.RawMessage
		mustNoReturn(json.Unmarshal(d, &items))
		for _, item := range items[0] {
			cmd := exec.Command(os.Args[0], "--receive", "--childprocess")
			cmd.Stdin = bytes.NewReader(item)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			mustNoReturn(cmd.Run())
		}
		return
	}
	var decoded []byte
	var typ gmproto.MessageType
	outgoing := true
	if slices.Contains(os.Args, "--receive") {
		outgoing = false
		var lpp gmproto.LongPollingPayload
		mustNoReturn(pblite.Unmarshal(d, &lpp))
		if lpp.Data == nil {
			if lpp.Ack != nil {
				_, _ = fmt.Fprintln(os.Stderr, "ACK COUNT:", lpp.Ack.GetCount())
			} else if lpp.Heartbeat != nil {
				_, _ = fmt.Fprintln(os.Stderr, "HEARTBEAT")
			} else if lpp.StartRead != nil {
				_, _ = fmt.Fprintln(os.Stderr, "START READ")
			} else {
				_, _ = fmt.Fprintln(os.Stderr, "UNKNOWN LONG POLLING PAYLOAD")
			}
			return
		}
		irm := lpp.Data
		decoded = irm.MessageData
		typ = irm.MessageType
		_, _ = fmt.Fprintln(os.Stderr, "REQUEST ID:", irm.ResponseID)
		_, _ = fmt.Fprintln(os.Stderr, "TIMESTAMP:", irm.Timestamp)
		_, _ = fmt.Fprintln(os.Stderr, "BUGLE ROUTE:", irm.BugleRoute.String())
	} else if json.Valid(d) {
		var orm gmproto.OutgoingRPCMessage
		mustNoReturn(pblite.Unmarshal(d, &orm))
		decoded = orm.Data.MessageData
		typ = orm.Data.MessageTypeData.MessageType
		fmt.Println("DEST REGISTRATION IDS:", orm.DestRegistrationIDs)
		fmt.Println("MOBILE:", orm.GetMobile())
	} else {
		decoded = must(base64.StdEncoding.DecodeString(string(d)))
	}
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
		doProtoDecode(requestType[ord.Action], decrypted)
	} else {
		var ird gmproto.RPCMessageData
		mustNoReturn(proto.Unmarshal(decoded, &ird))
		var decrypted []byte
		if ird.EncryptedData != nil {
			decrypted = must(x.Decrypt(ird.EncryptedData))
		} else if ird.EncryptedData2 != nil {
			decrypted = must(x.Decrypt(ird.EncryptedData2))
		} else {
			decrypted = ird.UnencryptedData
		}
		_, _ = fmt.Fprintln(os.Stderr)
		_, _ = fmt.Fprintln(os.Stderr, "CHANNEL:", typ.String())
		_, _ = fmt.Fprintln(os.Stderr, "REQUEST TYPE:", ird.Action.String())
		_, _ = fmt.Fprintln(os.Stderr, "REQUEST ID:", ird.SessionID)
		doProtoDecode(responseType[ird.Action], decrypted)
	}
	_, _ = fmt.Fprintln(os.Stderr, "--------------------------------------------------------------------------------")
}

func doProtoDecode(respType proto.Message, decrypted []byte) {
	_, _ = fmt.Fprintln(os.Stderr, "------------------------------ RAW DECRYPTED DATA ------------------------------")
	fmt.Println(base64.StdEncoding.EncodeToString(decrypted))
	_, _ = fmt.Fprintln(os.Stderr, "--------------------------------- DECODED DATA ---------------------------------")
	var cmd *exec.Cmd
	if respType != nil {
		cmd = exec.Command("protoc", "--proto_path=../gmproto", "--decode", string(respType.ProtoReflect().Type().Descriptor().FullName()), "client.proto")
	} else {
		cmd = exec.Command("protoc", "--decode_raw")
	}
	cmd.Stdin = bytes.NewReader(decrypted)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	mustNoReturn(cmd.Run())

	if respType != nil {
		respData := respType.ProtoReflect().New().Interface()
		mustNoReturn(proto.Unmarshal(decrypted, respData))
		_, _ = fmt.Fprintln(os.Stderr, "------------------------------ PARSED STRUCT DATA ------------------------------")
		_, _ = fmt.Fprintf(os.Stderr, "%+v\n", respData)
	}
}
