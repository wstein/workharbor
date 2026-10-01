#!/usr/bin/env bash
# Item 4 (continued): the host cannot reach an --internal network, so put the
# logging allowlist proxy in a sidecar container attached to both networks.
set -uo pipefail
. "$(dirname "$0")/lib.sh"
HERE="$(cd "$(dirname "$0")" && pwd)"
SCRATCH=${SCRATCH:?set SCRATCH to a scratch directory on the host}
rm -rf "$SCRATCH/tools" && mkdir -p "$SCRATCH/tools" && cp "$HERE/bin-probe-linux" "$SCRATCH/tools/probe"
trap cleanup EXIT
NET=${PREFIX}-net
tools="--mount type=bind,source=$SCRATCH/tools,target=/tools,readonly"
nets() { container inspect "$1" | python3 -c 'import json,sys; print(" ".join("%s=%s(gw %s)" % (n["network"], n["ipv4Address"].split("/")[0], n["ipv4Gateway"]) for n in json.load(sys.stdin)[0]["status"]["networks"]))'; }

container network create --internal $NET >/dev/null 2>&1
hdr "sidecar attached to default AND internal networks (does --network repeat?)"
P=${PREFIX}-p1
container run -d --name $P --init --network default --network $NET $tools $IMG \
  /tools/probe proxy 0.0.0.0:3128 "example.com,proxy.golang.org,sum.golang.org" 2>&1 | tail -2
sleep 1.5
say "sidecar networks: $(nets $P)"
PIP=$(container inspect $P | python3 -c 'import json,sys; ns=json.load(sys.stdin)[0]["status"]["networks"]; print([n["ipv4Address"].split("/")[0] for n in ns if n["network"]=="'$NET'"][0])')
say "sidecar internal ip: $PIP"

hdr "agent container on the internal network only"
A=${PREFIX}-a1
container run -d --name $A --init --network $NET $tools $IMG sleep 900 >/dev/null 2>&1
say "agent networks: $(nets $A)"
say "direct internet (must fail)                 : $(cexec $A '/tools/probe dial-tcp 1.1.1.1:443' 2>&1 | head -1 | cut -c1-70)"
say "agent -> sidecar proxy port                 : $(cexec $A "/tools/probe dial-tcp $PIP:3128" 2>&1 | head -1 | cut -c1-70)"
say "ALLOWED example.com via proxy               : $(cexec $A "/tools/probe http-get https://example.com http://$PIP:3128" 2>&1 | head -1)"
say "ALLOWED proxy.golang.org via proxy          : $(cexec $A "/tools/probe http-get https://proxy.golang.org http://$PIP:3128" 2>&1 | head -1)"
say "DENIED  github.com via proxy                : $(cexec $A "/tools/probe http-get https://github.com http://$PIP:3128" 2>&1 | head -1)"
say "DENIED  http://example.org via proxy        : $(cexec $A "/tools/probe http-get http://example.org http://$PIP:3128" 2>&1 | head -1)"
say "curl + HTTPS_PROXY, name resolved by proxy  : $(cexec $A "HTTPS_PROXY=http://$PIP:3128 curl -s -o /dev/null -w '%{http_code}' https://example.com" 2>&1)"
say "curl + HTTPS_PROXY, denied host             : $(cexec $A "HTTPS_PROXY=http://$PIP:3128 curl -s -o /dev/null -w '%{http_code}' https://github.com" 2>&1)"
say "bypass: CONNECT to a raw IP via proxy       : $(cexec $A "HTTPS_PROXY=http://$PIP:3128 curl -s -o /dev/null -w '%{http_code}' https://1.1.1.1" 2>&1)"
say "agent -> default-network container (must fail): $(cexec $A '/tools/probe dial-tcp 192.168.64.1:22' 2>&1 | head -1 | cut -c1-70)"
say "agent -> host gateway of default net (must fail): $(cexec $A '/tools/probe dial-tcp 192.168.64.1:18082' 2>&1 | head -1 | cut -c1-70)"

hdr "second agent on the internal network: can agents reach each other?"
B=${PREFIX}-a2
container run -d --name $B --init --network $NET $tools $IMG /tools/probe listen-tcp 0.0.0.0:18083 >/dev/null 2>&1
sleep 1
BIP=$(container inspect $B | python3 -c 'import json,sys; print(json.load(sys.stdin)[0]["status"]["networks"][0]["ipv4Address"].split("/")[0])')
say "agent1 -> agent2 $BIP:18083 : $(cexec $A "/tools/probe dial-tcp $BIP:18083" 2>&1 | head -1 | cut -c1-70)"

hdr "proxy log (sidecar)"
container logs $P 2>&1 | tail -20
