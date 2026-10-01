#!/usr/bin/env bash
# Item 6 (continued): what happens to containers, volumes, networks and images
# across `container system stop` and `container system start`.
# Approved by the owner on 2026-10-01. Only whspike-* objects are created or removed.
set -uo pipefail
. "$(dirname "$0")/lib.sh"
SCRATCH=${SCRATCH:?set SCRATCH to a scratch directory on the host}
rm -rf "$SCRATCH/restart" && mkdir -p "$SCRATCH/restart/bind"
VOL=${PREFIX}-rv; NET=${PREFIX}-rnet
R1=${PREFIX}-r1; R2=${PREFIX}-r2; R3=${PREFIX}-r3
runtime_procs() { ps -axo command | grep -c '[c]ontainer-runtime-linux'; }
snapshot() { # snapshot <label>
  say "-- $1"
  container ls -a 2>&1 | awk 'NR==1 || /whspike|fedora2|80feef4f/' | awk '{printf "    %-38.38s %-9s %-16s %s %s\n", $1, $5, $6, $7, $8}'
  say "    networks: $(container network ls 2>&1 | awk 'NR>1{printf "%s(%s) ", $1, $2}')"
  say "    volumes : $(container volume ls 2>&1 | awk 'NR>1{printf "%s ", $1}')"
  say "    images  : $(container image ls 2>&1 | awk 'NR>1{n++} END{print n+0}') entries"
}

hdr "setup: volume, internal network, three containers"
container volume create $VOL >/dev/null 2>&1
container network create --internal $NET >/dev/null 2>&1
echo "bind-before" > "$SCRATCH/restart/bind/b.txt"
mnt="--mount type=bind,source=$SCRATCH/restart/bind,target=/bind"
container run -d --name $R1 --init -v $VOL:/data $mnt $IMG sleep 3600 >/dev/null 2>&1
cexec $R1 'echo rootfs-r1 > /root/rootfs.txt; echo vol-r1 > /data/r1.txt; (sleep 3600 &) ; sleep 1; echo "bg proc started: $(ls /proc | grep -E "^[0-9]+$" | wc -l) procs"'
container run -d --name $R3 --init --network $NET $IMG sleep 3600 >/dev/null 2>&1
cexec $R3 'echo rootfs-r3 > /root/rootfs.txt'
container create --name $R2 --init -v $VOL:/data $IMG sleep 3600 >/dev/null 2>&1
container start $R2 >/dev/null 2>&1; cexec $R2 'echo rootfs-r2 > /root/rootfs.txt'; container stop $R2 >/dev/null 2>&1
say "runtime processes before: $(runtime_procs)"
snapshot "BEFORE stop"
container system status 2>&1 | grep -E 'status|version' | head -2 | sed 's/^/    /'
BEFORE_PIDS=$(launchctl list | grep -c 'apple.container')

hdr "container system stop"
t0=$(now_ms); container system stop 2>&1 | tail -3; t1=$(now_ms)
say "stop took $((t1-t0)) ms"
sleep 2
say "runtime processes right after stop: $(runtime_procs) | launchd container jobs: $(launchctl list | grep -c 'apple.container') (before: $BEFORE_PIDS)"
say "cli while stopped: $(timeout 20 container ls -a 2>&1 | head -2 | tr '\n' ' ' | cut -c1-160)"
say "apiserver process: $(ps -axo command | grep -c '[c]ontainer-apiserver') | any container VM processes: $(ps -axo command | grep -c '[c]ontainer-runtime-linux')"

hdr "container system start (--disable-kernel-install, non-interactive)"
t0=$(now_ms); container system start --disable-kernel-install --timeout 90 2>&1 | tail -4; t1=$(now_ms)
say "start took $((t1-t0)) ms"
container system status 2>&1 | grep -E 'status' | head -1 | sed 's/^/    /'
snapshot "AFTER start"
say "launchd container jobs: $(launchctl list | grep -c 'apple.container')"

hdr "what survived?"
for c in $R1 $R2 $R3; do
  st=$(container ls -a 2>&1 | awk -v c=$c '$1==c{print $5}')
  say "$c state after restart: ${st:-missing}"
done
hdr "start them again and check data"
for c in $R1 $R2 $R3; do container start $c >/dev/null 2>&1; done
sleep 1
snapshot "AFTER starting the containers again"
say "R1 rootfs file : $(cexec $R1 'cat /root/rootfs.txt' 2>&1 | head -1)"
say "R1 volume file : $(cexec $R1 'cat /data/r1.txt' 2>&1 | head -1)"
say "R1 bind file   : $(cexec $R1 'cat /bind/b.txt' 2>&1 | head -1)"
say "R1 bg process survived? $(cexec $R1 'ls /proc | grep -E "^[0-9]+$" | wc -l' 2>&1) procs now (a fresh container has about 5)"
say "R2 rootfs file : $(cexec $R2 'cat /root/rootfs.txt' 2>&1 | head -1)  | R2 sees volume: $(cexec $R2 'cat /data/r1.txt' 2>&1 | head -1)"
say "R3 rootfs file : $(cexec $R3 'cat /root/rootfs.txt' 2>&1 | head -1)"
say "R3 still on the internal network: $(container inspect $R3 | python3 -c 'import json,sys; print(json.load(sys.stdin)[0]["status"]["networks"][0]["network"])' 2>&1)"
say "internet from R1 (default network works again): $(cexec $R1 'curl -s -o /dev/null -w "%{http_code}" --max-time 8 https://example.com' 2>&1)"
say "internet from R3 (internal network stays blocked): $(cexec $R3 'curl -s -o /dev/null -w "%{http_code}" --max-time 5 https://example.com' 2>&1)"

hdr "the owner's containers and images"
container ls -a 2>&1 | awk '/fedora2|80feef4f/{printf "    %-38.38s %s\n", $1, $5}'
container image ls 2>&1 | awk 'NR>1{printf "    %s:%s\n", $1, $2}' | head -8
cleanup
