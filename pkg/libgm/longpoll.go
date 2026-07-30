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
	"go.mau.fi/util/pblite"

	"go.mau.fi/mautrix-gmessages/pkg/libgm/events"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/util"
)

const defaultPingTimeout = 1 * time.Minute
const shortPingTimeout = 10 * time.Second
const minPingInterval = 30 * time.Second
const initialRecoveryInterval = 1 * time.Minute
const maxRepingTickerTime = 64 * time.Minute

// If pings don't work we likely need to reconnect the session (assuming phone is alive)
const setActiveSessionAfterTimeouts = 2
const reconnectAfterTimeouts = 3

// longPollGapCatchUp is how long the long polling connection has to have been down before
// reopening it counts as a gap where events may have been missed.
const longPollGapCatchUp = 1 * time.Minute

var pingIDCounter atomic.Uint64

// phoneLiveness tracks whether the phone (for this bridge client) is responding
type phoneLiveness struct {
	lock sync.Mutex

	firstPingDone         bool
	lastPingTime          time.Time
	timeouts              int
	sendFails             int
	notRespondingSent     bool
	recoveryInterval      time.Duration
	lastRecoveryReconnect time.Time

	// currentReset cancels the waiters of the most recent ping generation
	currentReset atomic.Pointer[resetter]
}

func (pl *phoneLiveness) onLiveness() (notRespondingSent, wasFailing, needsCatchUp bool) {
	pl.lock.Lock()
	defer pl.lock.Unlock()
	notRespondingSent = pl.notRespondingSent
	wasFailing = notRespondingSent || pl.timeouts > 0 || pl.sendFails > 0
	needsCatchUp = notRespondingSent || pl.timeouts >= setActiveSessionAfterTimeouts
	pl.firstPingDone = true
	pl.notRespondingSent = false
	pl.timeouts = 0
	pl.sendFails = 0
	pl.recoveryInterval = 0
	return
}

func (pl *phoneLiveness) onTimeout(alertTimeoutCount int) (timeouts int, sendNotResponding bool) {
	pl.lock.Lock()
	defer pl.lock.Unlock()
	pl.timeouts++
	timeouts = pl.timeouts
	alert := !pl.firstPingDone || timeouts >= alertTimeoutCount
	sendNotResponding = alert && !pl.notRespondingSent
	if sendNotResponding {
		pl.notRespondingSent = true
	}
	return
}

func (pl *phoneLiveness) shouldMarkNotResponding(alertTimeoutCount int) bool {
	pl.lock.Lock()
	defer pl.lock.Unlock()
	if pl.notRespondingSent || pl.timeouts < alertTimeoutCount {
		return false
	}
	pl.notRespondingSent = true
	return true
}

func (pl *phoneLiveness) timeoutCount() int {
	pl.lock.Lock()
	defer pl.lock.Unlock()
	return pl.timeouts
}

func (pl *phoneLiveness) canDoRecovery() bool {
	pl.lock.Lock()
	defer pl.lock.Unlock()
	return pl.lastRecoveryReconnect.IsZero() || time.Since(pl.lastRecoveryReconnect) >= pl.recoveryInterval
}

func (pl *phoneLiveness) startRecovery() {
	pl.lock.Lock()
	pl.lastRecoveryReconnect = time.Now()
	pl.lock.Unlock()
}

func (pl *phoneLiveness) getRecoveryInterval() time.Duration {
	pl.lock.Lock()
	defer pl.lock.Unlock()
	if pl.recoveryInterval == 0 {
		pl.recoveryInterval = initialRecoveryInterval
	}
	return pl.recoveryInterval
}

func (pl *phoneLiveness) advanceRecoveryInterval() {
	pl.lock.Lock()
	if pl.recoveryInterval < maxRepingTickerTime {
		pl.recoveryInterval *= 2
	}
	pl.lock.Unlock()
}

