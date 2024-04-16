package libgm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"go.mau.fi/mautrix-gmessages/libgm/events"
	"go.mau.fi/mautrix-gmessages/libgm/pblite"

	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

const defaultPingTimeout = 1 * time.Minute
const shortPingTimeout = 10 * time.Second
const minPingInterval = 30 * time.Second
const maxRepingTickerTime = 64 * time.Minute

var pingIDCounter atomic.Uint64

// Goals of the ditto pinger:
//   - By default, send pings to the phone every 15 minutes when the long polling connection restarts
//   - If an outgoing request doesn't respond quickly, send a ping immediately
//   - If a ping caused by a request timeout doesn't respond quickly, send PhoneNotResponding
//     (the user is probably actively trying to use the bridge)
//   - If the first ping doesn't respond, send PhoneNotResponding
//     (to avoid the bridge being stuck in the CONNECTING state)
//   - If a ping doesn't respond, send new pings on increasing intervals
//     (starting from 1 minute up to 1 hour) until it responds
//   - If a normal ping doesn't respond, send PhoneNotResponding after 3 failed pings
//     (so after ~8 minutes in total, not faster to avoid unnecessarily spamming the user)
//   - If a request timeout happens during backoff pings, send PhoneNotResponding immediately
//   - If a ping responds and PhoneNotResponding was sent, send PhoneRespondingAgain
type dittoPinger struct {
	client *Client

	firstPingDone     bool
	pingHandlingLock  sync.RWMutex
	oldestPingTime    time.Time
	lastPingTime      time.Time
	pingFails         int
	notRespondingSent bool

	stop <-chan struct{}
	ping <-chan struct{}
	log  *zerolog.Logger
}

func (dp *dittoPinger) OnRespond(pingID uint64, dur time.Duration) {
	dp.pingHandlingLock.Lock()
	defer dp.pingHandlingLock.Unlock()
	logEvt := dp.log.Debug().Uint64("ping_id", pingID).Dur("duration", dur)
	if dp.notRespondingSent {
		logEvt.Msg("Ditto ping successful (phone is back online)")
		dp.client.triggerEvent(&events.PhoneRespondingAgain{})
	} else if dp.pingFails > 0 {
		logEvt.Msg("Ditto ping successful (stopped failing)")
		// TODO separate event?
		dp.client.triggerEvent(&events.PhoneRespondingAgain{})
	} else {
		logEvt.Msg("Ditto ping successful")
	}
	dp.oldestPingTime = time.Time{}
	dp.notRespondingSent = false
	dp.pingFails = 0
	dp.firstPingDone = true
}

func (dp *dittoPinger) OnTimeout(pingID uint64, sendNotResponding bool) {
	dp.pingHandlingLock.Lock()
	defer dp.pingHandlingLock.Unlock()
	dp.log.Warn().Uint64("ping_id", pingID).Msg("Ditto ping is taking long, phone may be offline")
	if (!dp.firstPingDone || sendNotResponding) && !dp.notRespondingSent {
		dp.client.triggerEvent(&events.PhoneNotResponding{})
		dp.notRespondingSent = true
	}
}

