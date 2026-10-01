#!/usr/bin/env bash
set -euo pipefail
. "$(dirname "$0")/lib.sh"

trap cleanup EXIT
cleanup

NET="${PREFIX}-net"
VOL="${PREFIX}-home"
P="${PREFIX}-proxy"
A="${PREFIX}-agent"

say "Creating internal network $NET and volume $VOL..."
container network create --internal "$NET" >/dev/null 2>&1
container volume create "$VOL" >/dev/null 2>&1

say "Starting egress proxy sidecar $P..."
ALLOW="claude.ai,claude.com,platform.claude.com,auth.anthropic.com,api.anthropic.com,auth.openai.com,api.openai.com,chatgpt.com"
container run -d --name "$P" --init --network default --network "$NET" \
  -v "$BIN:/tools:ro" \
  "$IMG" /tools/whr-proxy -listen 0.0.0.0:3128 -allow "$ALLOW" >/dev/null 2>&1

sleep 1.5
PIP=$(container inspect "$P" | python3 -c 'import json,sys; ns=json.load(sys.stdin)[0]["status"]["networks"]; print([n["ipv4Address"].split("/")[0] for n in ns if n["network"]=="'$NET'"][0])')
say "Proxy IP on internal network: $PIP"

say "Starting agent container $A..."
container run -d --name "$A" --init --network "$NET" --cpus 2 --memory 2G \
  -v "$VOL:/root" \
  -v "$BIN:/tools:ro" \
  -e HTTPS_PROXY="http://$PIP:3128" -e HTTP_PROXY="http://$PIP:3128" -e NO_PROXY="localhost,127.0.0.1" \
  "$IMG" sleep 3600 >/dev/null 2>&1

say "Checking tool versions inside agent container:"
cexec "$A" "/tools/claude --version"
cexec "$A" "/tools/codex --version"

say "Testing egress proxy from guest (curl to auth.openai.com):"
cexec "$A" "curl -s -o /dev/null -w 'code=%{http_code}\n' --max-time 5 https://auth.openai.com/codex/device || true"

say "Testing egress proxy denial from guest (curl to example.com):"
cexec "$A" "curl -s -o /dev/null -w 'code=%{http_code}\n' --max-time 5 https://example.com || true"

say "Proxy logs so far:"
container logs "$P" 2>&1 | tail -10

