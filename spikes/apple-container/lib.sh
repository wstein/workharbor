#!/usr/bin/env bash
# Shared helpers for the Apple Container spike (issue #2). Throwaway, not shipping code.
#
# Safety: this spike only ever creates and removes objects named whspike-*.
# It never uses `container rm --all`, `container system stop`, or touches
# containers, volumes or images it did not create.

PREFIX=whspike
IMG=${IMG:-docker.io/library/fedora:latest}
RESULTS="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/results"

say() { printf '%s\n' "$*"; }
hdr() { printf '\n== %s\n' "$*"; }

# now_ms prints a millisecond clock for timing container operations.
now_ms() { python3 -c 'import time; print(int(time.time()*1000))'; }
timed() { # timed <label> <cmd...>: run a command and report its wall time
  local label=$1 t0 t1 rc; shift
  t0=$(now_ms); "$@" >/dev/null 2>&1; rc=$?; t1=$(now_ms)
  say "$label: $((t1 - t0)) ms (exit $rc)"
}

# Remove only objects this spike created.
cleanup() {
  local id
  for id in $(container ls -a --quiet 2>/dev/null | grep "^${PREFIX}-"); do
    container rm -f "$id" >/dev/null 2>&1
  done
  for v in $(container volume ls --quiet 2>/dev/null | grep "^${PREFIX}-"); do
    container volume rm "$v" >/dev/null 2>&1
  done
  for n in $(container network ls --quiet 2>/dev/null | grep "^${PREFIX}-"); do
    container network rm "$n" >/dev/null 2>&1
  done
}

# cexec <name> <shell command>: run a command in a spike container.
cexec() { local n=$1; shift; container exec "$n" sh -c "$*"; }
