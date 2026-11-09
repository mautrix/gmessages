package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog"
	"go.mau.fi/util/exhttp"
	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-gmessages/pkg/libgm"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
)

var dbPath = os.Getenv("GM_DB")
var sessPath = os.Getenv("GM_RCS_SESSION")

type meta struct {
	Session     *libgm.AuthData            `json:"session"`
	SIMMetadata map[string]*gmproto.SIMCard `json:"sim_metadata"`
}

func main() {
	db, err := sql.Open("sqlite3", "file:"+dbPath+"?mode=ro")
	if err != nil {
		panic(err)
	}
	var raw string
	if err = db.QueryRow("select metadata from user_login").Scan(&raw); err != nil {
		panic(err)
	}
	var mt meta
	if err = json.Unmarshal([]byte(raw), &mt); err != nil {
		panic(err)
	}
	logf, _ := os.OpenFile(os.Getenv("GM_RCS_LOG"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	log := zerolog.New(logf).Level(zerolog.TraceLevel).With().Timestamp().Logger()
	cli := libgm.NewClient(mt.Session, nil, log, exhttp.ClientSettings{})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	var sim *gmproto.SIMCard
	for _, s := range mt.SIMMetadata {
		sim = s
	}
	var sess *libgm.RCSSession
	if data, err := os.ReadFile(sessPath); err == nil {
		sess = &libgm.RCSSession{}
		_ = json.Unmarshal(data, sess)
	} else {
		sess, err = cli.NewRCSSession(sim.GetRcsAddress2(), sim.GetSIMData().GetInternationalPhoneNumber())
		if err != nil {
			panic(err)
		}
	}
	save := func() {
		data, _ := json.MarshalIndent(sess, "", "  ")
		_ = os.WriteFile(sessPath, data, 0600)
	}
	show := func(label string, msg proto.Message, err error) {
		if err != nil {
			fmt.Println(label, "ERROR:", err)
			return
		}
		fmt.Println(label, "OK:", prototext.Format(msg))
	}
	fmt.Println("base url:", sess.BaseURL, "self:", sess.Self.GetSourceID() != "")
	switch os.Args[1] {
	case "list":
		resp, err := cli.RCSListIdentities(ctx, sess)
		show("ListIdentities", resp, err)
	case "signin":
		err := cli.RCSSignIn(ctx, sess)
		fmt.Println("SignInSecondary err:", err, "token len:", len(sess.Token), "expiry:", sess.TokenExpiry, "regID:", sess.RegistrationID)
		if err == nil {
			save()
		}
	case "refresh":
		err := cli.RCSRefresh(ctx, sess)
		fmt.Println("RegisterRefresh err:", err, "token len:", len(sess.Token), "expiry:", sess.TokenExpiry)
		if err == nil {
			save()
		}
	case "lookup":
		resp, err := cli.RCSLookupRegistered(ctx, sess, os.Args[2:]...)
		show("LookupRegistered", resp, err)
	case "send":
		dest := &gmproto.Device{UserID: 1, SourceID: os.Args[2], Network: libgm.RCSNetwork}
		resp, err := cli.RCSSend(ctx, sess, dest, "", libgm.RCSTextContent(os.Args[3]))
		show("SendMessage", resp, err)
	case "getkey":
		got := make(chan []byte, 1)
		cli.SetEventHandler(func(evt any) {
			if st, ok := evt.(*gmproto.Settings); ok && len(st.GetRCSSettings().GetUnknownBytes()) > 0 {
				select {
				case got <- st.GetRCSSettings().GetUnknownBytes():
				default:
				}
			}
		})
		if err := cli.Connect(ctx); err != nil {
			panic(err)
		}
		select {
		case key := <-got:
			sess.ReflectionKey = key
			save()
			fmt.Println("got reflection key, length", len(key))
		case <-time.After(90 * time.Second):
			fmt.Println("no settings with key within 90s")
		}
		cli.Disconnect()
	case "sendreflect":
		dest := &gmproto.Device{UserID: 1, SourceID: os.Args[2], Network: libgm.RCSNetwork}
		content := libgm.RCSTextContent(os.Args[3])
		msgID := uuid.NewString()
		resp, err := cli.RCSSend(ctx, sess, dest, msgID, content)
		show("SendMessage", resp, err)
		if err == nil {
			fmt.Println("reflect err:", cli.RCSReflect(ctx, sess, msgID, dest, content), "id:", msgID)
		}
	case "reflect":
		dest := &gmproto.Device{UserID: 1, SourceID: os.Args[2], Network: libgm.RCSNetwork}
		selfType, _ := strconv.Atoi(os.Args[4])
		self := &gmproto.Device{UserID: int64(selfType), SourceID: sess.Self.GetSourceID(), Network: libgm.RCSNetwork}
		msgID := uuid.NewString()
		fmt.Println("reflect err:", cli.RCSReflectTo(ctx, sess, self, msgID, dest, libgm.RCSTextContent(os.Args[3])), "id:", msgID)
	case "fetch":
		if err := cli.Connect(ctx); err != nil {
			panic(err)
		}
		time.Sleep(3 * time.Second)
		resp, err := cli.FetchMessages(ctx, os.Args[2], 6, nil)
		if err != nil {
			fmt.Println("fetch err", err)
		} else {
			for _, m := range resp.GetMessages() {
				txt := ""
				for _, p := range m.GetMessageInfo() {
					txt += p.GetMessageContent().GetContent()
				}
				fmt.Println(m.GetMessageID(), m.GetMessageStatus().GetStatus(), m.GetTimestamp(), m.GetTmpID(), txt)
			}
		}
		cli.Disconnect()
	case "recv":
		dur, _ := time.ParseDuration(os.Args[2])
		rctx, rcancel := context.WithTimeout(ctx, dur)
		defer rcancel()
		for rctx.Err() == nil {
			err := cli.RCSReceive(rctx, sess, func(msg *gmproto.TachyonInboxMessage) {
				var content gmproto.RCSContent
				_ = proto.Unmarshal(msg.GetPayload(), &content)
				fmt.Printf("%s RECV type=%d id=%s from=%s/%d group=%s class=%d\n", time.Now().Format("15:04:05"), msg.GetMessageType(), msg.GetMessageID(), msg.GetSender().GetSourceID(), msg.GetSender().GetUserID(), msg.GetGroup().GetSourceID(), msg.GetMessageClass())
				for _, p := range content.GetParts() {
					d := string(p.GetData())
					if len(d) > 300 {
						d = d[:300]
					}
					fmt.Printf("   part %q: %q\n", p.GetContentType(), d)
				}
				for _, h := range content.GetHeaders() {
					fmt.Printf("   header %s %s=%s\n", h.GetNamespace(), h.GetName(), h.GetValue())
				}
				if len(content.GetParts()) == 0 {
					fmt.Printf("   raw payload %d bytes\n", len(msg.GetPayload()))
				}
				if err := cli.RCSAck(ctx, sess, msg); err != nil {
					fmt.Println("   ack err:", err)
				}
			})
			fmt.Println(time.Now().Format("15:04:05"), "stream ended:", err)
			if err != nil {
				time.Sleep(5 * time.Second)
			}
		}
	}
}