func (dp *dittoPinger) WaitForResponse(pingID uint64, start time.Time, timeout time.Duration, timeoutCount int, pingChan <-chan *IncomingRPCMessage) {
	var timerChan <-chan time.Time
	var timer *time.Timer
	if timeout > 0 {
		timer = time.NewTimer(timeout)
		timerChan = timer.C
	}
	select {
	case <-pingChan:
		dp.OnRespond(pingID, time.Since(start))
		if timer != nil && !timer.Stop() {
			<-timer.C
		}
	case <-timerChan:
		dp.OnTimeout(pingID, timeout == shortPingTimeout || timeoutCount > 3)
		repingTickerTime := 1 * time.Minute
		var repingTicker *time.Ticker
		var repingTickerChan <-chan time.Time
		if timeoutCount == 0 {
			repingTicker = time.NewTicker(repingTickerTime)
			repingTickerChan = repingTicker.C
		}
		for {
			timeoutCount++
			select {
			case <-pingChan:
				dp.OnRespond(pingID, time.Since(start))
				return
			case <-repingTickerChan:
				if repingTickerTime < maxRepingTickerTime {
					repingTickerTime *= 2
					repingTicker.Reset(repingTickerTime)
				}
				subPingID := pingIDCounter.Add(1)
				dp.log.Debug().
					Uint64("parent_ping_id", pingID).
					Uint64("ping_id", subPingID).
					Str("next_reping", repingTickerTime.String()).
					Msg("Sending new ping")
				dp.Ping(subPingID, defaultPingTimeout, timeoutCount)
			case <-dp.client.pingShortCircuit:
				dp.pingHandlingLock.Lock()
				dp.log.Debug().Uint64("ping_id", pingID).
					Msg("Ditto ping wait short-circuited during ping backoff, sending PhoneNotResponding immediately")
				if !dp.notRespondingSent {
					dp.client.triggerEvent(&events.PhoneNotResponding{})
					dp.notRespondingSent = true
				}
				dp.pingHandlingLock.Unlock()
			case <-dp.stop:
				return
			}
		}
	case <-dp.stop:
		if timer != nil && !timer.Stop() {
			<-timer.C
		}
	}
}

func (dp *dittoPinger) Ping(pingID uint64, timeout time.Duration, timeoutCount int) {
	dp.pingHandlingLock.Lock()
	if time.Since(dp.lastPingTime) < minPingInterval {
		dp.log.Debug().
			Uint64("ping_id", pingID).
			Time("last_ping_time", dp.lastPingTime).
			Msg("Skipping ping since last one was too recently")
		dp.pingHandlingLock.Unlock()
		return
	}
	now := time.Now()
	dp.lastPingTime = now
	if dp.oldestPingTime.IsZero() {
		dp.oldestPingTime = now
	}
	pingChan, err := dp.client.NotifyDittoActivity()
	if err != nil {
		dp.log.Err(err).Uint64("ping_id", pingID).Msg("Error sending ping")
		dp.pingFails++
		dp.client.triggerEvent(&events.PingFailed{
			Error:      fmt.Errorf("failed to notify ditto activity: %w", err),
			ErrorCount: dp.pingFails,
		})
		dp.pingHandlingLock.Unlock()
		return
	}
	dp.pingHandlingLock.Unlock()
	if timeoutCount == 0 {
		dp.WaitForResponse(pingID, now, timeout, timeoutCount, pingChan)
	} else {
		go dp.WaitForResponse(pingID, now, timeout, timeoutCount, pingChan)
	}
}

const DefaultBugleDefaultCheckInterval = 2*time.Hour + 55*time.Minute

func (dp *dittoPinger) Loop() {
	for {
		select {
		case <-dp.client.pingShortCircuit:
			pingID := pingIDCounter.Add(1)
			dp.log.Debug().Uint64("ping_id", pingID).Msg("Ditto ping wait short-circuited")
			dp.Ping(pingID, shortPingTimeout, 0)
		case <-dp.ping:
			pingID := pingIDCounter.Add(1)
			dp.log.Trace().Uint64("ping_id", pingID).Msg("Doing normal ditto ping")
			dp.Ping(pingID, defaultPingTimeout, 0)
		case <-dp.stop:
			return
		}
		if dp.client.shouldDoDataReceiveCheck() {
			go dp.HandleNoRecentUpdates()
		}
	}
}

func (dp *dittoPinger) HandleNoRecentUpdates() {
	dp.client.triggerEvent(&events.NoDataReceived{})
	dp.log.Warn().Msg("No data received recently, sending extra GET_UPDATES call")
	err := dp.client.sessionHandler.sendMessageNoResponse(SendMessageParams{
		Action:    gmproto.ActionType_GET_UPDATES,
		OmitTTL:   true,
		RequestID: dp.client.sessionHandler.sessionID,
	})
	if err != nil {
		dp.log.Err(err).Msg("Failed to send extra GET_UPDATES call")
	} else {
		dp.log.Debug().Msg("Sent extra GET_UPDATES call")
	}
}

