#!/usr/bin/env bash
# Tests Codex CLI 0.159.2 sign-in flows in a hardened environment.
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

ALLOW="auth.openai.com,api.openai.com,chatgpt.com,chat.openai.com,cdn.oaistatic.com,oaistatic.com"
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

hdr "2. Test Codex CLI sign-in flow: 'codex login --device-auth'"
say "Executing 'codex login --device-auth' with timeout 6 to capture device code and URL:"
cexec "$A" 'timeout 6 /tools/codex login --device-auth 2>&1 || true' > "$RESULTS/codex-device-auth.out"
cat "$RESULTS/codex-device-auth.out"

hdr "3. Test Codex CLI sign-in flow: plain 'codex login'"
say "Executing 'codex login' with timeout 5 to observe local server fallback:"
cexec "$A" 'timeout 5 /tools/codex login 2>&1 || true' > "$RESULTS/codex-plain-login.out"
cat "$RESULTS/codex-plain-login.out"

hdr "4. Proxy access logs during Codex login attempt:"
container logs "$P" 2>&1 | tail -20

hdr "5. Verify credential storage location when simulated token is written to ~/.codex/auth.json"
say "Creating test session on agent-home volume:"
cexec "$A" 'mkdir -p /root/.codex && chmod 700 /root/.codex'
cexec "$A" 'cat << "JSON" > /root/.codex/auth.json
{
  "auth_mode": "chatgpt",
  "OPENAI_API_KEY": null,
  "tokens": {
    "id_token": "test-id-token",
    "access_token": "test-access-token",
    "refresh_token": "test-refresh-token",
    "account_id": "test-account-id"
  },
  "last_refresh": "2026-10-01T20:00:00Z"
}
JSON
chmod 600 /root/.codex/auth.json'

say "Checking permissions on auth.json:"
cexec "$A" 'ls -la /root/.codex/auth.json'

say "Testing 'codex login status':"
cexec "$A" '/tools/codex login status 2>&1 || true' > "$RESULTS/codex-login-status.out"
cat "$RESULTS/codex-login-status.out"

hdr "6. Stop, start, and rebuild test on the same agent-home volume"
say "Stopping container $A..."
container stop "$A" >/dev/null 2>&1
say "Starting container $A..."
container start "$A" >/dev/null 2>&1
say "Checking auth file after stop & start:"
cexec "$A" 'cat /root/.codex/auth.json | grep -o "test-[a-z-]*"'

say "Deleting container $A and recreating with SAME volume $VOL..."
container rm -f "$A" >/dev/null 2>&1
container run -d --name "$A" --init --network "$NET" --cpus 2 --memory 2G \
  -v "$VOL:/root" \
  -v "$BIN:/tools:ro" \
  -e HTTPS_PROXY="http://$PIP:3128" -e HTTP_PROXY="http://$PIP:3128" -e NO_PROXY="localhost,127.0.0.1" \
  "$IMG" sleep 3600 >/dev/null 2>&1

say "Checking auth file after container recreate:"
cexec "$A" 'ls -la /root/.codex/auth.json'
cexec "$A" 'cat /root/.codex/auth.json | grep -o "test-[a-z-]*"'

say "Codex test complete."
