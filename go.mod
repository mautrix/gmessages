module go.mau.fi/mautrix-gmessages

go 1.21

require (
	github.com/gabriel-vasile/mimetype v1.4.5
	github.com/lib/pq v1.10.9
	github.com/mattn/go-sqlite3 v1.14.22
	github.com/rs/zerolog v1.33.0
	github.com/skip2/go-qrcode v0.0.0-20200617195104-da1b6568686e
	go.mau.fi/mautrix-gmessages/libgm v0.4.3
	go.mau.fi/util v0.6.1-0.20240802175451-b430ebbffc98
	golang.org/x/exp v0.0.0-20240719175910-8a7402abbf56
	google.golang.org/protobuf v1.34.2
	gopkg.in/yaml.v3 v3.0.1
	maunium.net/go/mautrix v0.19.1-0.20240808105112-b5f968d8c386
)

require (
	github.com/coreos/go-systemd/v22 v22.5.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/mux v1.8.0 // indirect
	github.com/gorilla/websocket v1.5.0 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.19 // indirect
	github.com/rogpeppe/go-internal v1.10.0 // indirect
	github.com/rs/xid v1.5.0 // indirect
	github.com/tidwall/gjson v1.17.3 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.0 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	github.com/yuin/goldmark v1.7.4 // indirect
	go.mau.fi/zeroconfig v0.1.3 // indirect
	golang.org/x/crypto v0.25.0 // indirect
	golang.org/x/net v0.27.0 // indirect
	golang.org/x/sys v0.22.0 // indirect
	golang.org/x/text v0.16.0 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	maunium.net/go/mauflag v1.0.0 // indirect
)

replace go.mau.fi/mautrix-gmessages/libgm => ./libgm
