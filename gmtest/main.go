package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/mdp/qrterminal/v3"
	"github.com/rs/zerolog"

	"go.mau.fi/mautrix-gmessages/libgm"
	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/crypto"
	"go.mau.fi/mautrix-gmessages/libgm/events"
)

type Session struct {
	*libgm.DevicePair
	*crypto.Cryptor
	*binary.WebAuthKey
	Cookies []*http.Cookie
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func mustReturn[T any](val T, err error) T {
	must(err)
	return val
}

var cli *libgm.Client
var log zerolog.Logger
var sess Session

func main() {
	log = zerolog.New(zerolog.NewConsoleWriter(func(w *zerolog.ConsoleWriter) {
		w.Out = os.Stdout
		w.TimeFormat = time.Stamp
	})).With().Timestamp().Logger()
	file, err := os.Open("session.json")
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			panic(err)
		}
	} else {
		must(json.NewDecoder(file).Decode(&sess))
		log.Info().Msg("Loaded session?")
	}
	_ = file.Close()
	if sess.Cryptor == nil {
		sess.Cryptor = crypto.NewCryptor(nil, nil)
	}
	cli = libgm.NewClient(sess.DevicePair, sess.Cryptor, log, nil)
	if sess.Cookies != nil {
		cli.SetCookies(sess.Cookies)
	}
	cli.SetEventHandler(evtHandler)
	log.Debug().Msg(base64.StdEncoding.EncodeToString(sess.GetWebAuthKey()))
	if sess.DevicePair == nil {
		pairer := mustReturn(cli.NewPairer(nil, 20))
		registered := mustReturn(pairer.RegisterPhoneRelay())
		must(cli.Connect(registered.Field5.RpcKey))
	} else {
		//pairer := mustReturn(cli.NewPairer(nil, 20))
		//newKey := pairer.GetWebEncryptionKey(sess.GetWebAuthKey())
		//log.Debug().Msg(base64.StdEncoding.EncodeToString(newKey))
		must(cli.Connect(sess.GetWebAuthKey()))
	}

	c := make(chan os.Signal)
	input := make(chan string)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		defer close(input)
		scan := bufio.NewScanner(os.Stdin)
		for scan.Scan() {
			line := strings.TrimSpace(scan.Text())
			if len(line) > 0 {
				input <- line
			}
		}
	}()
	defer saveSession()
	for {
		select {
		case <-c:
			log.Info().Msg("Interrupt received, exiting")
			return
		case cmd := <-input:
			if len(cmd) == 0 {
				log.Info().Msg("Stdin closed, exiting")
				return
			}
			args := strings.Fields(cmd)
			cmd = args[0]
			args = args[1:]
			//go handleCmd(strings.ToLower(cmd), args)
		}
	}
}

func saveSession() {
	sess.Cookies = cli.GetCookies()
	file := mustReturn(os.OpenFile("session.json", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600))
	must(json.NewEncoder(file).Encode(sess))
	_ = file.Close()
}

func evtHandler(rawEvt any) {
	switch evt := rawEvt.(type) {
	case *events.ClientReady:
		log.Debug().Any("data", evt).Msg("Client is ready!")
	case *events.PairSuccessful:
		log.Debug().Any("data", evt).Msg("Pair successful")
		sess.DevicePair = &libgm.DevicePair{
			Mobile:  evt.PairDeviceData.Mobile,
			Browser: evt.PairDeviceData.Browser,
		}
		sess.WebAuthKey = evt.PairDeviceData.WebAuthKeyData
		saveSession()
		log.Debug().Msg("Wrote session")
	case *binary.Event_MessageEvent:
		log.Debug().Any("data", evt).Msg("Message event")
	case *binary.Event_ConversationEvent:
		log.Debug().Any("data", evt).Msg("Conversation event")
	case *events.QR:
		qrterminal.GenerateHalfBlock(evt.URL, qrterminal.L, os.Stdout)
	case *events.BrowserActive:
		log.Debug().Any("data", evt).Msg("Browser active")
	case *events.Battery:
		log.Debug().Any("data", evt).Msg("Battery")
	case *events.DataConnection:
		log.Debug().Any("data", evt).Msg("Data connection")
	default:
		log.Debug().Any("data", evt).Msg("Unknown event")
	}
}
