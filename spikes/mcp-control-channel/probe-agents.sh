#!/usr/bin/env bash
# Part C: does an agent CLI speak MCP over stdio and take exactly one server without reading repository config?
# Needs the binaries; with none given nothing is measured (the page says so). No login is needed for --help output.
# CODEX_BIN=/path/to/codex AGY_BIN=/path/to/agy ./probe-agents.sh
set -uo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
. "$HERE/lib.sh"
OUT="$RESULTS/c-agents.txt"
{
  for pair in "codex:${CODEX_BIN:-}" "agy:${AGY_BIN:-}"; do
    n=${pair%%:*} b=${pair#*:}
    hdr "$n"
    if [ -z "$b" ] || [ ! -x "$b" ]; then echo "NOT MEASURED: no binary (set ${n^^}_BIN)"; continue; fi
    "$b" --version 2>&1 | head -2
    echo "--- --help"; "$b" --help 2>&1 | head -60
    echo "--- mcp help"; "$b" mcp --help 2>&1 | head -40
    echo "--- exec help"; "$b" exec --help 2>&1 | grep -iE 'mcp|config|-c ' | head -20
  done
  hdr "next, by hand with a login (see README, Part C)"
} 2>&1 | tee "$OUT"
