package libgm

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	"go.mau.fi/mautrix-gmessages/pkg/libgm/crypto"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/util"
)

type MediaType struct {
	Extension string
	Format    string
	Type      gmproto.MediaFormats
}

var MimeToMediaType = map[string]MediaType{
	"image/jpeg":     {Extension: "jpeg", Type: gmproto.MediaFormats_IMAGE_JPEG},
	"image/jpg":      {Extension: "jpg", Type: gmproto.MediaFormats_IMAGE_JPG},
	"image/png":      {Extension: "png", Type: gmproto.MediaFormats_IMAGE_PNG},
	"image/gif":      {Extension: "gif", Type: gmproto.MediaFormats_IMAGE_GIF},
	"image/wbmp":     {Extension: "wbmp", Type: gmproto.MediaFormats_IMAGE_WBMP},
	"image/bmp":      {Extension: "bmp", Type: gmproto.MediaFormats_IMAGE_X_MS_BMP},
	"image/x-ms-bmp": {Extension: "bmp", Type: gmproto.MediaFormats_IMAGE_X_MS_BMP},

	"video/mp4":        {Extension: "mp4", Type: gmproto.MediaFormats_VIDEO_MP4},
	"video/3gpp2":      {Extension: "3gpp2", Type: gmproto.MediaFormats_VIDEO_3G2},
	"video/3gpp":       {Extension: "3gpp", Type: gmproto.MediaFormats_VIDEO_3GPP},
	"video/webm":       {Extension: "webm", Type: gmproto.MediaFormats_VIDEO_WEBM},
	"video/x-matroska": {Extension: "mkv", Type: gmproto.MediaFormats_VIDEO_MKV},

	"audio/aac":      {Extension: "aac", Type: gmproto.MediaFormats_AUDIO_AAC},
	"audio/amr":      {Extension: "amr", Type: gmproto.MediaFormats_AUDIO_AMR},
	"audio/mp3":      {Extension: "mp3", Type: gmproto.MediaFormats_AUDIO_MP3},
	"audio/mpeg":     {Extension: "mpeg", Type: gmproto.MediaFormats_AUDIO_MPEG},
	"audio/mpg":      {Extension: "mpg", Type: gmproto.MediaFormats_AUDIO_MPG},
	"audio/mp4":      {Extension: "mp4", Type: gmproto.MediaFormats_AUDIO_MP4},
	"audio/mp4-latm": {Extension: "latm", Type: gmproto.MediaFormats_AUDIO_MP4_LATM},
	"audio/3gpp":     {Extension: "3gpp", Type: gmproto.MediaFormats_AUDIO_3GPP},
	"audio/ogg":      {Extension: "ogg", Type: gmproto.MediaFormats_AUDIO_OGG},

	"text/vcard":         {Extension: "vcard", Type: gmproto.MediaFormats_TEXT_VCARD},
	"application/pdf":    {Extension: "pdf", Type: gmproto.MediaFormats_APP_PDF},
	"text/plain":         {Extension: "txt", Type: gmproto.MediaFormats_APP_TXT},
	"text/html":          {Extension: "html", Type: gmproto.MediaFormats_APP_HTML},
	"application/msword": {Extension: "doc", Type: gmproto.MediaFormats_APP_DOC},
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document":   {Extension: "docx", Type: gmproto.MediaFormats_APP_DOCX},
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": {Extension: "pptx", Type: gmproto.MediaFormats_APP_PPTX},
	"application/vnd.ms-powerpoint":                                             {Extension: "ppt", Type: gmproto.MediaFormats_APP_PPT},
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         {Extension: "xlsx", Type: gmproto.MediaFormats_APP_XLSX},
	"application/vnd.ms-excel":                                                  {Extension: "xls", Type: gmproto.MediaFormats_APP_XLS},
	"application/vnd.android.package-archive":                                   {Extension: "apk", Type: gmproto.MediaFormats_APP_APK},
	"application/zip":          {Extension: "zip", Type: gmproto.MediaFormats_APP_ZIP},
	"application/java-archive": {Extension: "jar", Type: gmproto.MediaFormats_APP_JAR},
	"text/x-calendar":          {Extension: "vcs", Type: gmproto.MediaFormats_CAL_TEXT_VCALENDAR},
	"text/calendar":            {Extension: "ics", Type: gmproto.MediaFormats_CAL_TEXT_CALENDAR},

	"image":       {Type: gmproto.MediaFormats_IMAGE_UNSPECIFIED},
	"video":       {Type: gmproto.MediaFormats_VIDEO_UNSPECIFIED},
	"audio":       {Type: gmproto.MediaFormats_AUDIO_UNSPECIFIED},
	"application": {Type: gmproto.MediaFormats_APP_UNSPECIFIED},
	"text":        {Type: gmproto.MediaFormats_APP_TXT},
}

