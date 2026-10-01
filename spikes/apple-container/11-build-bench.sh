#!/usr/bin/env bash
# Item 10: a real project build on a volume versus a bind mount.
# The workload is this repository's `make check` steps (build, vet, test, and
# golangci-lint and editorconfig-checker compiled from source). Layouts:
#   A  checkout and Go caches on one volume
#   B  checkout on a bind mount, Go caches on a volume
#   C  checkout and Go caches on a bind mount
# Usage: LAYOUT=A SCRATCH=dir ./11-build-bench.sh
set -uo pipefail
. "$(dirname "$0")/lib.sh"
HERE="$(cd "$(dirname "$0")" && pwd)"
SCRATCH=${SCRATCH:?set SCRATCH to a scratch directory}
LAYOUT=${LAYOUT:?set LAYOUT to A, B or C}
REPO_ROOT=${REPO_ROOT:-$(cd "$HERE/../.." && git rev-parse --show-toplevel)}
GOVER=${GOVER:-1.27.1}
trap cleanup EXIT
mkdir -p "$SCRATCH" && cd "$SCRATCH"

# Go toolchain, mounted read-only like any other tool (design 5.6)
if [ ! -x gotool/go/bin/go ]; then
  mkdir -p gotool && curl -fsSL "https://go.dev/dl/go${GOVER}.linux-arm64.tar.gz" | tar -xz -C gotool
fi
mkdir -p tools && cp "$HERE/bench-guest.sh" tools/bench.sh && chmod 755 tools/bench.sh
# a clean copy of the project at HEAD
rm -rf src && mkdir src && git -C "$REPO_ROOT" archive HEAD | tar -x -C src
say "layout $LAYOUT | project files: $(find src -type f | wc -l) | go $GOVER | resources: 4 CPUs, 4 GiB"

N=${PREFIX}-b$LAYOUT
common="--cpus 4 --memory 4G --mount type=bind,source=$SCRATCH/gotool/go,target=/opt/go,readonly --mount type=bind,source=$SCRATCH/tools,target=/tools,readonly"
case $LAYOUT in
  A)
    V=${PREFIX}-bva; container volume create $V >/dev/null 2>&1
    container run -d --name $N --init $common --mount type=bind,source="$SCRATCH/src",target=/src,readonly -v $V:/data $IMG sleep 7200 >/dev/null 2>&1
    t0=$(now_ms); container exec $N sh -c 'mkdir -p /data/repo /data/gomod /data/gocache && cp -a /src/. /data/repo/' >/dev/null 2>&1; t1=$(now_ms)
    say "copy the checkout into the volume: $((t1-t0)) ms"
    ENVS="-e REPO=/data/repo -e GOMODCACHE=/data/gomod -e GOCACHE=/data/gocache"; EDIT=/data/repo
    ;;
  B)
    V=${PREFIX}-bvb; container volume create $V >/dev/null 2>&1
    rm -rf "$SCRATCH/srcB" && cp -a src srcB
    container run -d --name $N --init $common --mount type=bind,source="$SCRATCH/srcB",target=/repo -v $V:/data $IMG sleep 7200 >/dev/null 2>&1
    container exec $N sh -c 'mkdir -p /data/gomod /data/gocache' >/dev/null 2>&1
    ENVS="-e REPO=/repo -e GOMODCACHE=/data/gomod -e GOCACHE=/data/gocache"; EDIT=/repo
    ;;
  C)
    rm -rf "$SCRATCH/dirC" && mkdir -p "$SCRATCH/dirC/gomod" "$SCRATCH/dirC/gocache" && cp -a src "$SCRATCH/dirC/repo"
    container run -d --name $N --init $common --mount type=bind,source="$SCRATCH/dirC",target=/data $IMG sleep 7200 >/dev/null 2>&1
    ENVS="-e REPO=/data/repo -e GOMODCACHE=/data/gomod -e GOCACHE=/data/gocache"; EDIT=/data/repo
    ;;
esac
say "container: $(container ls | awk -v c=$N '$1==c{print $5, $7" cpus", $8$9}')"

run() { container exec $ENVS $N /tools/bench.sh "$1" 2>&1; }
hdr "cold: empty module and build caches (includes downloading modules from the network)"
run cold
hdr "warm: same tree, caches full"
run warm
hdr "incremental: one source file changed"
container exec $N sh -c "echo '// bench' >> $EDIT/internal/version/version.go" >/dev/null 2>&1
run incr
hdr "cache sizes"
container exec $N sh -c 'du -sh /data/gomod /data/gocache 2>/dev/null; echo "gomod files: $(find /data/gomod -type f | wc -l)  gocache files: $(find /data/gocache -type f | wc -l)"' 2>&1
container stats --no-stream $N 2>&1 | awk 'NR<=2'
