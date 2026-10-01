#!/bin/sh
# Issue #77: make check inside the environment built from .devcontainer/devcontainer.json,
# with the Go caches on a volume (D16). Usage: run.sh <repo-dir>
set -u
REPO=$(cd "$1" && pwd)
IMAGE=whr-spike77-img
N=whr-spike77; V=$N-cache; W=
cleanup() { container image delete $IMAGE >/dev/null 2>&1; container delete --force $N >/dev/null 2>&1; container volume delete $V >/dev/null 2>&1; [ -n "$W" ] && rm -rf "$W"; }
cleanup
W=$(cd "$(mktemp -d)" && pwd -P)
trap cleanup EXIT
tar -C "$REPO" --exclude=.git -cf - . | tar -C "$W" -xf -
container volume create -s 8G $V >/dev/null
container build -t $IMAGE -f "$REPO/.devcontainer/Dockerfile" "$REPO/.devcontainer" >/dev/null 2>&1 && echo "image built from .devcontainer/Dockerfile"
container run -d --name $N -c 4 -m 6G --user agent -v "$W:/workspace" -v "$V:/cache" -w /workspace \
  -e HOME=/cache/home -e GOCACHE=/cache/go-build -e GOMODCACHE=/cache/go-mod -e GOLANGCI_LINT_CACHE=/cache/lint -e GOFLAGS=-buildvcs=false \
  "$IMAGE" sleep 3600 >/dev/null
container exec --user 0:0 $N sh -c 'mkdir -p /cache/home && chown -R agent:agent /cache'
container exec $N sh -c 'go version; git --version | head -1; make --version | head -1'
for run in 1 2; do
  s=$(date +%s)
  container exec $N make check >"$W/check$run.log" 2>&1; rc=$?
  e=$(date +%s)
  echo "make check run $run: exit $rc in $((e - s)) s"
  [ $rc -ne 0 ] && tail -15 "$W/check$run.log"
done
