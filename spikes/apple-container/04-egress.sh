#!/usr/bin/env bash
# Item 4: default-deny egress. An --internal network plus a host-side logging
# allowlist proxy, compared with the default network.
set -uo pipefail
. "$(dirname "$0")/lib.sh"
HERE="$(cd "$(dirname "$0")" && pwd)"
SCRATCH=${SCRATCH:?set SCRATCH to a scratch directory on the host}
rm -rf "$SCRATCH/tools" && mkdir -p "$SCRATCH/tools" && cp "$HERE/bin-probe-linux" "$SCRATCH/tools/probe"
PIDS=()
finish() { for p in "${PIDS[@]:-}"; do [ -n "$p" ] && kill "$p" 2>/dev/null; done; cleanup; }
trap finish EXIT
NET=${PREFIX}-net
ip_of() { container inspect "$1" | python3 -c 'import json,sys; n=json.load(sys.stdin)[0]["status"]["networks"][0]; print(n["ipv4Address"].split("/")[0], n["ipv4Gateway"], n["network"])'; }
mount_tools="--mount type=bind,source=$SCRATCH/tools,target=/tools,readonly"
LANIP=$(ipconfig getifaddr en0 2>/dev/null); DEFGW=$(route -n get default 2>/dev/null | awk '/gateway/{print $2}')

hdr "network create --internal"
container network create --internal $NET 2>&1 | tail -1
container network ls | grep -E "NETWORK|$NET"
container network inspect $NET 2>&1 | head -30

"$HERE/bin-probe-host" listen-tcp 0.0.0.0:18082 2>/dev/null & PIDS+=($!)
sleep 0.3

I=${PREFIX}-e1; D=${PREFIX}-e2
container run -d --name $I --init --network $NET $mount_tools $IMG sleep 900 >/dev/null 2>&1
container run -d --name $D --init $mount_tools $IMG sleep 900 >/dev/null 2>&1
read -r IIP IGW INET < <(ip_of $I); read -r DIP DGW DNET < <(ip_of $D)
say "internal container: ip=$IIP gateway=$IGW network=$INET"
say "default  container: ip=$DIP gateway=$DGW network=$DNET"
ifconfig | grep -E '^bridge' | tr '\n' ' '; echo

hdr "reachability matrix from the INTERNAL container"
t() { printf '%-58s %s\n' "$1" "$(cexec $I "$2" 2>&1 | head -1 | cut -c1-90)"; }
t "internet 1.1.1.1:443"                       "/tools/probe dial-tcp 1.1.1.1:443"
t "https://example.com direct"                 "/tools/probe http-get https://example.com"
t "dns: getent hosts example.com"              "getent hosts example.com || echo no-resolution"
t "LAN default gateway $DEFGW:80"              "/tools/probe dial-tcp $DEFGW:80"
t "host via internal gateway $IGW:18082"       "/tools/probe dial-tcp $IGW:18082"
t "host via LAN ip $LANIP:18082"               "/tools/probe dial-tcp $LANIP:18082"
t "host via default gateway 192.168.64.1:18082" "/tools/probe dial-tcp 192.168.64.1:18082"
t "container on the default network $DIP:22"   "/tools/probe dial-tcp $DIP:22"
t "ipv6 internet [2606:4700:4700::1111]:443"   "/tools/probe dial-tcp [2606:4700:4700::1111]:443"

hdr "reachability from the DEFAULT container to the internal one"
printf '%-58s %s\n' "default -> internal $IIP:22" "$(cexec $D "/tools/probe dial-tcp $IIP:22" 2>&1 | head -1 | cut -c1-90)"

hdr "host-side allowlist proxy bound only to the internal gateway $IGW:3128"
"$HERE/bin-probe-host" proxy "$IGW:3128" "example.com,proxy.golang.org" 2>"$SCRATCH/proxy.log" & PIDS+=($!)
sleep 0.5
head -2 "$SCRATCH/proxy.log"
say "allowed  https://example.com via proxy     : $(cexec $I "/tools/probe http-get https://example.com http://$IGW:3128" 2>&1 | head -1)"
say "allowed  https://proxy.golang.org via proxy : $(cexec $I "/tools/probe http-get https://proxy.golang.org http://$IGW:3128" 2>&1 | head -1)"
say "DENIED   https://github.com via proxy      : $(cexec $I "/tools/probe http-get https://github.com http://$IGW:3128" 2>&1 | head -1)"
say "DENIED   http://example.org via proxy      : $(cexec $I "/tools/probe http-get http://example.org http://$IGW:3128" 2>&1 | head -1)"
say "curl with HTTPS_PROXY (name resolved by proxy): $(cexec $I "HTTPS_PROXY=http://$IGW:3128 curl -s -o /dev/null -w '%{http_code}' https://example.com" 2>&1)"
say "direct attempt still blocked after the proxy exists: $(cexec $I '/tools/probe http-get https://github.com' 2>&1 | head -1 | cut -c1-80)"
say "proxy reachable from the DEFAULT container?  $(cexec $D "/tools/probe dial-tcp $IGW:3128" 2>&1 | head -1 | cut -c1-80)"
hdr "proxy log"
cat "$SCRATCH/proxy.log"
