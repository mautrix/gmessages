package libgm

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"go.mau.fi/mautrix-gmessages/libgm/events"
	"go.mau.fi/mautrix-gmessages/libgm/pblite"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

type RPC struct {
	client       *Client
	http         *http.Client
	conn         io.ReadCloser
	rpcSessionId string
	listenID     int

	skipCount int

	recentUpdates    [8][32]byte
	recentUpdatesPtr int
}

func (r *RPC) ListenReceiveMessages(payload []byte) {
	r.listenID++
	listenID := r.listenID
	errored := true
	for r.listenID == listenID {
		if r.client.authData.DevicePair != nil && r.client.authData.AuthenticatedAt.Add(20*time.Hour).Before(time.Now()) {
			r.client.Logger.Debug().Msg("Refreshing auth token before starting new long-polling request")
			err := r.client.refreshAuthToken()
			if err != nil {
				r.client.Logger.Err(err).Msg("Error refreshing auth token")
				return
			}
		}
		r.client.Logger.Debug().Msg("Starting new long-polling request")
		req, err := http.NewRequest("POST", util.RECEIVE_MESSAGES, bytes.NewReader(payload))
		if err != nil {
			panic(fmt.Errorf("Error creating request: %v", err))
		}
		util.BuildRelayHeaders(req, "application/json+protobuf", "*/*")
		resp, reqErr := r.http.Do(req)
		//r.client.Logger.Info().Any("bodyLength", len(payload)).Any("url", util.RECEIVE_MESSAGES).Any("headers", resp.Request.Header).Msg("RPC Request Headers")
		if reqErr != nil {
			r.client.triggerEvent(&events.ListenTemporaryError{Error: reqErr})
			errored = true
			r.client.Logger.Err(err).Msg("Error making listen request, retrying in 5 seconds")
			time.Sleep(5 * time.Second)
			continue
		}
		if resp.StatusCode >= 400 && resp.StatusCode < 501 {
			r.client.Logger.Error().Int("status_code", resp.StatusCode).Msg("Error making listen request")
			r.client.triggerEvent(&events.ListenFatalError{Resp: resp})
			return
		} else if resp.StatusCode >= 500 {
			r.client.triggerEvent(&events.ListenTemporaryError{Error: fmt.Errorf("http %d while polling", resp.StatusCode)})
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
		if r.client.authData.DevicePair != nil {
			go func() {
				err := r.client.Session.NotifyDittoActivity()
				if err != nil {
					r.client.Logger.Err(err).Msg("Error notifying ditto activity")
				}
			}()
		}
		r.startReadingData(resp.Body)
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
	defer rc.Close()
	reader := bufio.NewReader(rc)
	buf := make([]byte, 2621440)
	var accumulatedData []byte
	for {
		n, err := reader.Read(buf)
		if err != nil {
			if errors.Is(err, os.ErrClosed) {
				r.client.Logger.Err(err).Msg("Closed body from server")
				r.conn = nil
				return
			}
			r.client.Logger.Err(err).Msg("Stopped reading data from server")
			return
		}
		chunk := buf[:n]
		if n <= 25 { // this will catch the acknowledgement message unless you are required to ack 1000 messages for some reason
			isAck := r.isAcknowledgeMessage(chunk)
			if isAck {
				r.client.Logger.Info().Any("isAck", isAck).Msg("Got Ack Message")
			}
			isHeartBeat := r.isHeartBeat(chunk)
			if isHeartBeat {
				r.client.Logger.Info().Any("heartBeat", isHeartBeat).Msg("Got heartbeat message")
			}
			isStartData := r.isStartRead(chunk)
			if isStartData {
				r.client.Logger.Info().Any("startRead", isHeartBeat).Msg("Got startReading message")
			}
			accumulatedData = []byte{}
			continue
		}

		if len(accumulatedData) == 0 {
			chunk = bytes.TrimPrefix(chunk, []byte{44})
		}
		accumulatedData = append(accumulatedData, chunk...)
		var msgArr []interface{}
		err = r.tryUnmarshalJSON(accumulatedData, &msgArr)
		if err != nil {
			//r.client.Logger.Err(err).Any("accumulated", string(accumulatedData)).Msg("Unable to unmarshal data, will wait for more data")
			continue
		}

		accumulatedData = []byte{}
		//r.client.Logger.Info().Any("val", msgArr).Msg("MsgArr")
		r.HandleRPCMsg(msgArr)
	}
}

func (r *RPC) isAcknowledgeMessage(data []byte) bool {
	if data[0] == 44 {
		return false
	}
	if len(data) >= 3 && data[0] == 91 && data[1] == 91 && data[2] == 91 {
		parsed, parseErr := r.parseAckMessage(data)
		if parseErr != nil {
			panic(parseErr)
		}
		r.skipCount = int(parsed.Container.Data.GetAckAmount().Count)
		r.client.Logger.Info().Any("count", r.skipCount).Msg("Messages To Skip")
	} else {
		return false
	}
	return true
}

func (r *RPC) parseAckMessage(data []byte) (*binary.AckMessageResponse, error) {
	data = append(data, 93)
	data = append(data, 93)

	var msgArr []interface{}
	marshalErr := json.Unmarshal(data, &msgArr)
	if marshalErr != nil {
		return nil, marshalErr
	}

	msg := &binary.AckMessageResponse{}
	deserializeErr := pblite.Deserialize(msgArr, msg.ProtoReflect())
	if deserializeErr != nil {
		return nil, deserializeErr
	}
	return msg, nil
}

func (r *RPC) isStartRead(data []byte) bool {
	return string(data) == "[[[null,null,null,[]]"
}

func (r *RPC) isHeartBeat(data []byte) bool {
	return string(data) == ",[null,null,[]]"
}

func (r *RPC) CloseConnection() {
	if r.conn != nil {
		r.client.Logger.Debug().Msg("Attempting to connection...")
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
	//r.client.Logger.Info().Any("bodyLength", len(payload)).Any("url", url).Any("headers", resp.Request.Header).Msg("RPC Request Headers")
	if reqErr != nil {
		return nil, fmt.Errorf("error making request: %w", err)
	}
	return resp, reqErr
}