var FormatToMediaType = map[gmproto.MediaFormats]MediaType{
	gmproto.MediaFormats_CAL_TEXT_XVCALENDAR: MimeToMediaType["text/x-calendar"],
	gmproto.MediaFormats_CAL_APPLICATION_VCS: MimeToMediaType["text/x-calendar"],
	gmproto.MediaFormats_CAL_APPLICATION_ICS: MimeToMediaType["text/calendar"],
	//gmproto.MediaFormats_CAL_APPLICATION_HBSVCS: ???
}

func init() {
	for key, mediaType := range MimeToMediaType {
		if strings.ContainsRune(key, '/') {
			mediaType.Format = key
		}
		FormatToMediaType[mediaType.Type] = mediaType
	}
}

func (c *Client) UploadMedia(data []byte, fileName, mime string) (*gmproto.MediaContent, error) {
	mediaType := MimeToMediaType[mime]
	if mediaType.Type == 0 {
		mediaType = MimeToMediaType[strings.Split(mime, "/")[0]]
	}
	decryptionKey := crypto.GenerateKey(32)
	cryptor, err := crypto.NewAESGCMHelper(decryptionKey)
	if err != nil {
		return nil, err
	}
	encryptedBytes, err := cryptor.EncryptData(data)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt media: %w", err)
	}
	startUploadImage, err := c.StartUploadMedia(encryptedBytes, mime)
	if err != nil {
		return nil, fmt.Errorf("failed to start upload: %w", err)
	}
	upload, err := c.FinalizeUploadMedia(startUploadImage)
	if err != nil {
		return nil, fmt.Errorf("failed to finalize upload: %w", err)
	}
	return &gmproto.MediaContent{
		Format:        mediaType.Type,
		MediaID:       upload.MediaID,
		MediaName:     fileName,
		Size:          int64(len(data)),
		DecryptionKey: decryptionKey,
		MimeType:      mime,
	}, nil
}

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

func isBase64Character(char byte) bool {
	return (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '+' || char == '/' || char == '='
}

func isStandardBase64(data []byte) bool {
	if len(data)%4 != 0 {
		return false
	}
	for _, char := range data {
		if !isBase64Character(char) {
			return false
		}
	}
	return true
}

func (c *Client) FinalizeUploadMedia(upload *StartGoogleUpload) (*MediaUpload, error) {
	encryptedImageSize := strconv.Itoa(len(upload.EncryptedMediaBytes))

	finalizeUploadHeaders := util.NewMediaUploadHeaders(encryptedImageSize, "upload, finalize", "0", upload.MimeType, "")
	req, err := http.NewRequest(http.MethodPost, upload.UploadURL, bytes.NewBuffer(upload.EncryptedMediaBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to prepare request: %w", err)
	}
	req.Header = *finalizeUploadHeaders

	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected status code %d", res.StatusCode)
	}
	respData, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	if isStandardBase64(respData) {
		n, err := base64.StdEncoding.Decode(respData, respData)
		if err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}
		respData = respData[:n]
	}

	c.Logger.Debug().
		Str("upload_status", res.Header.Get("x-goog-upload-status")).
		Msg("Upload complete")

	mediaIDs := &gmproto.UploadMediaResponse{}
	err = proto.Unmarshal(respData, mediaIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}
	return &MediaUpload{
		MediaID:     mediaIDs.Media.MediaID,
		MediaNumber: mediaIDs.Media.MediaNumber,
	}, nil
}

