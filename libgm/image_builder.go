package libgm

import (
	"go.mau.fi/mautrix-gmessages/libgm/crypto"
	"go.mau.fi/mautrix-gmessages/libgm/util"
)

type ImageType struct {
	Extension string
	Format    string
	Type      int64
}

var ImageTypes = map[string]ImageType{
	"image/jpeg":         {Extension: "jpeg", Format: "image/jpeg", Type: 1},
	"image/jpg":          {Extension: "jpg", Format: "image/jpg", Type: 2},
	"image/png":          {Extension: "png", Format: "image/png", Type: 3},
	"image/gif":          {Extension: "gif", Format: "image/gif", Type: 4},
	"image/wbmp":         {Extension: "wbmp", Format: "image/wbmp", Type: 5},
	"image/bmp":          {Extension: "bmp", Format: "image/bmp", Type: 6},
	"image/x-ms-bmp":     {Extension: "bmp", Format: "image/x-ms-bmp", Type: 6},
	"audio/aac":          {Extension: "aac", Format: "audio/aac", Type: 14},
	"audio/amr":          {Extension: "amr", Format: "audio/amr", Type: 15},
	"audio/mp3":          {Extension: "mp3", Format: "audio/mp3", Type: 16},
	"audio/mpeg":         {Extension: "mpeg", Format: "audio/mpeg", Type: 17},
	"audio/mpg":          {Extension: "mpg", Format: "audio/mpg", Type: 18},
	"audio/mp4":          {Extension: "mp4", Format: "audio/mp4", Type: 19},
	"audio/mp4-latm":     {Extension: "latm", Format: "audio/mp4-latm", Type: 20},
	"audio/3gpp":         {Extension: "3gpp", Format: "audio/3gpp", Type: 21},
	"audio/ogg":          {Extension: "ogg", Format: "audio/ogg", Type: 22},
	"video/mp4":          {Extension: "mp4", Format: "video/mp4", Type: 8},
	"video/3gpp2":        {Extension: "3gpp2", Format: "video/3gpp2", Type: 9},
	"video/3gpp":         {Extension: "3gpp", Format: "video/3gpp", Type: 10},
	"video/webm":         {Extension: "webm", Format: "video/webm", Type: 11},
	"video/x-matroska":   {Extension: "mkv", Format: "video/x-matroska", Type: 12},
	"application/pdf":    {Extension: "pdf", Format: "application/pdf", Type: 25},
	"application/txt":    {Extension: "txt", Format: "application/txt", Type: 26},
	"application/html":   {Extension: "html", Format: "application/html", Type: 27},
	"application/msword": {Extension: "doc", Format: "application/msword", Type: 28},
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document":   {Extension: "docx", Format: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", Type: 29},
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": {Extension: "pptx", Format: "application/vnd.openxmlformats-officedocument.presentationml.presentation", Type: 30},
	"application/vnd.ms-powerpoint":                                             {Extension: "ppt", Format: "application/vnd.ms-powerpoint", Type: 31},
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         {Extension: "xlsx", Format: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", Type: 32},
	"application/vnd.ms-excel":                                                  {Extension: "xls", Format: "application/vnd.ms-excel", Type: 33},
	"application/vnd.android.package-archive":                                   {Extension: "apk", Format: "application/vnd.android.package-archive", Type: 34},
	"application/zip":          {Extension: "zip", Format: "application/zip", Type: 35},
	"application/java-archive": {Extension: "jar", Format: "application/java-archive", Type: 36},
	"text/x-vCalendar":         {Extension: "vcs", Format: "text/x-vCalendar", Type: 38},
	"text/x-vcalendar":         {Extension: "ics", Format: "text/x-vcalendar", Type: 39},
	"text/calendar":            {Extension: "ics", Format: "text/calendar", Type: 40},
	"application/vcs":          {Extension: "vcs", Format: "application/vcs", Type: 41},
	"application/ics":          {Extension: "ics", Format: "application/ics", Type: 42},
	"application/hbs-vcs":      {Extension: "vcs", Format: "application/hbs-vcs", Type: 43},
	"text/vcard":               {Extension: "vcard", Format: "text/vcard", Type: 24},
	"text/x-vcard":             {Extension: "vcard", Format: "text/x-vcard", Type: 24},
}

type Image struct {
	imageCryptor *crypto.ImageCryptor

	imageName  string
	imageID    string
	imageType  ImageType
	imageBytes []byte
	imageSize  int64
}

func (i *Image) GetEncryptedBytes() ([]byte, error) {
	encryptedBytes, encryptErr := i.imageCryptor.EncryptData(i.imageBytes)
	if encryptErr != nil {
		return nil, encryptErr
	}
	return encryptedBytes, nil
}

func (i *Image) GetImageCryptor() *crypto.ImageCryptor {
	return i.imageCryptor
}

func (i *Image) GetImageName() string {
	return i.imageName
}

func (i *Image) GetImageBytes() []byte {
	return i.imageBytes
}

func (i *Image) GetImageSize() int64 {
	return i.imageSize
}

func (i *Image) GetImageType() ImageType {
	return i.imageType
}

func (i *Image) GetImageID() string {
	return i.imageID
}

// This is the equivalent of dragging an image into the window on messages web
//
// Keep in mind that adding an image to a MessageBuilder will also upload the image to googles server
func (mb *MessageBuilder) AddImage(imgBytes []byte, mime string) *MessageBuilder {
	if mb.err != nil {
		return mb
	}

	newImage, newImageErr := mb.newImageData(imgBytes, mime)
	if newImageErr != nil {
		mb.err = newImageErr
		return mb
	}

	startUploadImage, failedUpload := mb.client.StartUploadMedia(newImage)
	if failedUpload != nil {
		mb.err = failedUpload
		return mb
	}

	finalizedImage, failedFinalize := mb.client.FinalizeUploadMedia(startUploadImage)
	if failedFinalize != nil {
		mb.err = failedFinalize
		return mb
	}

	mb.images = append(mb.images, finalizedImage)
	return mb
}

func (mb *MessageBuilder) newImageData(imgBytes []byte, mime string) (*Image, error) {
	// TODO explode on unsupported types
	imgType := ImageTypes[mime]
	imageId := util.GenerateImageID()
	imageName := util.RandStr(8) + "." + imgType.Extension
	decryptionKey, err := crypto.GenerateKey(32)
	if err != nil {
		return nil, err
	}
	imageCryptor, cryptorErr := crypto.NewImageCryptor(decryptionKey)
	if cryptorErr != nil {
		return nil, cryptorErr
	}
	return &Image{
		imageCryptor: imageCryptor,
		imageID:      imageId,
		imageBytes:   imgBytes,
		imageType:    imgType,
		imageSize:    int64(len(imgBytes)),
		imageName:    imageName,
	}, nil
}
