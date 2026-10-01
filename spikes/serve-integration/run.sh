#!/bin/sh
# Integration pass (issue #27, #28 groundwork): a real `whr serve` on Apple Container, up to the
# agent's first request, without a login. Everything lives under $HOME/whr-serve-int (the runtime
# refuses bind mounts below system directories such as /tmp).
# Usage: run.sh   (from the repository root, on a committed tree)
set -u
ROOT=$HOME/whr-serve-int
OUT=$(cd "$(dirname "$0")" && pwd)/output
PORT=8799; GHPORT=8798
IMAGE=${IMAGE:-docker.io/library/golang:1.27.1-trixie}   # a stock image that has git; fedora:latest has none (run1)
mkdir -p "$OUT"
log() { printf '%s\n' "$*" | tee -a "$OUT/steps.txt"; }
: >"$OUT/steps.txt"

cleanup() {
  [ -n "${SERVE_PID:-}" ] && kill "$SERVE_PID" 2>/dev/null && wait "$SERVE_PID" 2>/dev/null
  [ -n "${GH_PID:-}" ] && kill "$GH_PID" 2>/dev/null
  # remove what this run made, by exact name and the owner label only
  container list --all --format json 2>/dev/null | python3 -c '
import json,sys
for c in json.load(sys.stdin):
    if c["configuration"].get("labels",{}).get("workharbor.owner")=="whr": print(c["id"])' | while read -r id; do
    container stop "$id" >/dev/null 2>&1; container delete "$id" >/dev/null 2>&1
  done
  container network list --format json 2>/dev/null | python3 -c '
import json,sys
for n in json.load(sys.stdin):
    if n.get("config",{}).get("labels",{}).get("workharbor.owner")=="whr" or n.get("configuration",{}).get("labels",{}).get("workharbor.owner")=="whr": print(n.get("id") or n.get("name"))' | while read -r n; do container network delete "$n" >/dev/null 2>&1; done
  container volume list --format json 2>/dev/null | python3 -c '
import json,sys
for v in json.load(sys.stdin):
    if v.get("configuration",{}).get("labels",{}).get("workharbor.owner")=="whr": print(v["id"])' | while read -r v; do container volume delete "$v" >/dev/null 2>&1; done
}
trap cleanup EXIT

[ -d "$ROOT" ] && chmod -R u+w "$ROOT" 2>/dev/null
rm -rf "$ROOT"; mkdir -p "$ROOT/ws" "$ROOT/secrets" "$ROOT/state" "$ROOT/src" "$ROOT/prefix/bin" "$ROOT/prefix/libexec/whr"
chmod 700 "$ROOT/secrets"

log "== 1. build and install whr, whr-shim and whr-proxy (as make install does, from this commit)"
go build -trimpath -o "$ROOT/prefix/bin/whr" ./cmd/whr || exit 1
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o "$ROOT/prefix/libexec/whr/whr-shim-linux-arm64" ./cmd/whr-shim || exit 1
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o "$ROOT/prefix/libexec/whr/whr-proxy-linux-arm64" ./cmd/whr-proxy || exit 1
go build -o "$ROOT/prefix/fakegithub" ./spikes/serve-integration/fakegithub || exit 1
go build -o "$ROOT/prefix/setup" ./spikes/serve-integration/setup || exit 1
WHR=$ROOT/prefix/bin/whr

log "== 2. the tool store: pinned Claude Code (checked against the pin and the vendor manifest) and the launcher"
"$WHR" tools build -store "$ROOT/store" -shim "$ROOT/prefix/libexec/whr/whr-shim-linux-arm64" 2>&1 | tee -a "$OUT/steps.txt"

log "== 3. secrets (0600) and the configuration"
openssl genrsa -out "$ROOT/secrets/app.pem" 2048 2>/dev/null; chmod 600 "$ROOT/secrets/app.pem"
python3 -c 'import secrets;print(secrets.token_hex(24))' >"$ROOT/secrets/api.token"; chmod 600 "$ROOT/secrets/api.token"
cat >"$ROOT/config.json" <<JSON
{
  "listen": "127.0.0.1:$PORT",
  "repositories": [{"name": "wstein/workharbor"}],
  "roots": {"workspaces": ["$ROOT/ws"], "tool_store": "$ROOT/store"},
  "github": {"app_id": 4242, "key_file": "$ROOT/secrets/app.pem", "api_url": "http://127.0.0.1:$GHPORT"},
  "api_token_file": "$ROOT/secrets/api.token",
  "state_dir": "$ROOT/state",
  "agent_allowed_tools": ["Read", "Glob", "Grep"],
  "environment": {"image": "$IMAGE", "egress_allow": ["api.anthropic.com"]}
}
JSON
chmod 600 "$ROOT/config.json"

log "== 4. a source repository (the human's, outside every workspace root) and a stand-in for the GitHub API"
git init -q -b main "$ROOT/src/repo" && ( cd "$ROOT/src/repo" && echo "# hello" >README.md && git add . && git -c user.name=h -c user.email=h@h commit -q -m "base" )
"$ROOT/prefix/fakegithub" -listen 127.0.0.1:$GHPORT >"$OUT/fakegithub.log" 2>&1 & GH_PID=$!
sleep 1

