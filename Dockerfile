FROM golang:1-alpine3.21 AS builder

RUN apk add --no-cache git ca-certificates build-base su-exec olm-dev

COPY . /build
WORKDIR /build
RUN cd cmd/mautrix-gmessages && go build -o /usr/bin/mautrix-gmessages .

FROM alpine:3.21

ENV UID=1337 \
    GID=1337

RUN apk add --no-cache ffmpeg su-exec ca-certificates olm bash jq yq-go curl

COPY --from=builder /usr/bin/mautrix-gmessages /usr/bin/mautrix-gmessages
COPY --from=builder /build/docker-run.sh /docker-run.sh
VOLUME /data

CMD ["/docker-run.sh"]
