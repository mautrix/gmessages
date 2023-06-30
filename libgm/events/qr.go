package events

type QRCODE_UPDATED struct {
	Image  []byte
	Height int
	Width  int

	googleUrl string
}

func NewQrCodeUpdated(image []byte, height int, width int, googleUrl string) *QRCODE_UPDATED {
	return &QRCODE_UPDATED{
		Image:     image,
		Height:    height,
		Width:     width,
		googleUrl: googleUrl,
	}
}

func (q *QRCODE_UPDATED) GetGoogleUrl() string {
	return q.googleUrl
}
