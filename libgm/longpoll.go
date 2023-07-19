package libgm

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"go.mau.fi/mautrix-gmessages/libgm/events"
	"go.mau.fi/mautrix-gmessages/libgm/pblite"

	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

func (c *Client) doLongPoll(loggedIn bool) {
	c.listenID++
	listenID := c.listenID
	errored := true
	listenReqID := uuid.NewString()
	for c.listenID == listenID {
		err := c.refreshAuthToken()
		if err != nil {
			c.Logger.Err(err).Msg("Error refreshing auth token")
			if loggedIn {
				c.triggerEvent(&events.ListenFatalError{Error: fmt.Errorf("failed to refresh auth token: %w", err)})
			}
			return
		}
		c.Logger.Debug().Msg("Starting new long-polling request")
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
			errored = true
			c.Logger.Err(err).Msg("Error making listen request, retrying in 5 seconds")
			time.Sleep(5 * time.Second)
			continue
		}
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			c.Logger.Error().Int("status_code", resp.StatusCode).Msg("Error making listen request")
			if loggedIn {
				c.triggerEvent(&events.ListenFatalError{Error: events.HTTPError{Action: "polling", Resp: resp}})
			}
			return
		} else if resp.StatusCode >= 500 {
			if loggedIn {
				c.triggerEvent(&events.ListenTemporaryError{Error: events.HTTPError{Action: "polling", Resp: resp}})
			}
			errored = true
			c.Logger.Debug().Int("statusCode", resp.StatusCode).Msg("5xx error in long polling, retrying in 5 seconds")
			time.Sleep(5 * time.Second)
			continue
		}
		if errored {
			errored = false
			if loggedIn {
				c.triggerEvent(&events.ListenRecovered{})
			}
		}
		c.Logger.Debug().Int("statusCode", resp.StatusCode).Msg("Long polling opened")
		c.longPollingConn = resp.Body
		if c.AuthData.Browser != nil {
			go func() {
				err := c.NotifyDittoActivity()
				if err != nil {
					c.Logger.Err(err).Msg("Error notifying ditto activity")
				}
			}()
		}
		c.readLongPoll(resp.Body)
		c.longPollingConn = nil
	}
}

func (c *Client) readLongPoll(rc io.ReadCloser) {
	defer rc.Close()
	c.disconnecting = false
	reader := bufio.NewReader(rc)
	buf := make([]byte, 2621440)
	var accumulatedData []byte
	n, err := reader.Read(buf[:2])
	if err != nil {
		c.Logger.Err(err).Msg("Error reading opening bytes")
		return
	} else if n != 2 || string(buf[:2]) != "[[" {
		c.Logger.Err(err).Msg("Opening is not [[")
		return
	}
	var expectEOF bool
	for {
		n, err = reader.Read(buf)
		if err != nil {
			var logEvt *zerolog.Event
			if (errors.Is(err, io.EOF) && expectEOF) || c.disconnecting {
				logEvt = c.Logger.Debug()
			} else {
				logEvt = c.Logger.Warn()
			}
			logEvt.Err(err).Msg("Stopped reading data from server")
			return
		} else if expectEOF {
			c.Logger.Warn().Msg("Didn't get EOF after stream end marker")
		}
		chunk := buf[:n]
		if len(accumulatedData) == 0 {
			if len(chunk) == 2 && string(chunk) == "]]" {
				c.Logger.Debug().Msg("Got stream end marker")
				expectEOF = true
				continue
			}
			chunk = bytes.TrimPrefix(chunk, []byte{','})
		}
		accumulatedData = append(accumulatedData, chunk...)
		if !json.Valid(accumulatedData) {
			c.Logger.Trace().Bytes("data", chunk).Msg("Invalid JSON, reading next chunk")
			continue
		}
		currentBlock := accumulatedData
		accumulatedData = accumulatedData[:0]
		msg := &gmproto.LongPollingPayload{}
		err = pblite.Unmarshal(currentBlock, msg)
		if err != nil {
			c.Logger.Err(err).Msg("Error deserializing pblite message")
			continue
		}
		switch {
		case msg.GetData() != nil:
			c.HandleRPCMsg(msg.GetData())
		case msg.GetAck() != nil:
			c.Logger.Debug().Int32("count", msg.GetAck().GetCount()).Msg("Got startup ack count message")
			c.skipCount = int(msg.GetAck().GetCount())
		case msg.GetStartRead() != nil:
			c.Logger.Trace().Msg("Got startRead message")
		case msg.GetHeartbeat() != nil:
			c.Logger.Trace().Msg("Got heartbeat message")
		default:
			c.Logger.Warn().
				Str("data", base64.StdEncoding.EncodeToString(currentBlock)).
				Msg("Got unknown message")
		}
	}
}

func (c *Client) closeLongPolling() {
	if conn := c.longPollingConn; conn != nil {
		c.Logger.Debug().Msg("Closing long polling connection manually")
		c.listenID++
		c.disconnecting = true
		_ = conn.Close()
		c.longPollingConn = nil
	}
}
