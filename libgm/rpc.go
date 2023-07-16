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
	"go.mau.fi/mautrix-gmessages/libgm/payload"
	"go.mau.fi/mautrix-gmessages/libgm/pblite"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

type RPC struct {
	client   *Client
	http     *http.Client
	conn     io.ReadCloser
	stopping bool
	listenID int

	skipCount int

	recentUpdates    [8][32]byte
	recentUpdatesPtr int
}

func (r *RPC) ListenReceiveMessages() {
	r.listenID++
	listenID := r.listenID
	errored := true
	listenReqID := uuid.NewString()
	for r.listenID == listenID {
		err := r.client.refreshAuthToken()
		if err != nil {
			r.client.Logger.Err(err).Msg("Error refreshing auth token")
			r.client.triggerEvent(&events.ListenFatalError{Error: fmt.Errorf("failed to refresh auth token: %w", err)})
			return
		}
		r.client.Logger.Debug().Msg("Starting new long-polling request")
		receivePayload, err := pblite.Marshal(&binary.ReceiveMessagesRequest{
			Auth: &binary.AuthMessage{
				RequestID:        listenReqID,
				TachyonAuthToken: r.client.authData.TachyonAuthToken,
				ConfigVersion:    payload.ConfigMessage,
			},
			Unknown: &binary.ReceiveMessagesRequest_UnknownEmptyObject2{
				Unknown: &binary.ReceiveMessagesRequest_UnknownEmptyObject1{},
			},
		})
		if err != nil {
			panic(fmt.Errorf("Error marshaling request: %v", err))
		}
		req, err := http.NewRequest(http.MethodPost, util.ReceiveMessagesURL, bytes.NewReader(receivePayload))
		if err != nil {
			panic(fmt.Errorf("Error creating request: %v", err))
		}
		util.BuildRelayHeaders(req, "application/json+protobuf", "*/*")
		resp, reqErr := r.http.Do(req)
		if reqErr != nil {
			r.client.triggerEvent(&events.ListenTemporaryError{Error: reqErr})
			errored = true
			r.client.Logger.Err(err).Msg("Error making listen request, retrying in 5 seconds")
			time.Sleep(5 * time.Second)
			continue
		}
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			r.client.Logger.Error().Int("status_code", resp.StatusCode).Msg("Error making listen request")
			r.client.triggerEvent(&events.ListenFatalError{Error: events.HTTPError{Action: "polling", Resp: resp}})
			return
		} else if resp.StatusCode >= 500 {
			r.client.triggerEvent(&events.ListenTemporaryError{Error: events.HTTPError{Action: "polling", Resp: resp}})
			errored = true
			r.client.Logger.Debug().Int("statusCode", resp.StatusCode).Msg("5xx error in long polling, retrying in 5 seconds")
			time.Sleep(5 * time.Second)
			continue
		}
		if errored {
			errored = false
			r.client.triggerEvent(&events.ListenRecovered{})
		}
		r.client.Logger.Debug().Int("statusCode", resp.StatusCode).Msg("Long polling opened")
		r.conn = resp.Body
		if r.client.authData.Browser != nil {
			go func() {
				err := r.client.NotifyDittoActivity()
				if err != nil {
					r.client.Logger.Err(err).Msg("Error notifying ditto activity")
				}
			}()
		}
		r.startReadingData(resp.Body)
		r.conn = nil
	}
}

/*
	The start of a message always begins with byte 44 (",")
	If the message is parsable (after , has been removed) as an array of interfaces:
	func (r *RPC) tryUnmarshalJSON(jsonData []byte, msgArr *[]interface{}) error {
		err := json.Unmarshal(jsonData, &msgArr)
		return err
	}
	then the message is complete and it should continue to the HandleRPCMsg function and it should also reset the buffer so that the next message can be received properly.

	if it's not parsable, it should just append the received data to the buf and attempt to parse it until it's parsable. Because that would indicate that the full msg has been received
*/

func (r *RPC) startReadingData(rc io.ReadCloser) {
	r.stopping = false
	defer rc.Close()
	reader := bufio.NewReader(rc)
	buf := make([]byte, 2621440)
	var accumulatedData []byte
	n, err := reader.Read(buf[:2])
	if err != nil {
		r.client.Logger.Err(err).Msg("Error reading opening bytes")
		return
	} else if n != 2 || string(buf[:2]) != "[[" {
		r.client.Logger.Err(err).Msg("Opening is not [[")
		return
	}
	var expectEOF bool
	for {
		n, err = reader.Read(buf)
		if err != nil {
			var logEvt *zerolog.Event
			if (errors.Is(err, io.EOF) && expectEOF) || r.stopping {
				logEvt = r.client.Logger.Debug()
			} else {
				logEvt = r.client.Logger.Warn()
			}
			logEvt.Err(err).Msg("Stopped reading data from server")
			return
		} else if expectEOF {
			r.client.Logger.Warn().Msg("Didn't get EOF after stream end marker")
		}
		chunk := buf[:n]
		if len(accumulatedData) == 0 {
			if len(chunk) == 2 && string(chunk) == "]]" {
				r.client.Logger.Debug().Msg("Got stream end marker")
				expectEOF = true
				continue
			}
			chunk = bytes.TrimPrefix(chunk, []byte{','})
		}
		accumulatedData = append(accumulatedData, chunk...)
		if !json.Valid(accumulatedData) {
			r.client.Logger.Debug().Str("data", string(chunk)).Msg("Invalid JSON")
			continue
		}
		currentBlock := accumulatedData
		accumulatedData = accumulatedData[:0]
		msg := &binary.InternalMessage{}
		err = pblite.Unmarshal(currentBlock, msg)
		if err != nil {
			r.client.Logger.Err(err).Msg("Error deserializing pblite message")
			continue
		}
		switch {
		case msg.GetData() != nil:
			r.HandleRPCMsg(msg)
		case msg.GetAck() != nil:
			r.client.Logger.Debug().Int32("count", msg.GetAck().GetCount()).Msg("Got startup ack count message")
			r.skipCount = int(msg.GetAck().GetCount())
		case msg.GetStartRead() != nil:
			r.client.Logger.Trace().Msg("Got startRead message")
		case msg.GetHeartbeat() != nil:
			r.client.Logger.Trace().Msg("Got heartbeat message")
		default:
			r.client.Logger.Warn().
				Str("data", base64.StdEncoding.EncodeToString(currentBlock)).
				Msg("Got unknown message")
		}
	}
}

func (r *RPC) CloseConnection() {
	if r.conn != nil {
		r.client.Logger.Debug().Msg("Closing connection manually")
		r.listenID++
		r.stopping = true
		r.conn.Close()
		r.conn = nil
	}
}

func (r *RPC) sendMessageRequest(url string, payload []byte) (*http.Response, error) {
	req, err := http.NewRequest("POST", url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}
	util.BuildRelayHeaders(req, "application/json+protobuf", "*/*")
	resp, reqErr := r.client.http.Do(req)
	if reqErr != nil {
		return nil, fmt.Errorf("error making request: %w", err)
	}
	return resp, reqErr
}
