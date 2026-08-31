#!/bin/sh
cd $(dirname $0)
protoc --go_out=. --go_opt=paths=source_relative *.proto
goimports -w *.go
