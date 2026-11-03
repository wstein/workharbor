#!/bin/bash
. "$CLAUDE_JOB_DIR/tmp/env.sh"
T=$CLAUDE_JOB_DIR/tmp; S=$T/scripts/trustcase.sh; H=$T/lab/ws/repos/hostile
export LINES_SHOWN=1
label=$1; shif
for c in "env" "ls" "tasks" "run pwn" "exec -- true" "install" "hook-env -s bash" "x -- true" "pwn"; do $S "$label: mise $c" $H "$@" -- $c; done 2>&1 | grep -v '^      mise ERROR error parsing'