// Goals of the ditto pinger:
//   - By default, send pings to the phone every minute
//   - If an outgoing request the user is waiting on doesn't respond quickly, send a ping
//     immediately
//   - If the first ping doesn't respond, send PhoneNotResponding
//     (to avoid the bridge being stuck in the CONNECTING state)
//   - If a ping doesn't respond, send new pings on increasing intervals
//     (starting from 1 minute up to 1 hour) until it responds, escalating to re-arming the
//     phone's event subscription and then to reconnecting, since those recover the cases
//     that pinging alone never will
//   - Only send PhoneNotResponding once the phone has missed alertTimeoutCount pings in a
//     row (~8 minutes), however those pings were triggered. Phones doze constantly, so a
//     single missed ping is normal and telling the user about it just trains them to
//     ignore the warning.
//   - If a request timeout happens during backoff pings, skip the rest of the backoff and
//     send PhoneNotResponding as soon as that threshold is met
//   - If a ping responds, or any data arrives from the phone, and PhoneNotResponding was
//     sent, send PhoneRespondingAgain
//
// Waiting for pings never happens on the pinger's own goroutine: while the phone is
// unresponsive the loop still has to run data receive checks, which are the only thing
// that notices silently stalled event delivery.
type dittoPinger struct {
	client *Client

	pingInterval      time.Duration
	alertTimeoutCount int
	recovering        atomic.Bool

	stop <-chan struct{}
	log  *zerolog.Logger
}

type resetter struct {
	C chan struct{}
	d atomic.Bool
}

func newResetter() *resetter {
	return &resetter{
		C: make(chan struct{}),
	}
}

func (r *resetter) Done() {
	if r.d.CompareAndSwap(false, true) {
		go func() {
			time.Sleep(5 * time.Second)
			close(r.C)
		}()
	}
}

func (dp *dittoPinger) OnRespond(pingID uint64, dur time.Duration, reset *resetter) {
	notRespondingSent, wasFailing, needsCatchUp := dp.client.phone.onLiveness()
	logEvt := dp.log.Debug().Uint64("ping_id", pingID).Dur("duration", dur)
	switch {
	case notRespondingSent:
		logEvt.Msg("Ditto ping successful (phone is back online)")
	case wasFailing:
		logEvt.Msg("Ditto ping successful (stopped failing)")
	default:
		logEvt.Msg("Ditto ping successful")
	}
	if wasFailing {
		// TODO separate event for the case where PhoneNotResponding was never sent?
		dp.client.triggerEvent(&events.PhoneRespondingAgain{})
	}
	if needsCatchUp {
		go dp.client.requestUpdatesAfterGap(dp.log, "phone started responding to pings again")
	}
	reset.Done()
}

func (dp *dittoPinger) OnTimeout(pingID uint64, urgent bool, reset *resetter) {
	timeouts, sendNotResponding := dp.client.phone.onTimeout(dp.alertTimeoutCount)
	dp.log.Warn().
		Uint64("ping_id", pingID).
		Int("timeout_count", timeouts).
		Bool("urgent", urgent).
		Msg("Ditto ping is taking long, phone may be offline")
	if sendNotResponding {
		dp.client.triggerEvent(&events.PhoneNotResponding{})
	}
	dp.startRecovery(reset)
}

type pendingPing struct {
	id        uint64
	requestID string
	start     time.Time
	ch        chan *IncomingRPCMessage
}

func (dp *dittoPinger) WaitForResponse(ping pendingPing, timeout time.Duration, reset *resetter) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ping.ch:
		dp.OnRespond(ping.id, time.Since(ping.start), reset)
		return
	case <-timer.C:
		dp.OnTimeout(ping.id, timeout == shortPingTimeout, reset)
	case <-reset.C:
		dp.log.Debug().
			Uint64("ping_id", ping.id).
			Msg("Another ping was successful, giving up on this one")
	case <-dp.stop:
	}
	// Stop waiting for the response, but leave detecting a late one to onPhoneActivity:
	// the phone answering at all is what matters, not which request it was answering.
	dp.client.sessionHandler.cancelResponse(ping.requestID, ping.ch)
}

