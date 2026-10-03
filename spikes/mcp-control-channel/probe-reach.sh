#!/usr/bin/env bash
# Part B: what an --internal guest can reach, with no new host listener. Read-only on the host.
# Run once with the macOS Application Firewall off and once on (Werner toggles it: this script never does)
# and save each output (FIREWALL=off|on). Uses the environment from run-env.sh up.
set -uo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
. "$HERE/lib.sh"
A=${AGENT_CONTAINER:-${PREFIX}-ag1}
OUT="$RESULTS/b-reach-firewall-${FIREWALL:-unknown}.txt"
{
  hdr "host listeners before (read-only)"; lsof -nP -iTCP -sTCP:LISTEN | awk 'NR>1{print $1,$9}' | sort -u
  hdr "exec round trip into the internal guest (10 samples, ms)"
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    s=$(python3 -c 'import time;print(time.time())'); container exec "$A" true
    python3 -c "import time;print(round((time.time()-$s)*1000,1))"
  done
  hdr "vsock in the guest"
  container exec "$A" sh -c 'ls -l /dev/vsock 2>&1; python3 - <<P 2>&1 || echo "python3 missing in guest"
import socket
try:
    s=socket.socket(socket.AF_VSOCK,socket.SOCK_STREAM); s.settimeout(3)
    print("AF_VSOCK socket ok; connect to CID 2 port 1024:", s.connect_ex((2,1024)))
except Exception as e: print("AF_VSOCK:", e)
P'
  hdr "negative controls from #69: guest to host addresses (no listener expected, so refuse or timeout)"
  gw=$(container exec "$A" sh -c "ip route 2>/dev/null | awk '/default/{print \$3}'" || true)
  for h in "$gw" 192.168.64.1; do
    [ -n "$h" ] || continue
    container exec "$A" sh -c "timeout 3 bash -c 'exec 3<>/dev/tcp/$h/8080' 2>&1 && echo '$h:8080 OPEN' || echo '$h:8080 closed or filtered'"
  done
  hdr "host listeners after"; lsof -nP -iTCP -sTCP:LISTEN | awk 'NR>1{print $1,$9}' | sort -u
} 2>&1 | tee "$OUT"