func (c *Client) shouldDoDataReceiveCheck() bool {
	c.nextDataReceiveCheckLock.Lock()
	defer c.nextDataReceiveCheckLock.Unlock()
	if time.Until(c.nextDataReceiveCheck) <= 0 {
		c.nextDataReceiveCheck = time.Now().Add(DefaultBugleDefaultCheckInterval)
		return true
	}
	return false
}

func (c *Client) bumpNextDataReceiveCheck(after time.Duration) {
	c.nextDataReceiveCheckLock.Lock()
	if time.Until(c.nextDataReceiveCheck) < after {
		c.nextDataReceiveCheck = time.Now().Add(after)
	}
	c.nextDataReceiveCheckLock.Unlock()
}

func tryReadBody(resp io.ReadCloser) []byte {
	data, _ := io.ReadAll(resp)
	_ = resp.Close()
	return data
}

func (c *Client) doLongPoll(loggedIn bool, onFirstConnect func()) {
	c.listenID++
	listenID := c.listenID
	listenReqID := uuid.NewString()

	log := c.Logger.With().Int("listen_id", listenID).Logger()
	defer func() {
		log.Debug().Msg("Long polling stopped")
	}()
	ctx := log.WithContext(context.TODO())
	log.Debug().Str("listen_uuid", listenReqID).Msg("Long polling starting")

	dittoPing := make(chan struct{}, 1)
	stopDittoPinger := make(chan struct{})
	defer close(stopDittoPinger)
	go (&dittoPinger{
		ping:   dittoPing,
		stop:   stopDittoPinger,
		log:    &log,
		client: c,
	}).Loop()

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
		log.Trace().Msg("Starting new long-polling request")
		payload := &gmproto.ReceiveMessagesRequest{
			Auth: &gmproto.AuthMessage{
				RequestID:        listenReqID,
				TachyonAuthToken: c.AuthData.TachyonAuthToken,
				Network:          c.AuthData.AuthNetwork(),
				ConfigVersion:    util.ConfigMessage,
			},
			Unknown: &gmproto.ReceiveMessagesRequest_UnknownEmptyObject2{
				Unknown: &gmproto.ReceiveMessagesRequest_UnknownEmptyObject1{},
			},
		}
		url := util.ReceiveMessagesURL
		if c.AuthData.HasCookies() {
			url = util.ReceiveMessagesURLGoogle
		}
		resp, err := c.makeProtobufHTTPRequestContext(ctx, url, payload, ContentTypePBLite, true)
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
			body := tryReadBody(resp.Body)
			log.Error().
				Int("status_code", resp.StatusCode).
				Bytes("resp_body", body).
				Msg("Error making listen request")
			if loggedIn {
				c.triggerEvent(&events.ListenFatalError{Error: events.HTTPError{Action: "polling", Resp: resp, Body: body}})
			}
			return
		} else if resp.StatusCode >= 400 {
			if loggedIn {
				c.triggerEvent(&events.ListenTemporaryError{Error: events.HTTPError{Action: "polling", Resp: resp, Body: tryReadBody(resp.Body)}})
			} else {
				_ = resp.Body.Close()
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
		if onFirstConnect != nil {
			go onFirstConnect()
			onFirstConnect = nil
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
				logEvt = log.Trace()
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
				log.Trace().Msg("Got stream end marker")
				expectEOF = true
				continue
			}
			chunk = bytes.TrimPrefix(chunk, []byte{','})
		}
		accumulatedData = append(accumulatedData, chunk...)
		if !json.Valid(accumulatedData) {
			log.Trace().Msg("Invalid JSON, reading next chunk")
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
			level := zerolog.TraceLevel
			if msg.GetAck().GetCount() > 0 {
				level = zerolog.DebugLevel
			}
			log.WithLevel(level).Int32("count", msg.GetAck().GetCount()).Msg("Got startup ack count message")
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
