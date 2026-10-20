#!/bin/sh
# Runs go generate + go test inside the same Alpine toolchain the Dockerfile
# builds with, plus the preview tools. There is no Go on the mini itself.
#
# Several worktrees may call this at once, and the Colima VM has 4 GiB: one
# Go test container at a time. A lock directory (mkdir is atomic) serialises
# them across processes and holds the owner's pid. A lock whose owner is no
# longer alive is stale; so is one that never got a pid within 20 minutes
# (its owner died between mkdir and writing it). Breaking a stale lock is
# itself serialised by a second lock, and re-checked under it, so two
# waiters can never both break it and both run.
set -eu
cd "$(dirname "$0")/../.."
export DOCKER_HOST="${DOCKER_HOST:-unix:///Users/franco/.colima/default/docker.sock}"
if [ "$#" -eq 0 ]; then set -- ./...; fi

LOCK=/tmp/gokapi-gotest.lock
BREAK=/tmp/gokapi-gotest.lock.break

lock_is_stale() {
  owner=$(cat "$LOCK/pid" 2>/dev/null || true)
  if [ -n "$owner" ]; then
    # kill -0 fails with EPERM for another user's live process; ps does not
    ! { kill -0 "$owner" 2>/dev/null || ps -p "$owner" >/dev/null 2>&1; }
  else
    [ -n "$(find "$LOCK" -maxdepth 0 -mmin +20 2>/dev/null)" ]
  fi
}

break_stale_lock() {
  # A breaker killed mid-break leaves BREAK behind; a minute is plenty
  if [ -n "$(find "$BREAK" -maxdepth 0 -mmin +1 2>/dev/null)" ]; then rm -rf "$BREAK"; fi
  mkdir "$BREAK" 2>/dev/null || return 0
  if [ -d "$LOCK" ] && lock_is_stale; then
    echo "gotest: removing stale lock $LOCK (owner: ${owner:-none})" >&2
    rm -rf "$LOCK"
  fi
  rmdir "$BREAK" 2>/dev/null || true
}

waited=0
until mkdir "$LOCK" 2>/dev/null; do
  if lock_is_stale; then
    break_stale_lock
    continue
  fi
  if [ "$waited" -eq 0 ]; then echo "gotest: waiting for another test run to finish ($LOCK)" >&2; fi
  waited=1
  sleep 5
done
echo $$ > "$LOCK/pid"
# Only ever remove a lock that is still ours. Not exec: the shell must
# outlive docker to do it.
release() {
  if [ "$(cat "$LOCK/pid" 2>/dev/null || true)" = "$$" ]; then rm -rf "$LOCK"; fi
}
trap release EXIT
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
