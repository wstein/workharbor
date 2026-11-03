#!/bin/bash
# ct.sh <label> <workdir> <extra docker args...> -- <bash cmd>
T=$CLAUDE_JOB_DIR/tmp; . $T/lin/img.env; C=$T/lin/c
label=$1; wd=$2; shift 2; extra=(); while [ "$1" != "--" ]; do extra+=("$1"); shift; done; shif
rm -rf $C/canary; mkdir -p $C/canary
out=$(docker run --rm "${extra[@]}" -v $T/lin/bin/mise:/usr/local/bin/mise:ro -v $T/lin/data:/mise/data:ro -v $C/ws:/ws:ro -v $C/canary:/canary --tmpfs /mise/cache --tmpfs /mise/state -w $wd -e MISE_DATA_DIR=/mise/data -e MISE_CACHE_DIR=/mise/cache -e MISE_STATE_DIR=/mise/state -e HOME=/tmp/h $IMG bash -c "$*" 2>&1)
printf '%-52s canary=[%s]\n' "$label" "$(ls $C/canary | tr '\n' ' ')"; echo "$out" | head -${N:-2} | sed 's/^/      /'
