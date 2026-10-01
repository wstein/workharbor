#!/usr/bin/env bash
# Runs the full spike #84 Antigravity containerization measurement suite in Apple Container.
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
. "$HERE/lib.sh"

TOOLS_DIR="${TOOLS_DIR:-$HERE/bin}"

if [ ! -f "$TOOLS_DIR/antigravity" ] || [ ! -f "$TOOLS_DIR/whr-shim" ] || [ ! -f "$TOOLS_DIR/whr-proxy" ]; then
  echo "Building tools first..."
  TOOLS_DIR="$TOOLS_DIR" "$HERE/build-tools.sh"
fi

ALLOW=${ALLOW:-google.com,googleapis.com,googleusercontent.com}
NET=${PREFIX}-net
HOMEV=${PREFIX}-ahome
WORKV=${PREFIX}-awork
P=${PREFIX}-p1
A=${PREFIX}-ag1

finish() {
  say "Cleaning up container resources..."
  cleanup
}
trap finish EXIT

hdr "1. Setup container network, proxy sidecar, and agent container"
cleanup

container network create --internal "$NET" >/dev/null 2>&1
container volume create "$HOMEV" >/dev/null 2>&1
container volume create "$WORKV" >/dev/null 2>&1

container run -d --name "$P" --init --network default --network "$NET" \
  --mount type=bind,source="$TOOLS_DIR",target=/tools,readonly "$IMG" \
  /tools/whr-proxy -listen 0.0.0.0:3128 -allow "$ALLOW" >/dev/null 2>&1
sleep 1.5

PIP=$(container inspect "$P" | python3 -c 'import json,sys; ns=json.load(sys.stdin)[0]["status"]["networks"]; print([n["ipv4Address"].split("/")[0] for n in ns if n["network"]=="'$NET'"][0])')
say "Proxy sidecar $P running (internal IP: $PIP, allowing: $ALLOW)"

container run -d --name "$A" --init --network "$NET" --cpus 2 --memory 2G \
  --mount type=bind,source="$TOOLS_DIR",target=/tools,readonly -v "$HOMEV:/root" -v "$WORKV:/work" \
  -e HTTPS_PROXY="http://$PIP:3128" -e HTTP_PROXY="http://$PIP:3128" -e NO_PROXY="localhost,127.0.0.1" -e https_proxy="http://$PIP:3128" -e http_proxy="http://$PIP:3128" -e no_proxy="localhost,127.0.0.1" \
  "$IMG" sleep 7200 >/dev/null 2>&1

say "Agent container $A running: $(container ls | awk -v c=$A '$1==c{print $5}')"

hdr "2. Verifying egress control and agent tools"
say "Agent tool versions:"
container exec "$A" /bin/sh -c 'echo "Image: $(cat /etc/os-release | grep PRETTY_NAME | cut -d= -f2) | Antigravity: $(/tools/antigravity --version) | Shim: $(/tools/whr-shim 2>&1 | head -1)"'

AGENT_CONTAINER="$A" python3 "$HERE/driver.py" check-prereqs

hdr "3. Running Spike Driver Suite (Cases 1-7)"
AGENT_CONTAINER="$A" TOOLS_DIR="/tools" RESULTS_DIR="$RESULTS" python3 "$HERE/driver.py" all

hdr "4. Egress audit from proxy logs"
container logs "$P" 2>&1 | grep -E 'ALLOW|DENY' | awk '{print $2, $3, $4}' | sort | uniq -c | sed -E 's/^ +/  /'

say "\nAll tests finished successfully. Results saved in $RESULTS."
