#!/bin/bash
# usage: dk.sh <data-mount-mode:rw|ro> <state/cache mode: rw|tmpfs|ro> <cmd...>
T=$CLAUDE_JOB_DIR/tmp; . $T/lin/img.env
dm=$1; sm=$2; shift 2
args=(--rm -v $T/lin/bin/mise:/usr/local/bin/mise:ro -v $T/lin/data:/mise/data:$dm -v $T/lin/proj:/proj:rw -w /proj -e MISE_DATA_DIR=/mise/data -e MISE_CACHE_DIR=/mise/cache -e MISE_STATE_DIR=/mise/state -e HOME=/tmp/h -e MISE_YES=1)
case $sm in
 rw) args+=(-v $T/lin/cache:/mise/cache -v $T/lin/state:/mise/state);;
 tmpfs) args+=(--tmpfs /mise/cache --tmpfs /mise/state);;
 ro) args+=(-v $T/lin/cache:/mise/cache:ro -v $T/lin/state:/mise/state:ro);;
esac
docker run "${args[@]}" $IMG bash -c "$*"
