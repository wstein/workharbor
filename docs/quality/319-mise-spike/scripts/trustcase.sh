#!/bin/bash
# usage: trustcase.sh <label> <dir> <env assignments...> -- <mise args...>
. "$CLAUDE_JOB_DIR/tmp/env.sh"
L=$CLAUDE_JOB_DIR/tmp/lab
label=$1; dir=$2; shift 2
envs=(); while [ "$1" != "--" ]; do envs+=("$1"); shift; done; shif
rm -rf "$L/canary"; mkdir -p "$L/canary"
export MISE_STATE_DIR=$CLAUDE_JOB_DIR/tmp/state-$$; mkdir -p $MISE_STATE_DIR
cd "$dir"
out=$(env "${envs[@]}" mise "$@" 2>&1 </dev/null); rc=$?
printf '%-46s rc=%-3s canary=[%s]\n' "$label" "$rc" "$(ls $L/canary | tr '\n' ' ')"
echo "$out" | head -${LINES_SHOWN:-2} | sed 's/^/      /'
rm -rf $MISE_STATE_DIR