log "== 5. create a workspace and an agent through the real stack (clone, environment, tools mount, agent home volume, egress sidecar, worktree)"
"$ROOT/prefix/setup" -config "$ROOT/config.json" -exe "$WHR" -name docs-ws -role docs -source "$ROOT/src/repo" -repo wstein/workharbor -folder "$ROOT/ws/docs" 2>&1 | tee "$OUT/setup.txt" | tee -a "$OUT/steps.txt"

log "== 6. what the runtime now holds for the owner whr"
container list --all 2>&1 | tee "$OUT/containers.txt" | tee -a "$OUT/steps.txt"
container network list 2>&1 | tee -a "$OUT/steps.txt"
container volume list 2>&1 | tee -a "$OUT/steps.txt"

log "== 7. whr serve (reconciles first, then serves the API on loopback)"
"$WHR" serve --config "$ROOT/config.json" >"$OUT/serve.log" 2>&1 & SERVE_PID=$!
i=0; until grep -q "listening on" "$OUT/serve.log" 2>/dev/null || [ $i -gt 30 ]; do sleep 1; i=$((i+1)); done
cat "$OUT/serve.log" | tee -a "$OUT/steps.txt"
W="$WHR --config $ROOT/config.json"
log "-- whr ls (empty)"; $W ls 2>&1 | tee -a "$OUT/steps.txt"

log "== 8. whr run: the issue comes from the stand-in forge, the agent starts in its worktree"
$W run https://github.com/wstein/workharbor/issues/1 --agent docs-ws/docs 2>&1 | tee "$OUT/run.txt" | tee -a "$OUT/steps.txt"
sleep 20
log "-- whr ls"; $W ls 2>&1 | tee -a "$OUT/steps.txt"
log "-- whr inbox"; $W inbox 2>&1 | tee "$OUT/inbox.txt" | tee -a "$OUT/steps.txt"
TASK=$($W ls 2>/dev/null | awk 'NR==2{print $1}')
log "-- whr logs $TASK"; $W logs "$TASK" 2>&1 | cut -c1-400 | tee "$OUT/logs.txt" | tee -a "$OUT/steps.txt"
log "-- the fake forge saw (requests, never a credential)"; cat "$OUT/fakegithub.log" | tee -a "$OUT/steps.txt"
log "-- serve log"; cat "$OUT/serve.log" | tee -a "$OUT/steps.txt"
log "== 9. what is inside the environment 40 s after the run started"
sleep 20
ENV=$(container list --all --format json | python3 -c '
import json,sys
for c in json.load(sys.stdin):
    l=c["configuration"].get("labels",{})
    if l.get("workharbor.owner")=="whr" and l.get("workharbor.role")=="environment": print(c["id"])')
log "environment: $ENV"
{
  echo "--- id, home, worktree, tools"
  container exec "$ENV" sh -c 'id; echo HOME=$HOME; ls -la /ws /ws/wt/docs /home/agent /tools/profiles/*/bin 2>&1 | head -40'
  echo "--- processes (from /proc)"
  container exec "$ENV" sh -c 'for p in /proc/[0-9]*; do printf "%s " "${p#/proc/}"; tr "\0" " " <$p/cmdline | cut -c1-200; echo; done'
  echo "--- proxy variables the agent could use (none are set for it by whr unless listed here)"
  container exec "$ENV" sh -c 'env | grep -i proxy; echo "(end)"'
  echo "--- can the guest reach the internet directly? (it must not) and through the sidecar?"
  container exec "$ENV" sh -c 'timeout 8 curl -sS -m 6 -o /dev/null -w "direct %{http_code}\n" https://api.anthropic.com 2>&1 | tail -1'
  P=$(container list --format json | python3 -c '
import json,sys
for c in json.load(sys.stdin):
    l=c["configuration"].get("labels",{})
    if l.get("workharbor.role")=="sidecar":
        for n in c["status"]["networks"]:
            if "128" in n["ipv4Address"]: print(n["ipv4Address"].split("/")[0])')
  echo "sidecar address on the internal network: $P"
  container exec "$ENV" sh -c "timeout 15 curl -sS -m 10 -x http://$P:3128 -o /dev/null -w 'via the proxy %{http_code}\n' https://api.anthropic.com 2>&1 | tail -1"
  echo "--- claude by hand, as the adapter runs it, with the proxy set"
  container exec "$ENV" sh -c "cd /ws/wt/docs && HOME=/home/agent CLAUDE_CONFIG_DIR=/home/agent/.claude HTTPS_PROXY=http://$P:3128 timeout 40 /tools/profiles/*/bin/claude -p hello --output-format stream-json --verbose 2>&1 | cut -c1-400 | head -12"
  echo "--- the same without the proxy variable"
  container exec "$ENV" sh -c "cd /ws/wt/docs && HOME=/home/agent CLAUDE_CONFIG_DIR=/home/agent/.claude timeout 40 /tools/profiles/*/bin/claude -p hello --output-format stream-json --verbose 2>&1 | cut -c1-400 | head -12"
} 2>&1 | tee "$OUT/inside.txt" | tee -a "$OUT/steps.txt"
log "-- whr logs again"; $W logs "$TASK" 2>&1 | cut -c1-300 | tee -a "$OUT/steps.txt"
log "== done"
