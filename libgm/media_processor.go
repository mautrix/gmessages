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
	UploadId         string
	UploadUrl        string
	UploadStatus     string
	ChunkGranularity int64
	ControlUrl       string

	Image               *Image
	EncryptedMediaBytes []byte
}

type MediaUpload struct {
	MediaId     string
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
	req, reqErr := http.NewRequest("POST", upload.UploadUrl, bytes.NewBuffer(upload.EncryptedMediaBytes))
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

	mediaIds := &binary.UploadMediaResponse{}
	err3 = crypto.DecodeAndEncodeB64(string(googleResponse), mediaIds)
	if err3 != nil {
		return nil, err3
	}
	return &MediaUpload{
		MediaId:     mediaIds.Media.MediaId,
		MediaNumber: mediaIds.Media.MediaNumber,
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
		UploadId:         rHeaders.Get("x-guploader-uploadid"),
		UploadUrl:        rHeaders.Get("x-goog-upload-url"),
		UploadStatus:     rHeaders.Get("x-goog-upload-status"),
		ChunkGranularity: int64(chunkGranularity),
		ControlUrl:       rHeaders.Get("x-goog-upload-control-url"),

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
			RequestId: requestId,
			RpcKey:    c.rpcKey,
			Date: &binary.Date{
				Year: 2023,
				Seq1: 6,
				Seq2: 8,
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
