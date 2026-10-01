#!/usr/bin/env bash
# Item 5a: install Claude Code into a volume from inside a container on the
# internal network, through the allowlist proxy, and see which hosts it needs.
set -uo pipefail
. "$(dirname "$0")/lib.sh"
HERE="$(cd "$(dirname "$0")" && pwd)"
SCRATCH=${SCRATCH:?set SCRATCH to a scratch directory on the host}
ALLOW=${ALLOW:-downloads.claude.ai,claude.ai,anthropic.com}
rm -rf "$SCRATCH/tools" && mkdir -p "$SCRATCH/tools" && cp "$HERE/bin-probe-linux" "$SCRATCH/tools/probe"
# No trap here: later scripts reuse the volume and network. Run ./cleanup-all.sh to remove everything.
NET=${PREFIX}-net; VOL=${PREFIX}-home
tools="--mount type=bind,source=$SCRATCH/tools,target=/tools,readonly"

container network create --internal $NET >/dev/null 2>&1
container volume create $VOL >/dev/null 2>&1
P=${PREFIX}-p1
container rm -f $P >/dev/null 2>&1
container run -d --name $P --init --network default --network $NET $tools $IMG /tools/probe proxy 0.0.0.0:3128 "$ALLOW" >/dev/null 2>&1
sleep 1.5
PIP=$(container inspect $P | python3 -c 'import json,sys; ns=json.load(sys.stdin)[0]["status"]["networks"]; print([n["ipv4Address"].split("/")[0] for n in ns if n["network"]=="'$NET'"][0])')
echo "$PIP" > "$SCRATCH/proxy.ip"
say "proxy sidecar internal ip: $PIP, allowlist: $ALLOW"

A=${PREFIX}-ag1
container rm -f $A >/dev/null 2>&1
container run -d --name $A --init --network $NET --cpus 2 --memory 2G -v $VOL:/root \
  -e HTTPS_PROXY=http://$PIP:3128 -e HTTP_PROXY=http://$PIP:3128 -e NO_PROXY=localhost,127.0.0.1 $IMG sleep 7200 >/dev/null 2>&1

hdr "install (curl -fsSL https://claude.ai/install.sh | bash) inside the container"
t0=$(now_ms)
cexec $A 'curl -fsSL https://claude.ai/install.sh | bash' 2>&1 | tail -15
t1=$(now_ms); say "install took $((t1 - t0)) ms"
hdr "result"
cexec $A 'ls -l /root/.local/bin/ 2>&1; /root/.local/bin/claude --version 2>&1 | head -2; du -sh /root/.local/share/claude 2>/dev/null | head -1; uname -m'
hdr "proxy log so far"
container logs $P 2>&1 | tail -20
