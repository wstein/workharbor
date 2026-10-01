#!/usr/bin/env bash
# Tests concurrency: two environments signed in separately running concurrently behind sidecar.
set -euo pipefail
. "$(dirname "$0")/lib.sh"

trap cleanup EXIT
cleanup

NET="${PREFIX}-net"
V1="${PREFIX}-vol1"
V2="${PREFIX}-vol2"
P="${PREFIX}-proxy"
A1="${PREFIX}-env1"
A2="${PREFIX}-env2"

hdr "1. Setup internal network, sidecar proxy, and two separate environment volumes"
container network create --internal "$NET" >/dev/null 2>&1
container volume create "$V1" >/dev/null 2>&1
container volume create "$V2" >/dev/null 2>&1

ALLOW="claude.ai,claude.com,platform.claude.com,auth.anthropic.com,api.anthropic.com,auth.openai.com,api.openai.com,chatgpt.com"
container run -d --name "$P" --init --network default --network "$NET" \
  -v "$BIN:/tools:ro" \
  "$IMG" /tools/whr-proxy -listen 0.0.0.0:3128 -allow "$ALLOW" >/dev/null 2>&1

sleep 1.5
PIP=$(container inspect "$P" | python3 -c 'import json,sys; ns=json.load(sys.stdin)[0]["status"]["networks"]; print([n["ipv4Address"].split("/")[0] for n in ns if n["network"]=="'$NET'"][0])')
say "Proxy IP: $PIP"

hdr "2. Launch two concurrent agent containers: $A1 and $A2"
container run -d --name "$A1" --init --network "$NET" --cpus 2 --memory 2G \
  -v "$V1:/root" -v "$BIN:/tools:ro" \
  -e HTTPS_PROXY="http://$PIP:3128" -e HTTP_PROXY="http://$PIP:3128" -e NO_PROXY="localhost,127.0.0.1" \
  "$IMG" sleep 3600 >/dev/null 2>&1

container run -d --name "$A2" --init --network "$NET" --cpus 2 --memory 2G \
  -v "$V2:/root" -v "$BIN:/tools:ro" \
  -e HTTPS_PROXY="http://$PIP:3128" -e HTTP_PROXY="http://$PIP:3128" -e NO_PROXY="localhost,127.0.0.1" \
  "$IMG" sleep 3600 >/dev/null 2>&1

say "Env 1 IP: $(container inspect "$A1" | python3 -c 'import json,sys; print(json.load(sys.stdin)[0]["status"]["networks"][0]["ipv4Address"].split("/")[0])')"
say "Env 2 IP: $(container inspect "$A2" | python3 -c 'import json,sys; print(json.load(sys.stdin)[0]["status"]["networks"][0]["ipv4Address"].split("/")[0])')"

hdr "3. Independent credential setups on the two separate volumes"
# Environment 1 gets simulated credential session 1 with scopes
cexec "$A1" 'mkdir -p /root/.claude && cat << "JSON" > /root/.claude/.credentials.json
{
  "claudeAiOauth": {
    "accessToken": "sk-ant-env1-token-11111111111111111111",
    "refreshToken": "sk-ant-env1-refresh-1111111111111111",
    "expiresAt": 1800000000000,
    "scopes": ["user:profile", "user:inference", "user:sessions:claude_code"]
  }
}
JSON
chmod 600 /root/.claude/.credentials.json'

# Environment 2 gets simulated credential session 2 with scopes
cexec "$A2" 'mkdir -p /root/.claude && cat << "JSON" > /root/.claude/.credentials.json
{
  "claudeAiOauth": {
    "accessToken": "sk-ant-env2-token-22222222222222222222",
    "refreshToken": "sk-ant-env2-refresh-2222222222222222",
    "expiresAt": 1800000000000,
    "scopes": ["user:profile", "user:inference", "user:sessions:claude_code"]
  }
}
JSON
chmod 600 /root/.claude/.credentials.json'

say "Env 1 auth status:"
cexec "$A1" '/tools/claude auth status | grep -E "(loggedIn|authMethod)"'
say "Env 2 auth status:"
cexec "$A2" '/tools/claude auth status | grep -E "(loggedIn|authMethod)"'

hdr "4. Concurrent agent invocations in both environments simultaneously"
t0=$(now_ms)
(cexec "$A1" 'timeout 10 /tools/claude auth status' > "$RESULTS/concurrency-env1.out" 2>&1) &
pid1=$!
(cexec "$A2" 'timeout 10 /tools/claude auth status' > "$RESULTS/concurrency-env2.out" 2>&1) &
pid2=$!

wait "$pid1"
wait "$pid2"
t1=$(now_ms)
say "Concurrent execution took: $((t1 - t0)) ms"

hdr "5. Verify credential isolation across volumes"
say "Env 1 credential token: $(cexec "$A1" 'cat /root/.claude/.credentials.json | grep -o "sk-ant-env[0-9]-token-[0-9]*"')"
say "Env 2 credential token: $(cexec "$A2" 'cat /root/.claude/.credentials.json | grep -o "sk-ant-env[0-9]-token-[0-9]*"')"

say "Proxy access log during concurrent runs:"
container logs "$P" 2>&1 | tail -10

say "Concurrency test passed."
