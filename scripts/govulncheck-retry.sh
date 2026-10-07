#!/usr/bin/env bash
# Run govulncheck (or any command given as arguments) with a bounded retry for
# transient vulnerability database failures (#392).
#
#   scripts/govulncheck-retry.sh go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...
#
# Exit 0 passes. Exit 3 means govulncheck reported vulnerabilities and fails at
# once, never retried. Any other failure is retried only when its output shows a
# network error or an HTTP 5xx; everything else fails at once too. At most
# RETRY_ATTEMPTS (3) runs, RETRY_PAUSE_SECONDS (10) apart.
set -uo pipefail

attempts="${RETRY_ATTEMPTS:-3}"
pause="${RETRY_PAUSE_SECONDS:-10}"
transient='(status|HTTP|code)[: ]+5[0-9]{2}|timeout|timed out|connection (reset|refused)|no such host|unexpected EOF|temporary failure'

out="$(mktemp)"
trap 'rm -f "$out"' EXIT

n=1
while :; do
  "$@" >"$out" 2>&1
  rc=$?
  cat "$out"
  [ "$rc" -eq 0 ] && exit 0
  [ "$rc" -eq 3 ] && exit 3
  if [ "$n" -ge "$attempts" ] || ! grep -Eiq "$transient" "$out"; then
    exit "$rc"
  fi
  echo "::warning::transient failure (exit $rc), retrying in ${pause}s ($n/$attempts)"
  sleep "$pause"
  n=$((n + 1))
done
