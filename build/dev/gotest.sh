#!/bin/sh
# Runs go generate + go test inside the same Alpine toolchain the Dockerfile
# builds with, plus the preview tools. There is no Go on the mini itself.
set -eu
cd "$(dirname "$0")/../.."
export DOCKER_HOST="${DOCKER_HOST:-unix:///Users/franco/.colima/default/docker.sock}"
if [ "$#" -eq 0 ]; then set -- ./...; fi
exec docker run --rm \
  -v "$PWD":/src -w /src \
  -v gokapi-gomod:/go/pkg/mod -v gokapi-gocache:/root/.cache/go-build \
  golang:1.26.2-alpine sh -c '
    apk add -q --no-cache git vips-tools vips-heif ffmpeg exiftool >/dev/null &&
    go generate ./... &&
    go test -tags=test,awsmock -count=1 -parallel 8 "$@"' sh "$@"
