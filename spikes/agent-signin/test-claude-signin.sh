#!/usr/bin/env bash
# Tests Claude Code 2.1.285 sign-in flows in a hardened environment.
set -euo pipefail
. "$(dirname "$0")/lib.sh"

trap cleanup EXIT
cleanup

NET="${PREFIX}-net"
VOL="${PREFIX}-home"
P="${PREFIX}-proxy"
A="${PREFIX}-agent"

hdr "1. Setup hardened container environment with egress proxy sidecar"
container network create --internal "$NET" >/dev/null 2>&1
container volume create "$VOL" >/dev/null 2>&1

ALLOW="claude.ai,claude.com,platform.claude.com,auth.anthropic.com,api.anthropic.com,statsig.anthropic.com,downloads.claude.ai"
container run -d --name "$P" --init --network default --network "$NET" \
  -v "$BIN:/tools:ro" \
  "$IMG" /tools/whr-proxy -listen 0.0.0.0:3128 -allow "$ALLOW" >/dev/null 2>&1

sleep 1.5
PIP=$(container inspect "$P" | python3 -c 'import json,sys; ns=json.load(sys.stdin)[0]["status"]["networks"]; print([n["ipv4Address"].split("/")[0] for n in ns if n["network"]=="'$NET'"][0])')
say "Proxy sidecar IP: $PIP"

container run -d --name "$A" --init --network "$NET" --cpus 2 --memory 2G \
  -v "$VOL:/root" \
  -v "$BIN:/tools:ro" \
  -e HTTPS_PROXY="http://$PIP:3128" -e HTTP_PROXY="http://$PIP:3128" -e NO_PROXY="localhost,127.0.0.1" \
  "$IMG" sleep 3600 >/dev/null 2>&1

hdr "2. Test Claude Code sign-in flow: 'claude auth login'"
say "Executing 'claude auth login' with empty input to capture URL and prompt:"
cexec "$A" 'echo "" | timeout 6 /tools/claude auth login 2>&1 || true' > "$RESULTS/claude-auth-login.out"
cat "$RESULTS/claude-auth-login.out"

hdr "3. Test Claude Code sign-in flow: 'claude setup-token'"
say "Executing 'claude setup-token' with empty input to see behavior inside container:"
cexec "$A" 'echo "" | timeout 6 /tools/claude setup-token 2>&1 || true' > "$RESULTS/claude-setup-token.out"
cat "$RESULTS/claude-setup-token.out"

hdr "4. Proxy access logs during Claude login attempt:"
container logs "$P" 2>&1 | tail -20

hdr "5. Verify credential storage location when simulated token is written to ~/.claude/.credentials.json"
say "Creating test session on agent-home volume:"
cexec "$A" 'mkdir -p /root/.claude && chmod 700 /root/.claude'
cexec "$A" 'cat << "JSON" > /root/.claude/.credentials.json
{
  "claudeAiOauth": {
    "accessToken": "sk-ant-test-token-00000000000000000000",
    "refreshToken": "sk-ant-refresh-0000000000000000000",
    "expiresAt": 1800000000000,
    "scopes": ["user:profile", "user:inference", "user:sessions:claude_code"]
  }
}
JSON
chmod 600 /root/.claude/.credentials.json'

say "Checking permissions on .credentials.json:"
cexec "$A" 'ls -la /root/.claude/.credentials.json'

say "Testing 'claude auth status':"
cexec "$A" '/tools/claude auth status 2>&1 || true' > "$RESULTS/claude-auth-status.out"
cat "$RESULTS/claude-auth-status.out"

hdr "6. Stop, start, and rebuild test on the same agent-home volume"
say "Stopping container $A..."
container stop "$A" >/dev/null 2>&1
say "Starting container $A..."
container start "$A" >/dev/null 2>&1
say "Checking credential file after stop & start:"
cexec "$A" 'cat /root/.claude/.credentials.json | grep -o "sk-ant-[a-z0-9-]*"'

say "Deleting container $A and recreating with SAME volume $VOL..."
container rm -f "$A" >/dev/null 2>&1
container run -d --name "$A" --init --network "$NET" --cpus 2 --memory 2G \
  -v "$VOL:/root" \
  -v "$BIN:/tools:ro" \
  -e HTTPS_PROXY="http://$PIP:3128" -e HTTP_PROXY="http://$PIP:3128" -e NO_PROXY="localhost,127.0.0.1" \
  "$IMG" sleep 3600 >/dev/null 2>&1

say "Checking credential file after container recreate:"
cexec "$A" 'ls -la /root/.claude/.credentials.json'
cexec "$A" 'cat /root/.claude/.credentials.json | grep -o "sk-ant-[a-z0-9-]*"'

say "Claude test complete."
