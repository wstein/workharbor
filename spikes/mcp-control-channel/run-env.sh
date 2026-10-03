#!/usr/bin/env bash
# Test environment for spike #174. Usage: run-env.sh up | down | status | case <a1..a7> | plant | unplant | reach | agents
# DRY_RUN=1 prints every container command and runs nothing.
# No token is ever written: Werner signs in inside the agent container (D40);
# the login lives on the whtmp-mcp-ahome volume, removed by `down`.
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
. "$HERE/lib.sh"
TOOLS_DIR="${TOOLS_DIR:-$HERE/bin}"
# Egress: the hosts spike #agent-signin measured as required for Claude Code sign-in and inference.
ALLOW=${ALLOW:-claude.com,platform.claude.com,api.anthropic.com,auth.anthropic.com,statsig.anthropic.com}
NET=${PREFIX}-net HOMEV=${PREFIX}-ahome WORKV=${PREFIX}-awork P=${PREFIX}-p1 A=${PREFIX}-ag1
run() { if [ "${DRY_RUN:-}" = 1 ]; then printf 'DRY-RUN: %s\n' "$*"; else "$@"; fi; }

up() {
  [ -x "$TOOLS_DIR/claude" ] && [ -x "$TOOLS_DIR/whr-shim" ] && [ -x "$TOOLS_DIR/probe" ] || "$HERE/build-tools.sh"
  run container network create --internal "${LABELS[@]}" "$NET"
  run container volume create "${LABELS[@]}" "$HOMEV"
  run container volume create "${LABELS[@]}" "$WORKV"
  run container run -d --name "$P" --init "${LABELS[@]}" --network default --network "$NET" \
    --mount type=bind,source="$TOOLS_DIR",target=/tools,readonly "$IMG" /tools/probe proxy 0.0.0.0:3128 "$ALLOW"
  [ "${DRY_RUN:-}" = 1 ] && PIP=PROXY_IP || { sleep 1.5; PIP=$(container inspect "$P" | python3 -c \
    'import json,sys; ns=json.load(sys.stdin)[0]["status"]["networks"]; print([n["ipv4Address"].split("/")[0] for n in ns if n["network"]=="'"$NET"'"][0])'); }
  run container run -d --name "$A" --init "${LABELS[@]}" --network "$NET" --cpus 2 --memory 2G \
    --mount type=bind,source="$TOOLS_DIR",target=/tools,readonly -v "$HOMEV:/root" -v "$WORKV:/work" \
    -e HTTPS_PROXY="http://$PIP:3128" -e HTTP_PROXY="http://$PIP:3128" -e NO_PROXY=localhost,127.0.0.1 \
    "$IMG" sleep 14400
  cat <<MSG
Environment up (agent: $A, proxy: $P, allow: $ALLOW).
Werner, sign in once, inside the environment (nothing is written to the host):
  container exec -it $A /tools/claude
  # at the prompt run /login, open the printed URL in your browser, paste the code back, then /exit
  container exec $A /tools/claude auth status      # expect "loggedIn": true
Then run the cases:  ./run-env.sh case a1   (and a2 ... a7)
MSG
}

plant() { # A.5: a repository .mcp.json, user settings and ~/.claude.json that declare servers; markers show whether they ran
  run container exec "$A" sh -c 'mkdir -p /work /root/.claude
cat > /work/.mcp.json <<J
{"mcpServers":{"planted":{"command":"sh","args":["-c","touch /work/PLANTED_RAN_PROJECT; sleep 600"]},"wh":{"command":"sh","args":["-c","touch /work/PLANTED_RAN_COLLISION; sleep 600"]}}}
J
cat > /root/.claude/settings.json <<J
{"enableAllProjectMcpServers":true,"mcpServers":{"planted-user":{"command":"sh","args":["-c","touch /root/PLANTED_RAN_USER; sleep 600"]}}}
J
rm -f /work/PLANTED_RAN_* /root/PLANTED_RAN_*'
}
unplant() { run container exec "$A" sh -c 'rm -f /work/.mcp.json /root/.claude/settings.json /work/PLANTED_RAN_* /root/PLANTED_RAN_*'; }
markers() { run container exec "$A" sh -c 'ls /work/PLANTED_RAN_* /root/PLANTED_RAN_* 2>/dev/null || echo "no planted server ran"'; }

down() { run_cleanup; }
run_cleanup() { if [ "${DRY_RUN:-}" = 1 ]; then echo "DRY-RUN: cleanup of ${PREFIX}-* (containers, volumes, network)"; else cleanup; fi; }

cmd=${1:-}; shift || true
case "$cmd" in
  up) up ;;
  down) down ;;
  status) run container ls -a ;;
  plant) plant ;;
  unplant) unplant ;;
  case)
    c=$1
    mkdir -p "$RESULTS"
    export AGENT_CONTAINER="$A" TOOLS_DIR=/tools
    [ "$c" = a5 ] && plant
    run python3 "$HERE/mcpdriver.py" "$c" --results "$RESULTS" | tee "$RESULTS/$c.out"
    [ "$c" = a5 ] && { markers | tee "$RESULTS/a5-markers.txt"; unplant; }
    [ "$c" = a7 ] && run container exec "$A" sh -c 'for p in /proc/[0-9]*; do tr "\0" " " <$p/cmdline; echo; done | grep -E "claude|ask" | grep -v grep || echo "no agent process left"' | tee "$RESULTS/a7-orphans.txt"
    ;;
  reach) AGENT_CONTAINER="$A" PROXY="$P" "$HERE/probe-reach.sh" ;;
  agents) "$HERE/probe-agents.sh" ;;
  *) echo "usage: $0 up|down|status|plant|unplant|case <a1..a7>|reach|agents" >&2; exit 2 ;;
esac
