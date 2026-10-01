#!/usr/bin/env bash
# Item 1: lifecycle (create, start, stop, delete) and CPU/memory limits.
set -uo pipefail
. "$(dirname "$0")/lib.sh"
trap cleanup EXIT
N=${PREFIX}-l1

hdr "versions"
container --version; sw_vers -productVersion; uname -m

hdr "lifecycle timings (image already local: $IMG)"
timed "run -d (create+start)" container run -d --name $N --cpus 2 --memory 512M $IMG sleep 3600
container ls -a | grep -E "ID|$N"
timed "exec true (ready)" container exec $N true
timed "stop" container stop $N
container ls -a | grep -E "ID|$N"
timed "start" container start $N
timed "exec true after restart" container exec $N true
timed "stop again" container stop $N
timed "rm" container rm $N
container ls -a | grep -c "$N" | sed 's/^/containers named l1 left: /'

hdr "limits as seen by the guest (--cpus 2 --memory 512M)"
container run -d --name $N --cpus 2 --memory 512M $IMG sleep 3600 >/dev/null
cexec $N 'echo "nproc: $(nproc)"; grep MemTotal /proc/meminfo; uname -r; echo "uid: $(id -u)"; cat /etc/os-release | head -2'
hdr "inspect (selected)"
container inspect $N | python3 -c '
import json,sys
d=json.load(sys.stdin)[0]
def walk(o,p=""):
    if isinstance(o,dict):
        for k,v in o.items(): walk(v,p+"."+k)
    elif isinstance(o,list):
        for i,v in enumerate(o): walk(v,p+"[%d]"%i)
    else:
        if any(s in p.lower() for s in ("cpu","memory","status","network","address","image.reference","platform","started","hostname","rootfs","readonly")):
            print(p,"=",o)
' | head -40

hdr "memory limit enforcement: allocate beyond 512M with tail /dev/zero"
cexec $N 'timeout 60 tail /dev/zero; echo "tail exit: $?"'
container ls | grep "$N" | sed 's/^/after OOM test: /'
cexec $N 'echo alive-after-oom'
hdr "dmesg tail (OOM evidence)"
cexec $N 'dmesg 2>/dev/null | tail -5 || echo "no dmesg access"'

hdr "stats"
container stats --no-stream $N 2>&1 | head -4

hdr "cpu limit: 4 busy loops for 4 s on 2 cpus"
cexec $N 'for i in 1 2 3 4; do (timeout 4 sh -c "while :; do :; done" &) ; done; sleep 5; echo done' >/dev/null
cexec $N 'cat /proc/loadavg'
