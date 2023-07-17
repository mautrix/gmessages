package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/mdp/qrterminal/v3"
	"github.com/rs/zerolog"

	"go.mau.fi/mautrix-gmessages/libgm"
	"go.mau.fi/mautrix-gmessages/libgm/events"
	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
)

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
var sess libgm.AuthData

func main() {
	log = zerolog.New(zerolog.NewConsoleWriter(func(w *zerolog.ConsoleWriter) {
		w.Out = os.Stdout
		w.TimeFormat = time.Stamp
	})).With().Timestamp().Logger()
	file, err := os.Open("session.json")
	var doLogin bool
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			panic(err)
		}
		sess = *libgm.NewAuthData()
		doLogin = true
	} else {
		must(json.NewDecoder(file).Decode(&sess))
		log.Info().Msg("Loaded session?")
	}
	_ = file.Close()
	cli = libgm.NewClient(&sess, log)
	cli.SetEventHandler(evtHandler)
	if doLogin {
		qr := mustReturn(cli.StartLogin())
		qrterminal.GenerateHalfBlock(qr, qrterminal.L, os.Stdout)
		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				if sess.Browser != nil {
					return
				}
				qr := mustReturn(cli.RefreshPhoneRelay())
				qrterminal.GenerateHalfBlock(qr, qrterminal.L, os.Stdout)
			}
		}()
	} else {
		must(cli.Connect())
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
			switch cmd {
			//case "getavatar":
			//	_, err := cli.GetFullSizeImage(args)
			//	fmt.Println(err)
			case "listcontacts":
				cli.ListContacts()
			case "topcontacts":
				cli.ListTopContacts()
			case "getconversation":
				cli.GetConversation(args[0])
			}
			//go handleCmd(strings.ToLower(cmd), args)
		}
	}
}

func saveSession() {
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
		saveSession()
		log.Debug().Msg("Wrote session")
	case *gmproto.Message:
		log.Debug().Any("data", evt).Msg("Message event")
	case *gmproto.Conversation:
		log.Debug().Any("data", evt).Msg("Conversation event")
	case *events.BrowserActive:
		log.Debug().Any("data", evt).Msg("Browser active")
	default:
		log.Debug().Any("data", evt).Msg("Unknown event")
	}
}
