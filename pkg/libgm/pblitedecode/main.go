package main

import (
	"fmt"
	"io"
	"os"

	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"

	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/pblite"
)

func must[T any](t T, err error) T {
	if err != nil {
		panic(err)
	}
	return t
}

func main() {
	files := []protoreflect.FileDescriptor{
		gmproto.File_authentication_proto,
		gmproto.File_config_proto,
		gmproto.File_client_proto,
		gmproto.File_conversations_proto,
		gmproto.File_events_proto,
		gmproto.File_rpc_proto,
		gmproto.File_settings_proto,
		gmproto.File_util_proto,
		gmproto.File_ukey_proto,
	}
	var msgDesc protoreflect.MessageDescriptor
	for _, file := range files {
		msgDesc = file.Messages().ByName(protoreflect.Name(os.Args[1]))
		if msgDesc != nil {
			break
		}
	}
	if msgDesc == nil {
		fmt.Println("Message not found")
		os.Exit(1)
	}
	msg := dynamicpb.NewMessage(msgDesc)

	err := pblite.Unmarshal(must(io.ReadAll(os.Stdin)), msg)
	if err != nil {
		fmt.Println(err)
		os.Exit(2)
	}
	fmt.Println(prototext.Format(msg))
}