func (dp *dittoPinger) Ping(pingID uint64, timeout time.Duration, reset *resetter) {
	pl := &dp.client.phone
	pl.lock.Lock()
	if time.Since(pl.lastPingTime) < minPingInterval {
		dp.log.Debug().
			Uint64("ping_id", pingID).
			Time("last_ping_time", pl.lastPingTime).
			Msg("Skipping ping since last one was too recently")
		pl.lock.Unlock()
		return
	}
	now := time.Now()
	pl.lastPingTime = now
	pl.lock.Unlock()

	// Publish the generation only for pings that are actually sent, so data from the phone
	// always cancels the waiters that are really outstanding.
	pl.currentReset.Store(reset)

	pingChan, requestID, err := dp.client.notifyDittoActivity(dp.log.WithContext(context.TODO()))
	if err != nil {
		pl.lock.Lock()
		pl.sendFails++
		sendFails := pl.sendFails
		pl.lock.Unlock()
		dp.log.Err(err).Uint64("ping_id", pingID).Msg("Error sending ping")
		dp.client.triggerEvent(&events.PingFailed{
			Error:      fmt.Errorf("failed to notify ditto activity: %w", err),
			ErrorCount: sendFails,
		})
		return
	}
	go dp.WaitForResponse(pendingPing{
		id:        pingID,
		requestID: requestID,
		start:     now,
		ch:        pingChan,
	}, timeout, reset)
}

// startRecovery hands an unresponsive phone off to a background goroutine, unless recovery
// is already running.
func (dp *dittoPinger) startRecovery(reset *resetter) {
	if dp.recovering.CompareAndSwap(false, true) {
		go dp.recoveryLoop(reset)
	}
}

// recoveryLoop re-pings an unresponsive phone on increasing intervals, escalating to
// session-level recovery as the failures add up: first re-arming the phone's event
// subscription, then reconnecting to get a new listener session. Reconnects are rate
// limited to the current backoff interval, so a phone that stays offline for hours is
// probed less and less often rather than being reconnected in a loop.
func (dp *dittoPinger) recoveryLoop(reset *resetter) {
	defer dp.recovering.Store(false)
	pl := &dp.client.phone
	didSetActiveSession := false
	for {
		select {
		case <-time.After(pl.getRecoveryInterval()):
		case <-reset.C:
			dp.log.Debug().Msg("Phone responded, stopping ditto ping recovery")
			return
		case <-dp.stop:
			return
		}
		// The connection may have been torn down while waiting, in which case recovering it
		// would resurrect a client the bridge has already disconnected.
		select {
		case <-dp.stop:
			return
		default:
		}
		timeouts := pl.timeoutCount()
		if timeouts == 0 {
			dp.log.Debug().Msg("Phone is responding again, stopping ditto ping recovery")
			return
		}
		ctx := dp.log.WithContext(context.TODO())
		reconnected := false
		switch {
		case timeouts >= reconnectAfterTimeouts && pl.canDoRecovery():
			dp.log.Warn().
				Int("timeout_count", timeouts).
				Msg("Phone hasn't responded to pings, reconnecting to get a new listener session")
			pl.startRecovery()
			if err := dp.client.Reconnect(); err != nil {
				dp.log.Err(err).Msg("Failed to reconnect while recovering from ping timeouts")
				return
			}
			reconnected = true
		case timeouts >= setActiveSessionAfterTimeouts && !didSetActiveSession:
			didSetActiveSession = true
			dp.log.Warn().
				Int("timeout_count", timeouts).
				Msg("Phone hasn't responded to pings, re-arming its event subscription")
			if err := dp.client.SetActiveSession(ctx); err != nil {
				dp.log.Err(err).Msg("Failed to set active session while recovering from ping timeouts")
			}
		}
		pl.advanceRecoveryInterval()
		if reconnected {
			return
		}
		pingID := pingIDCounter.Add(1)
		dp.log.Debug().
			Uint64("ping_id", pingID).
			Int("timeout_count", timeouts).
			Msg("Sending recovery ping")
		dp.Ping(pingID, defaultPingTimeout, reset)
	}
}

