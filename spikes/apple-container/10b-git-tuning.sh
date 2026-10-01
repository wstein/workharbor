#!/usr/bin/env bash
# Item 9 (continued): does git tuning change the cost of git status on a bind-mounted clone?
# Needs the scratch directory of 10-git-worktree.sh (its clone3 and cache.git).
set -uo pipefail
. "$(dirname "$0")/lib.sh"
SCRATCH=${SCRATCH:?set SCRATCH to the directory used by 10-git-worktree.sh}
G="$SCRATCH/g"; GITIMG=${GITIMG:-docker.io/alpine/git:latest}; N=${PREFIX}-gt
trap cleanup EXIT
container run -d --name $N --init --entrypoint sleep \
  --mount type=bind,source="$G/clone3",target=/bindclone \
  --mount type=bind,source="$G/cache.git/objects",target="$G/cache.git/objects",readonly $GITIMG 3600 >/dev/null 2>&1
edits() { container exec $N sh -c "cd /bindclone && git reset -q --hard && for i in \$(seq 1 300); do echo more >> d\$((i % 50))/f\$((i % 100)).txt; done"; }
time_status() { local t0 t1 i out=""; for i in 1 2 3; do t0=$(now_ms); container exec $N sh -c "cd /bindclone && git status --short | wc -l" >/dev/null 2>&1; t1=$(now_ms); out="$out $((t1-t0))"; done; say "$1: status runs (ms):$out"; }
container exec $N sh -c 'cd /bindclone && git config --unset core.untrackedCache; git config --unset feature.manyFiles; git config --unset core.fsmonitor; true' >/dev/null 2>&1
edits; time_status "default"
container exec $N sh -c 'cd /bindclone && git config core.untrackedCache true'
edits; time_status "core.untrackedCache=true"
container exec $N sh -c 'cd /bindclone && git config feature.manyFiles true && git update-index --index-version 4 2>/dev/null; git status >/dev/null'
edits; time_status "feature.manyFiles=true"
container exec $N sh -c 'cd /bindclone && git config core.preloadIndex true; git config core.checkStat minimal'
edits; time_status "plus preloadIndex and checkStat=minimal"
