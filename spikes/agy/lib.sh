#!/usr/bin/env bash
# Shared helpers for spike #84 Antigravity containerization.
#
# Safety: this spike only ever creates and removes objects named whspike-agy-*.
# It never uses container rm --all, container system stop, or touches
# containers, volumes or images it did not create.

PREFIX=whspike-agy
IMG=${IMG:-docker.io/library/fedora:latest}
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RESULTS="$HERE/results"
mkdir -p "$RESULTS"

say() { printf '%s\n' "$*"; }
hdr() { printf '\n== %s\n' "$*"; }

now_ms() { python3 -c 'import time; print(int(time.time()*1000))'; }

timed() {
  local label=$1 t0 t1 rc
  shift
  t0=$(now_ms)
  "$@" >/dev/null 2>&1
  rc=$?
  t1=$(now_ms)
  say "$label: $((t1 - t0)) ms (exit $rc)"
}

cleanup() {
  local id v n
  for id in $(container ls -a --quiet 2>/dev/null | grep "^${PREFIX}-"); do
    container rm -f "$id" >/dev/null 2>&1 || true
  done
  for v in $(container volume ls --quiet 2>/dev/null | grep "^${PREFIX}-"); do
    container volume rm "$v" >/dev/null 2>&1 || true
  done
  for n in $(container network ls --quiet 2>/dev/null | grep "^${PREFIX}-"); do
    container network rm "$n" >/dev/null 2>&1 || true
  done
}

cexec() {
  local n=$1
  shift
  container exec "$n" sh -c "$*"
}
