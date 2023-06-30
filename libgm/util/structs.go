package util

import "go.mau.fi/mautrix-gmessages/libgm/binary"

type SessionResponse struct {
	Success  bool
	Settings *binary.Settings
}

type Headers struct {
	Host                    string `json:"host,omitempty" header:"host"`
	Connection              string `json:"connection,omitempty" header:"connection"`
	SecChUa                 string `json:"sec-ch-ua,omitempty" header:"sec-ch-ua"`
	SecChUaMobile           string `json:"sec-ch-ua-mobile,omitempty" header:"sec-ch-ua-mobile"`
	SecChUaPlatform         string `json:"sec-ch-ua-platform,omitempty" header:"sec-ch-ua-platform"`
	UpgradeInsecureRequests string `json:"upgrade-insecure-requests,omitempty" header:"upgrade-insecure-requests"`
	UserAgent               string `json:"user-agent,omitempty" header:"user-agent"`
	Accept                  string `json:"accept,omitempty" header:"accept"`
	Cookie                  string `json:"cookie,omitempty" header:"cookie"`
	Referer                 string `json:"referer,omitempty" header:"referer"`
	SecFetchSite            string `json:"sec-fetch-site,omitempty" header:"sec-fetch-site"`
	SecFetchMode            string `json:"sec-fetch-mode,omitempty" header:"sec-fetch-mode"`
	SecFetchUser            string `json:"sec-fetch-user,omitempty" header:"sec-fetch-user"`
	SecFetchDest            string `json:"sec-fetch-dest,omitempty" header:"sec-fetch-dest"`
	AcceptEncoding          string `json:"accept-encoding,omitempty" header:"accept-encoding"`
	AcceptLanguage          string `json:"accept-language,omitempty" header:"accept-language"`
}

func (h *Headers) Build() {
	h.Connection = "keep-alive"
	h.SecChUa = `"Google Chrome";v="113", "Chromium";v="113", "Not-A.Brand";v="24"`
	h.SecChUaMobile = "?0"
	h.SecChUaPlatform = `"Linux"`
	h.UpgradeInsecureRequests = "1"
	h.UserAgent = USER_AGENT
	h.Accept = `text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7`
	h.SecFetchSite = "none"
	h.SecFetchMode = "navigate"
	h.SecFetchUser = "?1"
	h.SecFetchDest = "document"
	h.AcceptEncoding = "gzip, deflate, br"
	h.AcceptLanguage = "en-US,en;q=0.9"
}

func (h *Headers) SetReferer(referer string) {
	h.Referer = referer
}

func (h *Headers) SetSecFetchSite(val string) {
	h.SecFetchSite = val
}

func (h *Headers) SetSecFetchUser(val string) {
	h.SecFetchUser = val
}

func (h *Headers) SetSecFetchDest(val string) {
	h.SecFetchDest = val
}

func (h *Headers) SetUpgradeInsecureRequests(val string) {
	h.UpgradeInsecureRequests = val
}

func (h *Headers) SetAccept(val string) {
	h.Accept = val
}

type RelayHeaders struct {
	Host            string `json:"host,omitempty"`
	Connection      string `json:"connection,omitempty"`
	SecChUa         string `json:"sec-ch-ua,omitempty"`
	XUserAgent      string `json:"x-user-agent,omitempty"`
	XGoogAPIKey     string `json:"x-goog-api-key,omitempty"`
	ContentType     string `json:"content-type,omitempty"`
	SecChUaMobile   string `json:"sec-ch-ua-mobile,omitempty"`
	UserAgent       string `json:"user-agent,omitempty"`
	SecChUaPlatform string `json:"sec-ch-ua-platform,omitempty"`
	Accept          string `json:"accept,omitempty"`
	Origin          string `json:"origin,omitempty"`
	XClientData     string `json:"x-client-data,omitempty"`
	SecFetchSite    string `json:"sec-fetch-site,omitempty"`
	SecFetchMode    string `json:"sec-fetch-mode,omitempty"`
	SecFetchDest    string `json:"sec-fetch-dest,omitempty"`
	Referer         string `json:"referer,omitempty"`
	AcceptEncoding  string `json:"accept-encoding,omitempty"`
	AcceptLanguage  string `json:"accept-language,omitempty"`
}

type MediaUploadHeaders struct {
	Host                           string `json:"host"`
	Connection                     string `json:"connection"`
	SecChUa                        string `json:"sec-ch-ua"`
	XGoogUploadProtocol            string `json:"x-goog-upload-protocol"`
	XGoogUploadHeaderContentLength string `json:"x-goog-upload-header-content-length"`
	SecChUaMobile                  string `json:"sec-ch-ua-mobile"`
	UserAgent                      string `json:"user-agent"`
	XGoogUploadHeaderContentType   string `json:"x-goog-upload-header-content-type"`
	ContentType                    string `json:"content-type"`
	XGoogUploadCommand             string `json:"x-goog-upload-command"`
	SecChUaPlatform                string `json:"sec-ch-ua-platform"`
	Accept                         string `json:"accept"`
	Origin                         string `json:"origin"`
	XClientData                    string `json:"x-client-data"`
	SecFetchSite                   string `json:"sec-fetch-site"`
	SecFetchMode                   string `json:"sec-fetch-mode"`
	SecFetchDest                   string `json:"sec-fetch-dest"`
	Referer                        string `json:"referer"`
	AcceptEncoding                 string `json:"accept-encoding"`
	AcceptLanguage                 string `json:"accept-language"`
}
