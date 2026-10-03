#!/usr/bin/env bash
# Shared helpers for spike #174 (MCP over the control channel). Throwaway spike code.
#
# Safety (AGENTS.md, temporary containers): every container, volume and network
# carries the three workharbor.* labels and a whtmp- name. Only objects with
# that prefix AND the lane label are ever removed. Never `container rm --all`.
PREFIX=whtmp-mcp
LANE=wh-verify
PURPOSE=mcp-control-channel
IMG=${IMG:-docker.io/library/fedora:latest}
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RESULTS="${RESULTS:-$HERE/results}"
mkdir -p "$RESULTS"
LABELS=(--label workharbor.temp=true --label "workharbor.lane=$LANE" --label "workharbor.purpose=$PURPOSE")

say() { printf '%s\n' "$*"; }
hdr() { printf '\n== %s\n' "$*"; }

cleanup() {
  local id v n
  for id in $(container ls -a --quiet 2>/dev/null | grep "^${PREFIX}-"); do container rm -f "$id" >/dev/null 2>&1; done
  for v in $(container volume ls --quiet 2>/dev/null | grep "^${PREFIX}-"); do container volume rm "$v" >/dev/null 2>&1; done
  for n in $(container network ls --quiet 2>/dev/null | grep "^${PREFIX}-"); do container network rm "$n" >/dev/null 2>&1; done
}