func (dp *dittoPinger) Loop() {
	for {
		select {
		case <-dp.client.pingShortCircuit:
			if dp.recovering.Load() {
				// Recovery pings are already in flight, but the user is actively waiting
				// for a request, so don't make them wait out the backoff for a notice.
				if dp.client.phone.shouldMarkNotResponding(dp.alertTimeoutCount) {
					dp.log.Debug().Msg("Ditto ping wait short-circuited during recovery, sending PhoneNotResponding immediately")
					dp.client.triggerEvent(&events.PhoneNotResponding{})
				} else {
					dp.log.Debug().Msg("Ditto ping wait short-circuited during recovery")
				}
			} else {
				pingID := pingIDCounter.Add(1)
				dp.log.Debug().Uint64("ping_id", pingID).Msg("Ditto ping wait short-circuited")
				dp.Ping(pingID, shortPingTimeout, newResetter())
			}
		case <-time.After(dp.pingInterval):
			if dp.recovering.Load() {
				dp.log.Trace().Msg("Skipping normal ditto ping, recovery pings are already in progress")
			} else {
				pingID := pingIDCounter.Add(1)
				dp.log.Trace().Uint64("ping_id", pingID).Msg("Doing normal ditto ping")
				dp.Ping(pingID, defaultPingTimeout, newResetter())
			}
		case <-dp.stop:
			return
		}
		if dp.client.shouldDoDataReceiveCheck() {
			dp.log.Warn().Msg("No data received recently, sending extra GET_UPDATES call")
			go dp.client.requestUpdatesAfterGap(dp.log, "no data received recently")
		}
	}
}

// onPhoneActivity is called when data is received from the phone outside of the ditto pings and
// is counted as liveness just like pings.
func (c *Client) onPhoneActivity(source string) {
	notRespondingSent, wasFailing, needsCatchUp := c.phone.onLiveness()
	if !wasFailing {
		return
	}
	c.Logger.Debug().
		Str("liveness_source", source).
		Bool("not_responding_sent", notRespondingSent).
		Msg("Received data from phone while it was considered unresponsive")
	// Stop any recovery ladder: the phone is reachable, so escalating to a reconnect
	// would only interrupt a working connection.
	if reset := c.phone.currentReset.Load(); reset != nil {
		reset.Done()
	}
	if notRespondingSent {
		c.triggerEvent(&events.PhoneRespondingAgain{})
	}
	if needsCatchUp {
		go c.requestUpdatesAfterGap(&c.Logger, "phone started responding again")
	}
}

func (c *Client) requestUpdatesAfterGap(log *zerolog.Logger, reason string) {
	c.triggerEvent(&events.NoDataReceived{})
	err := c.sessionHandler.sendMessageNoResponse(log.WithContext(context.TODO()), SendMessageParams{
		Action:    gmproto.ActionType_GET_UPDATES,
		OmitTTL:   true,
		RequestID: c.sessionHandler.sessionID,
	})
	if err != nil {
		log.Err(err).Str("reason", reason).Msg("Failed to send extra GET_UPDATES call")
	} else {
		log.Debug().Str("reason", reason).Msg("Sent extra GET_UPDATES call")
	}
}

