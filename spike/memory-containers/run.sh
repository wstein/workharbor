#!/bin/sh
# Spike #39: host memory pressure and swap with 0, 1 and 4 agent-sized containers.
# Each container gets -m $MEM and holds $HOLD MiB of touched memory (an agent plus a build).
# Usage: run.sh [mem] [hold-mib]   e.g. run.sh 2G 1200
MEM=${1:-2G}; HOLD=${2:-1200}
HERE=$(cd "$(dirname "$0")" && pwd)
OUT=$HERE/results-$MEM-$HOLD.txt
: >"$OUT"
snap() { # label
  swap=$(sysctl -n vm.swapusage | tr -s ' ' | tr '\n' ' ')
  po=$(vm_stat | awk '/Pageouts/{gsub("\\.","",$2); print $2}')
  free=$(memory_pressure | awk '/free percentage/{print $5}')
  vm=$(ps -axo rss=,comm= | awk '/container-runtime-linux|com.apple.Virtualization.VirtualMachine/{s+=$1} END{printf "%.0f", s/1024}')
  printf '%s | memory_free=%s pageouts=%s | vm_processes_rss_MiB=%s | %s\n' "$1" "$free" "$po" "$vm" "$swap" | tee -a "$OUT"
}
stop_all() { for i in 1 2 3 4; do container delete --force whr-mem$i >/dev/null 2>&1; done; }
stop_all
sleep 5; snap "baseline"
for n in 1 4; do
  for i in $(seq 1 $n); do container run -d --name whr-mem$i -c 2 -m "$MEM" fedora sleep 3600 >/dev/null; done
  sleep 15; snap "$n containers idle"
  stop_all; sleep 10
  # the memory is held by the container's own main process, so it lives as long as the container
  for i in $(seq 1 $n); do
    container run -d --name whr-mem$i -c 2 -m "$MEM" fedora bash -c "x=\$(head -c ${HOLD}M /dev/zero | tr '\\0' a); sleep 3600" >/dev/null
  done
  sleep 40; snap "$n containers holding ${HOLD} MiB each"
  stop_all; sleep 10; snap "after stopping $n"
done
