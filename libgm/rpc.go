package libgm

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"

	"go.mau.fi/mautrix-gmessages/libgm/events"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

type RPC struct {
	client       *Client
	http         *http.Client
	conn         io.ReadCloser
	rpcSessionID string
	webAuthKey   []byte
	listenID     int
}

func (r *RPC) ListenReceiveMessages(payload []byte) {
	r.listenID++
	listenID := r.listenID
	for r.listenID == listenID {
		r.client.Logger.Debug().Msg("Starting new long-polling request")
		req, err := http.NewRequest("POST", util.RECEIVE_MESSAGES, bytes.NewReader(payload))
		if err != nil {
			panic(fmt.Errorf("Error creating request: %v", err))
		}
		util.BuildRelayHeaders(req, "application/json+protobuf", "*/*")
		resp, reqErr := r.http.Do(req)
		//r.client.Logger.Info().Any("bodyLength", len(payload)).Any("url", util.RECEIVE_MESSAGES).Any("headers", resp.Request.Header).Msg("RPC Request Headers")
		if reqErr != nil {
			panic(fmt.Errorf("Error making request: %v", err))
		}
		r.client.Logger.Debug().Int("statusCode", resp.StatusCode).Msg("Long polling opened")
		r.conn = resp.Body
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
		if n <= 25 {
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
		r.client.Logger.Info().Any("val", msgArr).Msg("MsgArr")
		go r.HandleRPCMsg(msgArr)
	}
}

func (r *RPC) isStartRead(data []byte) bool {
	return string(data) == "[[[null,null,null,[]]"
}

func (r *RPC) isHeartBeat(data []byte) bool {
	return string(data) == ",[null,null,[]]"
}

/*
func (r *RPC) startReadingData(rc io.ReadCloser) {
	defer rc.Close()
	reader := bufio.NewReader(rc)
	buf := make([]byte, 5242880)
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
		var msgArr []interface{}
		isComplete := r.tryUnmarshalJSON(chunk, &msgArr)
		r.client.Logger.Info().Any("val", chunk[0] == 44).Any("isComplete", string(chunk)).Msg("is Start?")
		go r.HandleRPCMsg(buf[:n])
	}
}
*/

func (r *RPC) CloseConnection() {
	if r.conn != nil {
		r.listenID++
		r.client.Logger.Debug().Msg("Attempting to connection...")
		r.conn.Close()
		r.conn = nil
	}
}

func (r *RPC) sendMessageRequest(url string, payload []byte) (*http.Response, error) {
	req, err := http.NewRequest("POST", url, bytes.NewReader(payload))
	if err != nil {
		panic(fmt.Errorf("Error creating request: %v", err))
	}
	util.BuildRelayHeaders(req, "application/json+protobuf", "*/*")
	resp, reqErr := r.client.http.Do(req)
	//r.client.Logger.Info().Any("bodyLength", len(payload)).Any("url", url).Any("headers", resp.Request.Header).Msg("RPC Request Headers")
	if reqErr != nil {
		panic(fmt.Errorf("Error making request: %v", err))
	}
	return resp, reqErr
}

func (r *RPC) sendInitialData() error {
	sessionResponse, err := r.client.Session.SetActiveSession()
	if err != nil {
		return err
	}

	_, convErr := r.client.Conversations.List(25)
	if convErr != nil {
		return convErr
	}

	evtData := events.NewClientReady(sessionResponse)
	r.client.triggerEvent(evtData)
	r.client.sessionHandler.startAckInterval()
	return nil
}
