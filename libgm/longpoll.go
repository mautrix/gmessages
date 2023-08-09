package libgm

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"go.mau.fi/mautrix-gmessages/libgm/events"
	"go.mau.fi/mautrix-gmessages/libgm/pblite"

	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

const phoneNotRespondingTimeout = 30 * time.Second

func (c *Client) doDittoPinger(log *zerolog.Logger, dittoPing chan struct{}, stopPinger chan struct{}) {
	notResponding := false
	exit := false
	onRespond := func() {
		if notResponding {
			log.Debug().Msg("Ditto ping succeeded, phone is back online")
			c.triggerEvent(&events.PhoneRespondingAgain{})
			notResponding = false
		}
	}
	doPing := func() {
		pingChan, err := c.NotifyDittoActivity()
		if err != nil {
			log.Err(err).Msg("Error notifying ditto activity")
			return
		}
		select {
		case <-pingChan:
			onRespond()
			return
		case <-time.After(phoneNotRespondingTimeout):
			log.Warn().Msg("Ditto ping is taking long, phone may be offline")
			c.triggerEvent(&events.PhoneNotResponding{})
			notResponding = true
		case <-stopPinger:
			exit = true
			return
		}
		select {
		case <-pingChan:
			onRespond()
		case <-stopPinger:
			exit = true
			return
		}
	}
	for !exit {
		select {
		case <-c.pingShortCircuit:
			log.Debug().Msg("Ditto ping wait short-circuited")
			doPing()
		case <-dittoPing:
			log.Trace().Msg("Doing normal ditto ping")
			doPing()
		case <-stopPinger:
			return
		}
	}
}

func (c *Client) doLongPoll(loggedIn bool) {
	c.listenID++
	listenID := c.listenID
	listenReqID := uuid.NewString()

	log := c.Logger.With().Int("listen_id", listenID).Logger()
	defer func() {
		log.Debug().Msg("Long polling stopped")
	}()
	log.Debug().Str("listen_uuid", listenReqID).Msg("Long polling starting")

	dittoPing := make(chan struct{}, 1)
	stopDittoPinger := make(chan struct{})

	defer close(stopDittoPinger)
	go c.doDittoPinger(&log, dittoPing, stopDittoPinger)

	errorCount := 1
	for c.listenID == listenID {
		err := c.refreshAuthToken()
		if err != nil {
			log.Err(err).Msg("Error refreshing auth token")
			if loggedIn {
				c.triggerEvent(&events.ListenFatalError{Error: fmt.Errorf("failed to refresh auth token: %w", err)})
			}
			return
		}
		log.Debug().Msg("Starting new long-polling request")
		payload := &gmproto.ReceiveMessagesRequest{
			Auth: &gmproto.AuthMessage{
				RequestID:        listenReqID,
				TachyonAuthToken: c.AuthData.TachyonAuthToken,
				ConfigVersion:    util.ConfigMessage,
			},
			Unknown: &gmproto.ReceiveMessagesRequest_UnknownEmptyObject2{
				Unknown: &gmproto.ReceiveMessagesRequest_UnknownEmptyObject1{},
			},
		}
		resp, err := c.makeProtobufHTTPRequest(util.ReceiveMessagesURL, payload, ContentTypePBLite)
		if err != nil {
			if loggedIn {
				c.triggerEvent(&events.ListenTemporaryError{Error: err})
			}
			errorCount++
			sleepSeconds := (errorCount + 1) * 5
			log.Err(err).Int("sleep_seconds", sleepSeconds).Msg("Error making listen request, retrying in a while")
			time.Sleep(time.Duration(sleepSeconds) * time.Second)
			continue
		}
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			log.Error().Int("status_code", resp.StatusCode).Msg("Error making listen request")
			if loggedIn {
				c.triggerEvent(&events.ListenFatalError{Error: events.HTTPError{Action: "polling", Resp: resp}})
			}
			return
		} else if resp.StatusCode >= 400 {
			if loggedIn {
				c.triggerEvent(&events.ListenTemporaryError{Error: events.HTTPError{Action: "polling", Resp: resp}})
			}
			errorCount++
			sleepSeconds := (errorCount + 1) * 5
			log.Debug().
				Int("statusCode", resp.StatusCode).
				Int("sleep_seconds", sleepSeconds).
				Msg("Error in long polling, retrying in a while")
			time.Sleep(time.Duration(sleepSeconds) * time.Second)
			continue
		}
		if errorCount > 0 {
			errorCount = 0
			if loggedIn {
				c.triggerEvent(&events.ListenRecovered{})
			}
		}
		log.Debug().Int("statusCode", resp.StatusCode).Msg("Long polling opened")
		c.longPollingConn = resp.Body
		if loggedIn {
			select {
			case dittoPing <- struct{}{}:
			default:
				log.Debug().Msg("Ditto pinger is still waiting for previous ping, skipping new ping")
			}
		}
		c.readLongPoll(&log, resp.Body)
		c.longPollingConn = nil
	}
}

