package libgm

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strconv"

	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/payload"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

type StartGoogleUpload struct {
	UploadID         string
	UploadURL        string
	UploadStatus     string
	ChunkGranularity int64
	ControlURL       string
	MimeType         string

	EncryptedMediaBytes []byte
}

type MediaUpload struct {
	MediaID     string
	MediaNumber int64
}

var (
	errStartUploadMedia    = errors.New("failed to start uploading media")
	errFinalizeUploadMedia = errors.New("failed to finalize uploading media")
)

func (c *Client) FinalizeUploadMedia(upload *StartGoogleUpload) (*MediaUpload, error) {
	encryptedImageSize := strconv.Itoa(len(upload.EncryptedMediaBytes))

	finalizeUploadHeaders := util.NewMediaUploadHeaders(encryptedImageSize, "upload, finalize", "0", upload.MimeType, "")
	req, reqErr := http.NewRequest("POST", upload.UploadURL, bytes.NewBuffer(upload.EncryptedMediaBytes))
	if reqErr != nil {
		return nil, reqErr
	}

	req.Header = *finalizeUploadHeaders

	res, resErr := c.http.Do(req)
	if resErr != nil {
		panic(resErr)
	}

	statusCode := res.StatusCode
	if statusCode != 200 {
		return nil, errFinalizeUploadMedia
	}

	defer res.Body.Close()

	rHeaders := res.Header
	googleResponse, err3 := io.ReadAll(base64.NewDecoder(base64.StdEncoding, res.Body))
	if err3 != nil {
		return nil, err3
	}

	uploadStatus := rHeaders.Get("x-goog-upload-status")
	c.Logger.Debug().Str("upload_status", uploadStatus).Msg("Upload complete")

	mediaIDs := &binary.UploadMediaResponse{}
	err3 = proto.Unmarshal(googleResponse, mediaIDs)
	if err3 != nil {
		return nil, err3
	}
	return &MediaUpload{
		MediaID:     mediaIDs.Media.MediaID,
		MediaNumber: mediaIDs.Media.MediaNumber,
	}, nil
}

func (c *Client) StartUploadMedia(encryptedImageBytes []byte, mime string) (*StartGoogleUpload, error) {
	encryptedImageSize := strconv.Itoa(len(encryptedImageBytes))

	startUploadHeaders := util.NewMediaUploadHeaders(encryptedImageSize, "start", "", mime, "resumable")
	startUploadPayload, buildPayloadErr := c.buildStartUploadPayload()
	if buildPayloadErr != nil {
		return nil, buildPayloadErr
	}

	req, reqErr := http.NewRequest("POST", util.UPLOAD_MEDIA, bytes.NewBuffer([]byte(startUploadPayload)))
	if reqErr != nil {
		return nil, reqErr
	}

	req.Header = *startUploadHeaders

	res, resErr := c.http.Do(req)
	if resErr != nil {
		panic(resErr)
	}

	statusCode := res.StatusCode
	if statusCode != 200 {
		return nil, errStartUploadMedia
	}

	rHeaders := res.Header

	chunkGranularity, convertErr := strconv.Atoi(rHeaders.Get("x-goog-upload-chunk-granularity"))
	if convertErr != nil {
		return nil, convertErr
	}

	uploadResponse := &StartGoogleUpload{
		UploadID:         rHeaders.Get("x-guploader-uploadid"),
		UploadURL:        rHeaders.Get("x-goog-upload-url"),
		UploadStatus:     rHeaders.Get("x-goog-upload-status"),
		ChunkGranularity: int64(chunkGranularity),
		ControlURL:       rHeaders.Get("x-goog-upload-control-url"),
		MimeType:         mime,

		EncryptedMediaBytes: encryptedImageBytes,
	}
	return uploadResponse, nil
}

func (c *Client) buildStartUploadPayload() (string, error) {
	requestID := util.RandomUUIDv4()
	protoData := &binary.StartMediaUploadPayload{
		ImageType: 1,
		AuthData: &binary.AuthMessage{
			RequestID:        requestID,
			TachyonAuthToken: c.authData.TachyonAuthToken,
			ConfigVersion:    payload.ConfigMessage,
		},
		Mobile: c.authData.DevicePair.Mobile,
	}

	protoDataBytes, err := proto.Marshal(protoData)
	if err != nil {
		return "", err
	}
	protoDataEncoded := base64.StdEncoding.EncodeToString(protoDataBytes)

	return protoDataEncoded, nil
}
