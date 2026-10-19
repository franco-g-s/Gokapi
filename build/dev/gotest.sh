#!/bin/sh
# Runs go generate + go test inside the same Alpine toolchain the Dockerfile
# builds with, plus the preview tools. There is no Go on the mini itself.
#
# Several worktrees may call this at once, and the Colima VM has 4 GiB: one
# Go test container at a time. A lock directory serialises them across
# processes; a lock older than 20 minutes is taken to be left over from a
# crashed run and removed.
set -eu
cd "$(dirname "$0")/../.."
export DOCKER_HOST="${DOCKER_HOST:-unix:///Users/franco/.colima/default/docker.sock}"
if [ "$#" -eq 0 ]; then set -- ./...; fi

LOCK=/tmp/gokapi-gotest.lock
waited=0
until mkdir "$LOCK" 2>/dev/null; do
  if [ -n "$(find "$LOCK" -maxdepth 0 -mmin +20 2>/dev/null)" ]; then
    echo "gotest: removing stale lock $LOCK" >&2
    rmdir "$LOCK" 2>/dev/null || rm -rf "$LOCK"
    continue
  fi
  if [ "$waited" -eq 0 ]; then echo "gotest: waiting for another test run to finish ($LOCK)" >&2; fi
  waited=1
  sleep 5
done
# Not exec: the shell must outlive docker to remove the lock
trap 'rmdir "$LOCK" 2>/dev/null || true' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'exit 129' HUP

status=0
docker run --rm \
  -v "$PWD":/src -w /src \
  -v gokapi-gomod:/go/pkg/mod -v gokapi-gocache:/root/.cache/go-build \
  golang:1.26.2-alpine sh -c '
    apk add -q --no-cache git vips-tools vips-heif ffmpeg exiftool >/dev/null &&
    go generate ./... &&
    go test -tags=test,awsmock -count=1 -parallel 8 "$@"' sh "$@" || status=$?
exit "$status"