func (c *Client) shouldDoDataReceiveCheck() bool {
	c.nextDataReceiveCheckLock.Lock()
	defer c.nextDataReceiveCheckLock.Unlock()
	if time.Until(c.nextDataReceiveCheck) <= 0 {
		c.nextDataReceiveCheck = time.Now().Add(c.dataReceiveCheckInterval)
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

func (c *Client) doLongPoll(loggedIn, background bool, onFirstConnect func()) bool {
	c.listenID++
	listenID := c.listenID
	listenReqID := uuid.NewString()

	log := c.Logger.With().Int("listen_id", listenID).Logger()
	defer func() {
		log.Debug().Msg("Long polling stopped")
	}()
	ctx := log.WithContext(context.TODO())
	log.Debug().Str("listen_uuid", listenReqID).Msg("Long polling starting")

	if loggedIn {
		stopDittoPinger := make(chan struct{})
		defer close(stopDittoPinger)
		go (&dittoPinger{
			pingInterval:      c.pingInterval,
			alertTimeoutCount: c.alertTimeoutCount,
			stop:              stopDittoPinger,
			log:               &log,
			client:            c,
		}).Loop()
	}

	errorCount := 1
	var disconnectedAt time.Time
	for c.listenID == listenID {
		err := c.refreshAuthToken(nil)
		if err != nil {
			if isFatalRefreshError(err) {
				log.Err(err).Msg("Error refreshing auth token")
				if loggedIn {
					c.triggerEvent(&events.ListenFatalError{Error: fmt.Errorf("failed to refresh auth token: %w", err)})
				}
				return false
			}
			if loggedIn {
				c.triggerEvent(&events.ListenTemporaryError{Error: fmt.Errorf("failed to refresh auth token: %w", err)})
			}
			errorCount++
			sleepSeconds := (errorCount + 1) * 5
			if background {
				if errorCount >= 3 {
					return false
				}
				sleepSeconds = errorCount * 2
			}
			log.Err(err).Int("sleep_seconds", sleepSeconds).Msg("Error refreshing auth token, retrying in a while")
			time.Sleep(time.Duration(sleepSeconds) * time.Second)
			continue
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
			if background {
				if errorCount >= 3 {
					return false
				}
				sleepSeconds = errorCount * 2
			}
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
			return false
		} else if resp.StatusCode >= 400 {
			if loggedIn {
				c.triggerEvent(&events.ListenTemporaryError{Error: events.HTTPError{Action: "polling", Resp: resp, Body: tryReadBody(resp.Body)}})
			} else {
				_ = resp.Body.Close()
			}
			errorCount++
			sleepSeconds := (errorCount + 1) * 5
			if background {
				if errorCount >= 3 {
					return false
				}
				sleepSeconds = errorCount * 2
			}
			log.Debug().
				Int("statusCode", resp.StatusCode).
				Int("sleep_seconds", sleepSeconds).
				Msg("Error in long polling, retrying in a while")
			time.Sleep(time.Duration(sleepSeconds) * time.Second)
			continue
		}
		if c.listenID != listenID {
			log.Debug().Msg("Long polling stopped while opening stream, closing it")
			_ = resp.Body.Close()
			return true
		}
		if errorCount > 0 {
			errorCount = 0
			if loggedIn {
				c.triggerEvent(&events.ListenRecovered{})
			}
		}
		log.Debug().Int("statusCode", resp.StatusCode).Msg("Long polling opened")
		c.longPollingConn = resp.Body
		if loggedIn && !disconnectedAt.IsZero() && time.Since(disconnectedAt) > longPollGapCatchUp {
			// Events the phone sent while the connection was down may not be redelivered.
			log.Warn().
				Time("disconnected_at", disconnectedAt).
				Msg("Long polling was disconnected for a while, requesting updates")
			go c.requestUpdatesAfterGap(&log, "long polling was disconnected")
		}
		if onFirstConnect != nil {
			go onFirstConnect()
			onFirstConnect = nil
		}
		cleanClose := c.readLongPoll(&log, resp.Body, background)
		c.longPollingConn = nil
		if background {
			return cleanClose
		}
		disconnectedAt = time.Now()
	}
	return true
}

func (c *Client) readLongPoll(log *zerolog.Logger, rc io.ReadCloser, background bool) bool {
	defer rc.Close()
	c.disconnecting = false
	reader := bufio.NewReader(rc)
	buf := make([]byte, 2621440)
	var accumulatedData []byte
	n, err := reader.Read(buf[:2])
	if err != nil {
		log.Err(err).Msg("Error reading opening bytes")
		return false
	} else if n != 2 || string(buf[:2]) != "[[" {
		log.Err(err).Msg("Opening is not [[")
		return false
	}
	var closeIn *time.Timer
	receivedEvents := false
	onRead := func() {
		if closeIn == nil {
			return
		}
		if receivedEvents {
			closeIn.Reset(3 * time.Second)
		} else {
			closeIn.Reset(5 * time.Second)
		}
	}
	if background {
		closeIn = time.NewTimer(10 * time.Second)
		streamEnded := make(chan struct{})
		defer close(streamEnded)
		go func() {
			select {
			case <-closeIn.C:
				c.closeLongPolling()
			case <-streamEnded:
			}
		}()
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
			return receivedEvents
		} else if expectEOF {
			log.Warn().Msg("Didn't get EOF after stream end marker")
		}
		onRead()
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
			receivedEvents = true
			onRead()
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
	conn := c.longPollingConn
	c.Logger.Debug().
		Int("current_listen_id", c.listenID).
		Bool("connection_open", conn != nil).
		Msg("Closing long polling connection manually")
	c.listenID++
	c.disconnecting = true
	if conn != nil {
		_ = conn.Close()
		c.longPollingConn = nil
	}
}
