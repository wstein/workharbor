#!/bin/bash
T=$CLAUDE_JOB_DIR/tmp; . $T/lin/img.env; B=$T/bench; P="python3 -I $T/scripts/t.py"
TOOLS='jq --version; rg --version; fd --version; bat --version; fzf --version; gh --version; starship --version; zoxide --version'
MISEBASE=(-v $T/lin/bin/mise:/usr/local/bin/mise:ro -v $B/ws:/ws:ro -w /ws -e MISE_DATA_DIR=/mise/data -e MISE_CACHE_DIR=/mise/cache -e MISE_STATE_DIR=/mise/state -e HOME=/tmp/h -e MISE_YES=1)
VOL=bundledock-toolbox-d3a83465d5223e7d7e2e870b48bbd996dba37cbe92d7230bdcf3ef22f46f5f75
echo "E baseline: docker run true"; for i in 1 2 3; do $P docker run --rm $IMG true; done
echo "A mise COLD (empty data+cache+state; install --locked then run 8 tools)"
for i in 1 2 3; do rm -rf $B/d $B/c $B/s; mkdir -p $B/d $B/c $B/s
  $P docker run --rm "${MISEBASE[@]}" -v $B/d:/mise/data -v $B/c:/mise/cache -v $B/s:/mise/state $IMG bash -c "mise install --locked >/dev/null 2>&1 && mise exec -- bash -c '$TOOLS'"; done
echo "A2 mise COLD-DOWNLOAD-CACHED (empty data, cache kept)"
for i in 1 2 3; do rm -rf $B/d $B/s; mkdir -p $B/d $B/s
  $P docker run --rm "${MISEBASE[@]}" -v $B/d:/mise/data -v $B/c:/mise/cache -v $B/s:/mise/state $IMG bash -c "mise install --locked >/dev/null 2>&1 && mise exec -- bash -c '$TOOLS'"; done
echo "B mise WARM (data ro, cache/state tmpfs): mise exec -- 8 tools"
for i in 1 2 3; do $P docker run --rm "${MISEBASE[@]}" -v $B/d:/mise/data:ro --tmpfs /mise/cache --tmpfs /mise/state $IMG bash -c "mise exec -- bash -c '$TOOLS'"; done
echo "B2 mise WARM via shims on PATH (data ro)"
for i in 1 2 3; do $P docker run --rm "${MISEBASE[@]}" -v $B/d:/mise/data:ro --tmpfs /mise/cache --tmpfs /mise/state -e PATH=/mise/data/shims:/usr/local/bin:/usr/bin:/bin $IMG bash -c "$TOOLS"; done
echo "B3 mise WARM, PATH precomputed from installs (no mise process at all)"
PATHS=$(docker run --rm "${MISEBASE[@]}" -v $B/d:/mise/data:ro --tmpfs /mise/cache --tmpfs /mise/state $IMG mise bin-paths | tr '\n' ':');
for i in 1 2 3; do $P docker run --rm -v $B/d:/mise/data:ro -e PATH=${PATHS}/usr/bin:/bin $IMG bash -c "$TOOLS"; done
echo "C BundleDock COLD (new volume + init-volume from local carrier image; pull not included)"
for i in 1 2 3; do docker volume rm bd-bench >/dev/null 2>&1; docker volume create bd-bench >/dev/null
  $P docker run --rm -v bd-bench:/bundle quay.io/bundledock/toolbox:latest; $P docker run --rm -v bd-bench:/opt/bdock/toolbox:ro -e PATH=/opt/bdock/toolbox/bin:/usr/bin:/bin $IMG bash -c "$TOOLS"; done; docker volume rm bd-bench >/dev/null
echo "D BundleDock WARM (existing volume ro): 8 tools"
for i in 1 2 3; do $P docker run --rm -v $VOL:/opt/bdock/toolbox:ro -e PATH=/opt/bdock/toolbox/bin:/usr/bin:/bin $IMG bash -c "$TOOLS"; done
du -sh $B/d
