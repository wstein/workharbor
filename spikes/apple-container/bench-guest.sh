#!/bin/bash
# Runs inside the guest. Times the steps of this project's `make check` one by one.
# Env: REPO (the source tree), GOMODCACHE, GOCACHE. Usage: bench-guest.sh <label>
export PATH=/opt/go/bin:$PATH GOTOOLCHAIN=local GOFLAGS=-buildvcs=false CGO_ENABLED=0
LABEL=$1
cd "$REPO" || { echo "no repo at $REPO"; exit 1; }
LINT=github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
EC=github.com/editorconfig-checker/editorconfig-checker/v3/cmd/editorconfig-checker@v3.11.3
step() {
  local n=$1 t0 t1 rc; shift
  t0=$(date +%s%3N); "$@" >/tmp/step.out 2>&1; rc=$?; t1=$(date +%s%3N)
  printf '%-6s %-13s %7d ms (exit %d)\n' "$LABEL" "$n" $((t1 - t0)) $rc
}
T0=$(date +%s%3N)
step build go build ./...
step vet go vet ./...
step test go test ./...
step lint go run $LINT run
step editorconfig go run $EC
T1=$(date +%s%3N)
printf '%-6s %-13s %7d ms\n' "$LABEL" TOTAL $((T1 - T0))
