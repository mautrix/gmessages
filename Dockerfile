FROM golang:1-alpine3.19 AS builder

RUN apk add --no-cache git ca-certificates build-base su-exec olm-dev

COPY . /build
WORKDIR /build
RUN go build -o /usr/bin/mautrix-gmessages

FROM alpine:3.19

ENV UID=1337 \
    GID=1337

RUN apk add --no-cache ffmpeg su-exec ca-certificates olm bash jq yq curl

COPY --from=builder /usr/bin/mautrix-gmessages /usr/bin/mautrix-gmessages
COPY --from=builder /build/docker-run.sh /docker-run.sh
VOLUME /data

CMD ["/docker-run.sh"]
