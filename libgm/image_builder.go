package libgm

import (
	"strings"

	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/crypto"
)

type MediaType struct {
	Extension string
	Format    string
	Type      binary.MediaFormats
}

var MimeToMediaType = map[string]MediaType{
	"image/jpeg":     {Extension: "jpeg", Type: binary.MediaFormats_IMAGE_JPEG},
	"image/jpg":      {Extension: "jpg", Type: binary.MediaFormats_IMAGE_JPG},
	"image/png":      {Extension: "png", Type: binary.MediaFormats_IMAGE_PNG},
	"image/gif":      {Extension: "gif", Type: binary.MediaFormats_IMAGE_GIF},
	"image/wbmp":     {Extension: "wbmp", Type: binary.MediaFormats_IMAGE_WBMP},
	"image/bmp":      {Extension: "bmp", Type: binary.MediaFormats_IMAGE_X_MS_BMP},
	"image/x-ms-bmp": {Extension: "bmp", Type: binary.MediaFormats_IMAGE_X_MS_BMP},

	"video/mp4":        {Extension: "mp4", Type: binary.MediaFormats_VIDEO_MP4},
	"video/3gpp2":      {Extension: "3gpp2", Type: binary.MediaFormats_VIDEO_3G2},
	"video/3gpp":       {Extension: "3gpp", Type: binary.MediaFormats_VIDEO_3GPP},
	"video/webm":       {Extension: "webm", Type: binary.MediaFormats_VIDEO_WEBM},
	"video/x-matroska": {Extension: "mkv", Type: binary.MediaFormats_VIDEO_MKV},

	"audio/aac":      {Extension: "aac", Type: binary.MediaFormats_AUDIO_AAC},
	"audio/amr":      {Extension: "amr", Type: binary.MediaFormats_AUDIO_AMR},
	"audio/mp3":      {Extension: "mp3", Type: binary.MediaFormats_AUDIO_MP3},
	"audio/mpeg":     {Extension: "mpeg", Type: binary.MediaFormats_AUDIO_MPEG},
	"audio/mpg":      {Extension: "mpg", Type: binary.MediaFormats_AUDIO_MPG},
	"audio/mp4":      {Extension: "mp4", Type: binary.MediaFormats_AUDIO_MP4},
	"audio/mp4-latm": {Extension: "latm", Type: binary.MediaFormats_AUDIO_MP4_LATM},
	"audio/3gpp":     {Extension: "3gpp", Type: binary.MediaFormats_AUDIO_3GPP},
	"audio/ogg":      {Extension: "ogg", Type: binary.MediaFormats_AUDIO_OGG},

	"text/vcard":         {Extension: "vcard", Type: binary.MediaFormats_TEXT_VCARD},
	"application/pdf":    {Extension: "pdf", Type: binary.MediaFormats_APP_PDF},
	"text/plain":         {Extension: "txt", Type: binary.MediaFormats_APP_TXT},
	"text/html":          {Extension: "html", Type: binary.MediaFormats_APP_HTML},
	"application/msword": {Extension: "doc", Type: binary.MediaFormats_APP_DOC},
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document":   {Extension: "docx", Type: binary.MediaFormats_APP_DOCX},
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": {Extension: "pptx", Type: binary.MediaFormats_APP_PPTX},
	"application/vnd.ms-powerpoint":                                             {Extension: "ppt", Type: binary.MediaFormats_APP_PPT},
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         {Extension: "xlsx", Type: binary.MediaFormats_APP_XLSX},
	"application/vnd.ms-excel":                                                  {Extension: "xls", Type: binary.MediaFormats_APP_XLS},
	"application/vnd.android.package-archive":                                   {Extension: "apk", Type: binary.MediaFormats_APP_APK},
	"application/zip":          {Extension: "zip", Type: binary.MediaFormats_APP_ZIP},
	"application/java-archive": {Extension: "jar", Type: binary.MediaFormats_APP_JAR},
	"text/x-calendar":          {Extension: "vcs", Type: binary.MediaFormats_CAL_TEXT_VCALENDAR},
	"text/calendar":            {Extension: "ics", Type: binary.MediaFormats_CAL_TEXT_CALENDAR},

	"image":       {Type: binary.MediaFormats_IMAGE_UNSPECIFIED},
	"video":       {Type: binary.MediaFormats_VIDEO_UNSPECIFIED},
	"audio":       {Type: binary.MediaFormats_AUDIO_UNSPECIFIED},
	"application": {Type: binary.MediaFormats_APP_UNSPECIFIED},
	"text":        {Type: binary.MediaFormats_APP_TXT},
}

var FormatToMediaType = map[binary.MediaFormats]MediaType{
	binary.MediaFormats_CAL_TEXT_XVCALENDAR: MimeToMediaType["text/x-calendar"],
	binary.MediaFormats_CAL_APPLICATION_VCS: MimeToMediaType["text/x-calendar"],
	binary.MediaFormats_CAL_APPLICATION_ICS: MimeToMediaType["text/calendar"],
	//binary.MediaFormats_CAL_APPLICATION_HBSVCS: ???
}

func init() {
	for key, mediaType := range MimeToMediaType {
		if strings.ContainsRune(key, '/') {
			mediaType.Format = key
		}
		FormatToMediaType[mediaType.Type] = mediaType
	}
}

func (c *Client) UploadMedia(data []byte, fileName, mime string) (*binary.MediaContent, error) {
	mediaType := MimeToMediaType[mime]
	if mediaType.Type == 0 {
		mediaType = MimeToMediaType[strings.Split(mime, "/")[0]]
	}
	decryptionKey, err := crypto.GenerateKey(32)
	if err != nil {
		return nil, err
	}
	cryptor, err := crypto.NewImageCryptor(decryptionKey)
	if err != nil {
		return nil, err
	}
	encryptedBytes, err := cryptor.EncryptData(data)
	if err != nil {
		return nil, err
	}
	startUploadImage, err := c.StartUploadMedia(encryptedBytes, mime)
	if err != nil {
		return nil, err
	}
	upload, err := c.FinalizeUploadMedia(startUploadImage)
	if err != nil {
		return nil, err
	}
	return &binary.MediaContent{
		Format:        mediaType.Type,
		MediaID:       upload.MediaID,
		MediaName:     fileName,
		Size:          int64(len(data)),
		DecryptionKey: decryptionKey,
	}, nil
}
