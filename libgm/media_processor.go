package libgm

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strconv"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/crypto"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

type StartGoogleUpload struct {
	UploadID         string
	UploadURL        string
	UploadStatus     string
	ChunkGranularity int64
	ControlURL       string

	Image               *Image
	EncryptedMediaBytes []byte
}

type MediaUpload struct {
	MediaID     string
	MediaNumber int64
	Image       *Image
}

var (
	errStartUploadMedia    = errors.New("failed to start uploading media")
	errFinalizeUploadMedia = errors.New("failed to finalize uploading media")
)

func (c *Client) FinalizeUploadMedia(upload *StartGoogleUpload) (*MediaUpload, error) {
	imageType := upload.Image.GetImageType()
	encryptedImageSize := strconv.Itoa(len(upload.EncryptedMediaBytes))

	finalizeUploadHeaders := util.NewMediaUploadHeaders(encryptedImageSize, "upload, finalize", "0", imageType.Format, "")
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
	googleResponse, err3 := io.ReadAll(res.Body)
	if err3 != nil {
		return nil, err3
	}

	uploadStatus := rHeaders.Get("x-goog-upload-status")
	c.Logger.Debug().Str("upload_status", uploadStatus).Msg("Upload status")

	mediaIDs := &binary.UploadMediaResponse{}
	err3 = crypto.DecodeAndEncodeB64(string(googleResponse), mediaIDs)
	if err3 != nil {
		return nil, err3
	}
	return &MediaUpload{
		MediaID:     mediaIDs.Media.MediaID,
		MediaNumber: mediaIDs.Media.MediaNumber,
		Image:       upload.Image,
	}, nil
}

func (c *Client) StartUploadMedia(image *Image) (*StartGoogleUpload, error) {
	imageType := image.GetImageType()

	encryptedImageBytes, encryptErr := image.GetEncryptedBytes()
	if encryptErr != nil {
		return nil, encryptErr
	}
	encryptedImageSize := strconv.Itoa(len(encryptedImageBytes))

	startUploadHeaders := util.NewMediaUploadHeaders(encryptedImageSize, "start", "", imageType.Format, "resumable")
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

		Image:               image,
		EncryptedMediaBytes: encryptedImageBytes,
	}
	return uploadResponse, nil
}

func (c *Client) buildStartUploadPayload() (string, error) {
	requestId := util.RandomUUIDv4()
	protoData := &binary.StartMediaUploadPayload{
		ImageType: 1,
		AuthData: &binary.AuthMessage{
			RequestID: requestId,
			RpcKey:    c.rpcKey,
			Date: &binary.Date{
				Year: 2023,
				Seq1: 6,
				Seq2: 22,
				Seq3: 4,
				Seq4: 6,
			},
		},
		Mobile: c.devicePair.Mobile,
	}

	protoDataEncoded, protoEncodeErr := crypto.EncodeProtoB64(protoData)
	if protoEncodeErr != nil {
		return "", protoEncodeErr
	}

	return protoDataEncoded, nil
}