func (c *Client) readLongPoll(log *zerolog.Logger, rc io.ReadCloser) {
	defer rc.Close()
	c.disconnecting = false
	reader := bufio.NewReader(rc)
	buf := make([]byte, 2621440)
	var accumulatedData []byte
	n, err := reader.Read(buf[:2])
	if err != nil {
		log.Err(err).Msg("Error reading opening bytes")
		return
	} else if n != 2 || string(buf[:2]) != "[[" {
		log.Err(err).Msg("Opening is not [[")
		return
	}
	var expectEOF bool
	for {
		n, err = reader.Read(buf)
		if err != nil {
			var logEvt *zerolog.Event
			if (errors.Is(err, io.EOF) && expectEOF) || c.disconnecting {
				logEvt = log.Debug()
			} else {
				logEvt = log.Warn()
			}
			logEvt.Err(err).Msg("Stopped reading data from server")
			return
		} else if expectEOF {
			log.Warn().Msg("Didn't get EOF after stream end marker")
		}
		chunk := buf[:n]
		if len(accumulatedData) == 0 {
			if len(chunk) == 2 && string(chunk) == "]]" {
				log.Debug().Msg("Got stream end marker")
				expectEOF = true
				continue
			}
			chunk = bytes.TrimPrefix(chunk, []byte{','})
		}
		accumulatedData = append(accumulatedData, chunk...)
		if !json.Valid(accumulatedData) {
			log.Trace().Bytes("data", chunk).Msg("Invalid JSON, reading next chunk")
			continue
		}
		currentBlock := accumulatedData
		accumulatedData = accumulatedData[:0]
		msg := &gmproto.LongPollingPayload{}
		err = pblite.Unmarshal(currentBlock, msg)
		if err != nil {
			log.Err(err).Msg("Error deserializing pblite message")
			continue
		}
		switch {
		case msg.GetData() != nil:
			c.HandleRPCMsg(msg.GetData())
		case msg.GetAck() != nil:
			log.Debug().Int32("count", msg.GetAck().GetCount()).Msg("Got startup ack count message")
			c.skipCount = int(msg.GetAck().GetCount())
		case msg.GetStartRead() != nil:
			log.Trace().Msg("Got startRead message")
		case msg.GetHeartbeat() != nil:
			log.Trace().Msg("Got heartbeat message")
		default:
			log.Warn().
				Str("data", base64.StdEncoding.EncodeToString(currentBlock)).
				Msg("Got unknown message")
		}
	}
}

func (c *Client) closeLongPolling() {
	if conn := c.longPollingConn; conn != nil {
		c.Logger.Debug().Int("current_listen_id", c.listenID).Msg("Closing long polling connection manually")
		c.listenID++
		c.disconnecting = true
		_ = conn.Close()
		c.longPollingConn = nil
	}
}
