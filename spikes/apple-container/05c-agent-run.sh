#!/usr/bin/env bash
# Item 5 (continued): an authenticated agent run inside a container.
#
#   host harness (spike #1) --container exec -i--> claude in a stock image
#   on an --internal network, tools from the read-only store, model API
#   reached only through the allowlist proxy sidecar.
#
# The login token is read from a mode-600 env file that the caller provides and
# is passed per exec with --env-file, so it never sits in the container's own
# configuration, on a command line or in this repository. The caller deletes
# the file afterwards and revokes the token.
set -uo pipefail
. "$(dirname "$0")/lib.sh"
HERE="$(cd "$(dirname "$0")" && pwd)"
SCRATCH=${SCRATCH:?set SCRATCH to a scratch directory with store/, token.env and transcript-spike}
TOKEN_FILE="$SCRATCH/token.env"
STORE="$SCRATCH/store"
HARNESS="$SCRATCH/transcript-spike"
for f in "$TOKEN_FILE" "$HARNESS"; do [ -e "$f" ] || { echo "missing $f" >&2; exit 2; }; done
[ -d "$STORE/profiles/default/bin" ] || { echo "missing tool store (LIGHT=1 STORE=... ./build-store.sh)" >&2; exit 2; }
ALLOW=${ALLOW:-api.anthropic.com}
B=127.0.0.1:8788; BASE=http://$B
NET=${PREFIX}-net; HOMEV=${PREFIX}-ahome; WORKV=${PREFIX}-awork
P=${PREFIX}-p1; A=${PREFIX}-ag1
PIDS=()
finish() { for p in "${PIDS[@]:-}"; do [ -n "$p" ] && kill "$p" 2>/dev/null; done; cleanup; }
trap finish EXIT
rm -rf "$SCRATCH/tools" && mkdir -p "$SCRATCH/tools" && cp "$HERE/bin-probe-linux" "$SCRATCH/tools/probe"
CLAUDE=/opt/store/profiles/default/bin/claude

hdr "setup: internal network, proxy sidecar (allow: $ALLOW), agent container on the store + volumes"
container network create --internal $NET >/dev/null 2>&1
container volume create $HOMEV >/dev/null 2>&1; container volume create $WORKV >/dev/null 2>&1
container run -d --name $P --init --network default --network $NET \
  --mount type=bind,source="$SCRATCH/tools",target=/tools,readonly $IMG /tools/probe proxy 0.0.0.0:3128 "$ALLOW" >/dev/null 2>&1
sleep 1.5
PIP=$(container inspect $P | python3 -c 'import json,sys; ns=json.load(sys.stdin)[0]["status"]["networks"]; print([n["ipv4Address"].split("/")[0] for n in ns if n["network"]=="'$NET'"][0])')
run_agent() {
  container run -d --name $A --init --network $NET --cpus 2 --memory 2G \
    --mount type=bind,source="$STORE",target=/opt/store,readonly -v $HOMEV:/root -v $WORKV:/work \
    -e HTTPS_PROXY=http://$PIP:3128 -e HTTP_PROXY=http://$PIP:3128 -e NO_PROXY=localhost,127.0.0.1 \
    $IMG sleep 7200 >/dev/null 2>&1
}
run_agent
say "proxy internal ip $PIP; agent state: $(container ls | awk -v c=$A '$1==c{print $5}')"
cexec $A 'echo "agent image: $(. /etc/os-release; echo $PRETTY_NAME) | tool: $(/opt/store/profiles/default/bin/claude --version)"'

hdr "1. direct headless run inside the container with the login token"
OUT=$(container exec --env-file "$TOKEN_FILE" -w /work $A sh -c "echo 'Reply with exactly: pong' | $CLAUDE -p --model haiku --max-turns 1 2>&1" | head -c 400)
say "reply: $OUT"
say "through the proxy variables (curl obeys HTTPS_PROXY): $(container exec $A /bin/sh -c 'curl -s -o /dev/null -w "%{http_code}" --max-time 5 https://api.anthropic.com 2>&1; true')"
say "direct, ignoring the proxy variables (must fail): $(container exec $A /bin/sh -c 'curl --noproxy "*" -s -o /dev/null -w "%{http_code}" --max-time 5 https://api.anthropic.com 2>&1; true')"
hdr "hosts the agent contacted (proxy log)"
container logs $P 2>&1 | grep -E 'ALLOW|DENY' | awk '{print $2, $3, $4}' | sort | uniq -c | sed -E 's/^ +/  /'

