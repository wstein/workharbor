#!/usr/bin/env bash
# Item 5b: agent state survives stop, start and rebuild; unauthenticated run
# through the proxy; idle memory. Needs 05a first (volume, network, proxy).
set -uo pipefail
. "$(dirname "$0")/lib.sh"
SCRATCH=${SCRATCH:?set SCRATCH}
NET=${PREFIX}-net; VOL=${PREFIX}-home; A=${PREFIX}-ag1; P=${PREFIX}-p1
PIP=$(cat "$SCRATCH/proxy.ip")
envs="-e HTTPS_PROXY=http://$PIP:3128 -e HTTP_PROXY=http://$PIP:3128 -e NO_PROXY=localhost,127.0.0.1"
CL=/root/.local/bin/claude

hdr "marker in the auth/config directory (/root/.claude lives in the volume)"
cexec $A 'mkdir -p /root/.claude && echo "session-marker-1" > /root/.claude/whspike-marker; cat /root/.claude/whspike-marker'
container stop $A >/dev/null 2>&1; container start $A >/dev/null 2>&1
say "after stop and start: $(cexec $A 'cat /root/.claude/whspike-marker')"
container rm -f $A >/dev/null 2>&1
container run -d --name $A --init --network $NET --cpus 2 --memory 2G -v $VOL:/root $envs $IMG sleep 7200 >/dev/null 2>&1
say "after delete and recreate with the same volume: $(cexec $A 'cat /root/.claude/whspike-marker; ls /root/.local/bin/')"

hdr "unauthenticated headless run inside the container, through the proxy"
cexec $A "cd /tmp && echo '{\"type\":\"user\",\"message\":{\"role\":\"user\",\"content\":[{\"type\":\"text\",\"text\":\"say ok\"}]}}' | timeout 60 $CL -p --input-format stream-json --output-format stream-json --verbose --model haiku --max-turns 1 2>&1 | head -c 3000" > "$SCRATCH/noauth.out" 2>&1
python3 - "$SCRATCH/noauth.out" <<'PY'
import json,sys
for line in open(sys.argv[1]):
    line=line.strip()
    if not line.startswith("{"): print("non-json:", line[:120]); continue
    try: e=json.loads(line)
    except Exception: print("partial:", line[:100]); continue
    print({k:(e.get(k) if not isinstance(e.get(k),(dict,list)) else "...") for k in ("type","subtype","error","is_error","apiKeySource","result") if k in e})
PY
hdr "hosts contacted (proxy log, last lines)"
container logs $P 2>&1 | tail -8

hdr "idle memory of the agent in stream-json mode (waiting for input)"
cexec $A "cd /tmp && (sleep 600 | $CL -p --input-format stream-json --output-format stream-json --verbose --model haiku > /tmp/idle.out 2>&1 &) ; sleep 8; for p in \$(pgrep -f 'claude -p' || true); do echo \"pid \$p: \$(grep -E 'VmRSS|VmHWM' /proc/\$p/status | tr -s ' \t' ' ' | tr '\n' ' ')\"; done; grep -E 'MemTotal|MemAvailable' /proc/meminfo"
container stats --no-stream $A 2>&1 | head -3
cexec $A 'pkill -f "claude -p" ; true'
