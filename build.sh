#!/bin/sh
MAUTRIX_VERSION=$(cat go.mod | grep 'maunium.net/go/mautrix ' | awk '{ print $2 }' | head -n1)
GO_LDFLAGS="-s -w -X main.Tag=$(git describe --exact-match --tags 2>/dev/null) -X main.Commit=$(git rev-parse HEAD) -X 'main.BuildTime=`date -Iseconds`' -X 'maunium.net/go/mautrix.GoModVersion=$MAUTRIX_VERSION'"

if [ "$(uname)" = "Darwin" ] && [ "$(uname -m)" = "arm64" ] && [ -z "${LIBRARY_PATH}" ] && [ -d /opt/homebrew ]; then
	echo "Using /opt/homebrew for LIBRARY_PATH and CPATH"
	export LIBRARY_PATH=/opt/homebrew/lib
	export CPATH=/opt/homebrew/include
fi

go build -ldflags="-s -w $GO_LDFLAGS" ./cmd/mautrix-gmessages "$@"