hdr "2. the spike #1 harness on the host drives the agent in the container"
cat > "$SCRATCH/claude-in-container" <<WRAP
#!/bin/sh
# Stands in for the claude binary: run it inside the container, stdin and stdout passed through.
exec container exec -i --env-file "$TOKEN_FILE" -w /work $A $CLAUDE "\$@"
WRAP
chmod 700 "$SCRATCH/claude-in-container"
rm -rf "$SCRATCH/data"
"$HARNESS" -addr $B -dir "$SCRATCH" -data "$SCRATCH/data" -approvals=false -claude "$SCRATCH/claude-in-container" \
  -tools "Read,Bash(ls:*),Bash(cat:*),Bash(sleep:*)" > "$SCRATCH/harness.log" 2>&1 & PIDS+=($!)
sleep 1
ev() { jq -c "select(.id>$1) | {id,kind,tool,text:((.text//\"\")|.[0:70])}" "$SCRATCH/data/events.jsonl"; }
n() { curl -s $BASE/state | jq .events; }
idle() { for i in $(seq 1 45); do [ "$(curl -s $BASE/state | jq -r .status)" = idle ] && [ "$(n)" -gt "$1" ] && return 0; sleep 2; done; return 1; }

N0=$(n)
curl -s -X POST "$BASE/start?mode=manual" -d '{"text":"Run the shell command `ls /work`, then run `sleep 8`, then reply with the single word: done."}' -o /dev/null -w "start=%{http_code}\n"
sleep 4
curl -s -X POST $BASE/say -d '{"text":"Also end your reply with the word BANANA."}' -o /dev/null -w "say (mid-run)=%{http_code}\n"
sleep 3; container stats --no-stream $A 2>&1 | awk 'NR<=2' | sed 's/^/while running: /'
idle $N0 && say "turn finished" || say "turn did not finish in time"
ev $N0

hdr "3. stop and start the container, then resume the same session from the volume"
SID=$(curl -s $BASE/state | jq -r .session_id)
curl -s -X POST $BASE/cancel -o /dev/null; sleep 2
container stop $A >/dev/null 2>&1; container start $A >/dev/null 2>&1; sleep 1
say "container after stop and start: $(container ls | awk -v c=$A '$1==c{print $5}'), session id kept by the harness: ${SID:0:8}..."
say "agent session files on the volume: $(cexec $A 'ls /root/.claude/projects 2>/dev/null | head -3 | tr "\n" " "')"
N1=$(n)
curl -s -X POST "$BASE/start?resume=1" -d '{"text":"What word did I ask you to end your reply with? Answer with just that word."}' -o /dev/null -w "resume start=%{http_code}\n"
idle $N1 && say "resumed turn finished" || say "resumed turn did not finish in time"
ev $N1 | grep -E '"(delta|text|result|error)"' | head -6

hdr "4. egress after everything: what the proxy saw, and the direct path"
container logs $P 2>&1 | grep -E 'ALLOW|DENY' | awk '{print $2, $3, $4}' | sort | uniq -c | sed -E 's/^ +/  /'
say "through the proxy, a host that is not allowed (must be blocked): $(container exec $A /bin/sh -c 'curl -s -o /dev/null -w "%{http_code}" --max-time 5 https://example.com 2>&1; true')"
say "direct to a raw IP, ignoring the proxy (must fail): $(container exec $A /bin/sh -c 'curl --noproxy "*" -s -o /dev/null -w "%{http_code}" --max-time 5 https://1.1.1.1 2>&1; true')"
hdr "memory of the agent container now"
container stats --no-stream $A 2>&1 | awk 'NR<=2'