func (c *Client) StartUploadMedia(encryptedImageBytes []byte, mime string) (*StartGoogleUpload, error) {
	encryptedImageSize := strconv.Itoa(len(encryptedImageBytes))

	startUploadHeaders := util.NewMediaUploadHeaders(encryptedImageSize, "start", "", mime, "resumable")
	startUploadPayload, err := c.buildStartUploadPayload()
	if err != nil {
		return nil, fmt.Errorf("failed to build payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, util.UploadMediaURL, bytes.NewBuffer([]byte(startUploadPayload)))
	if err != nil {
		return nil, fmt.Errorf("failed to prepare request: %w", err)
	}
	req.Header = *startUploadHeaders

	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	_ = res.Body.Close()

	if res.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected status code %d", res.StatusCode)
	}

	chunkGranularity, err := strconv.Atoi(res.Header.Get("x-goog-upload-chunk-granularity"))
	if err != nil {
		return nil, fmt.Errorf("failed to parse chunk granularity: %w", err)
	}

	uploadResponse := &StartGoogleUpload{
		UploadID:         res.Header.Get("x-guploader-uploadid"),
		UploadURL:        res.Header.Get("x-goog-upload-url"),
		UploadStatus:     res.Header.Get("x-goog-upload-status"),
		ChunkGranularity: int64(chunkGranularity),
		ControlURL:       res.Header.Get("x-goog-upload-control-url"),
		MimeType:         mime,

		EncryptedMediaBytes: encryptedImageBytes,
	}
	return uploadResponse, nil
}

func (c *Client) buildStartUploadPayload() (string, error) {
	protoData := &gmproto.StartMediaUploadRequest{
		AttachmentType: 1,
		AuthData: &gmproto.AuthMessage{
			RequestID:        uuid.NewString(),
			TachyonAuthToken: c.AuthData.TachyonAuthToken,
			Network:          c.AuthData.AuthNetwork(),
			ConfigVersion:    util.ConfigMessage,
		},
		Mobile: c.AuthData.Mobile,
	}

	protoDataBytes, err := proto.Marshal(protoData)
	if err != nil {
		return "", err
	}
	protoDataEncoded := base64.StdEncoding.EncodeToString(protoDataBytes)

	return protoDataEncoded, nil
}

func (c *Client) DownloadMedia(mediaID string, key []byte) ([]byte, error) {
	downloadMetadata := &gmproto.DownloadAttachmentRequest{
		Info: &gmproto.AttachmentInfo{
			AttachmentID: mediaID,
			Encrypted:    true,
		},
		AuthData: &gmproto.AuthMessage{
			RequestID:        uuid.NewString(),
			TachyonAuthToken: c.AuthData.TachyonAuthToken,
			Network:          c.AuthData.AuthNetwork(),
			ConfigVersion:    util.ConfigMessage,
		},
	}
	downloadMetadataBytes, err := proto.Marshal(downloadMetadata)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal download request: %w", err)
	}
	downloadMetadataEncoded := base64.StdEncoding.EncodeToString(downloadMetadataBytes)
	req, err := http.NewRequest(http.MethodGet, util.UploadMediaURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare request: %w", err)
	}
	util.BuildUploadHeaders(req, downloadMetadataEncoded)
	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer res.Body.Close()
	respData, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	cryptor, err := crypto.NewAESGCMHelper(key)
	if err != nil {
		return nil, err
	}
	decryptedImageBytes, err := cryptor.DecryptData(respData)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt media: %w", err)
	}
	return decryptedImageBytes, nil
}
