#!/usr/bin/env bash
# Item 3: isolation and escape tests. Probes only loopback, the host gateway,
# the Mac's own LAN address, the default gateway (one TCP connect each) and
# test listeners started here.
set -uo pipefail
. "$(dirname "$0")/lib.sh"
HERE="$(cd "$(dirname "$0")" && pwd)"
SCRATCH=${SCRATCH:?set SCRATCH to a scratch directory on the host}
SOCKDIR=${SOCKDIR:-/tmp/whs-sock}   # short on purpose: macOS limits unix socket paths to 104 bytes
rm -rf "$SCRATCH/tools" "$SOCKDIR" && mkdir -p "$SCRATCH/tools" "$SOCKDIR"
cp "$HERE/bin-probe-linux" "$SCRATCH/tools/probe"
PIDS=()
finish() { for p in "${PIDS[@]:-}"; do [ -n "$p" ] && kill "$p" 2>/dev/null; done; rm -rf "$SOCKDIR"; cleanup; }
trap finish EXIT

LANIP=$(ipconfig getifaddr en0 2>/dev/null)
DEFGW=$(route -n get default 2>/dev/null | awk '/gateway/{print $2}')
say "mac LAN ip: $LANIP | LAN default gateway: $DEFGW"

hdr "A. adapter-level mount deny-list (the runtime accepts any host path; see B)"
# check_mount resolves symlinks first, then rejects the home directory and its
# parents, secrets directories, unix sockets and runtime sockets.
check_mount() {
  local p; p=$(python3 -c 'import os,sys; print(os.path.realpath(sys.argv[1]))' "$1" 2>/dev/null) || { echo "reject (unresolvable)"; return; }
  local home; home=$(python3 -c 'import os; print(os.path.realpath(os.environ["HOME"]))')
  [ -S "$p" ] && { echo "reject (unix socket)"; return; }
  case "$p" in
    "$home"|"$home"/.ssh|"$home"/.ssh/*|"$home"/.gnupg*|"$home"/.aws*|"$home"/.config/gh*|"$home"/Library/Keychains*) echo "reject (secrets or home: $p)"; return;;
    "$home"/.socktainer*|/var/run/*|/private/var/run/*|/run/*) echo "reject (runtime socket dir: $p)"; return;;
    /|/Users|/private|/var|/etc|/System) echo "reject (too broad: $p)"; return;;
  esac
  case "$home" in "$p"/*) echo "reject (parent of home: $p)"; return;; esac
  echo "accept ($p)"
}
ln -sfn "$HOME" "$SCRATCH/home-link"
for p in "$HOME" "$HOME/.ssh" "$SCRATCH/home-link" /Users / "$HOME/.socktainer" "$SOCKDIR" "$SCRATCH/tools"; do
  printf '%-48s -> %s\n' "$p" "$(check_mount "$p")"
done
rm -f "$SCRATCH/home-link"

hdr "B. the runtime accepts an arbitrary host path (read-only /etc, only existence checked)"
N=${PREFIX}-i1
container run -d --name $N --init --mount type=bind,source=/etc,target=/hostetc,readonly $IMG sleep 600 >/dev/null 2>&1
cexec $N 'test -r /hostetc/hosts && echo "mounted /etc: readable"; touch /hostetc/x 2>&1 | head -1'
container rm -f $N >/dev/null 2>&1

hdr "C. unix sockets: can a guest connect to a host unix socket?"
"$HERE/bin-probe-host" listen-unix "$SOCKDIR/test.sock" 2>"$SCRATCH/unix.log" & PIDS+=($!)
sleep 0.5
container run -d --name $N --init --mount type=bind,source="$SOCKDIR",target=/sock \
  --mount type=bind,source="$SCRATCH/tools",target=/tools,readonly \
  --mount type=bind,source="$HOME/.socktainer",target=/socktainer,readonly $IMG sleep 600 >/dev/null 2>&1
say "socket file visible in guest: $(cexec $N 'ls -l /sock/test.sock 2>&1')"
say "connect to a test unix socket (mounted):  $(cexec $N '/tools/probe dial-unix /sock/test.sock' 2>&1)"
say "host listener saw a connection:          $(grep -c accepted "$SCRATCH/unix.log")"
say "socktainer socket visible in guest:       $(cexec $N 'ls -l /socktainer/container.sock 2>&1')"
say "connect to the real socktainer socket:    $(cexec $N '/tools/probe dial-unix /socktainer/container.sock' 2>&1)"
container rm -f $N >/dev/null 2>&1

hdr "D. network: guest to host, LAN and other containers"
"$HERE/bin-probe-host" listen-tcp 127.0.0.1:18081 2>/dev/null & PIDS+=($!)
"$HERE/bin-probe-host" listen-tcp 0.0.0.0:18082 2>"$SCRATCH/tcp82.log" & PIDS+=($!)
sleep 0.5
A=${PREFIX}-i2; B=${PREFIX}-i3
container run -d --name $A --init --mount type=bind,source="$SCRATCH/tools",target=/tools,readonly $IMG sleep 600 >/dev/null 2>&1
container run -d --name $B --init --mount type=bind,source="$SCRATCH/tools",target=/tools,readonly $IMG /tools/probe listen-tcp 0.0.0.0:18083 >/dev/null 2>&1
sleep 1
BIP=$(container inspect $B | python3 -c 'import json,sys; print(json.load(sys.stdin)[0]["status"]["networks"][0]["ipv4Address"].split("/")[0])')
AIP=$(container inspect $A | python3 -c 'import json,sys; print(json.load(sys.stdin)[0]["status"]["networks"][0]["ipv4Address"].split("/")[0])')
HOSTGW=$(container inspect $A | python3 -c 'import json,sys; print(json.load(sys.stdin)[0]["status"]["networks"][0]["ipv4Gateway"])')
say "container A=$AIP B=$BIP host gateway=$HOSTGW"
say "guest -> host loopback-only listener via gateway $HOSTGW:18081 : $(cexec $A "/tools/probe dial-tcp $HOSTGW:18081" 2>&1)"
say "guest -> host all-interfaces listener via gateway $HOSTGW:18082: $(cexec $A "/tools/probe dial-tcp $HOSTGW:18082" 2>&1)"
say "guest -> host all-interfaces listener via LAN ip $LANIP:18082   : $(cexec $A "/tools/probe dial-tcp $LANIP:18082" 2>&1)"
say "guest -> its own 127.0.0.1:18081 (guest loopback, not the host): $(cexec $A "/tools/probe dial-tcp 127.0.0.1:18081" 2>&1)"
say "guest -> LAN default gateway $DEFGW:80  : $(cexec $A "/tools/probe dial-tcp $DEFGW:80" 2>&1)"
say "guest A -> container B $BIP:18083      : $(cexec $A "/tools/probe dial-tcp $BIP:18083" 2>&1)"
say "host -> container B $BIP:18083         : $("$HERE/bin-probe-host" dial-tcp $BIP:18083 2>&1)"
say "guest -> internet 1.1.1.1:443 (baseline): $(cexec $A "/tools/probe dial-tcp 1.1.1.1:443" 2>&1 | cut -c1-60)"
say "guest -> https://example.com (baseline) : $(cexec $A "/tools/probe http-get https://example.com" 2>&1)"
say "listener 18082 saw: $(grep -c accepted "$SCRATCH/tcp82.log") connections"

hdr "E. hardened container: --read-only --cap-drop ALL --user 1000:1000 --tmpfs /tmp"
H=${PREFIX}-i4
container run -d --name $H --init --read-only --cap-drop ALL --user 1000:1000 --tmpfs /tmp $IMG sleep 600 >/dev/null 2>&1
cexec $H 'id; grep -E "^Cap(Eff|Prm)" /proc/self/status; touch /root/x 2>&1 | head -1; touch /usr/x 2>&1 | head -1; echo hi > /tmp/ok && echo "tmp writable"; mount -t tmpfs none /mnt 2>&1 | head -1'

hdr "F. VM boundary facts"
say "host kernel: $(uname -sr) | guest kernel: $(cexec $A 'uname -sr')"
say "host runtime processes: $(ps aux | grep -c '[c]ontainer-runtime-linux')"
cexec $A 'ls /dev | tr "\n" " "; echo; cat /proc/1/cgroup | head -2'
