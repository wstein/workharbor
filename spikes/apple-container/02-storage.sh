#!/usr/bin/env bash
# Item 2: what survives stop, start and rebuild; bind mount versus volume versus rootfs.
set -uo pipefail
. "$(dirname "$0")/lib.sh"
trap cleanup EXIT
BIND=${BIND:?set BIND to a scratch directory on the host}
rm -rf "$BIND" && mkdir -p "$BIND"
VOL=${PREFIX}-vol1
N=${PREFIX}-s1

hdr "volume create"
container volume create $VOL 2>&1 | tail -1
container volume ls | grep -E "NAME|$VOL"
container volume inspect $VOL 2>&1 | head -12

run() { container run -d --name $N --init --cpus 2 --memory 1G \
  --mount type=bind,source="$BIND",target=/bind -v $VOL:/data $IMG sleep 3600 >/dev/null 2>&1; }

hdr "write in rootfs, volume and bind mount"
run
cexec $N 'echo rootfs-1 > /root/r.txt; echo volume-1 > /data/v.txt; echo bind-1 > /bind/b.txt; chmod 600 /bind/b.txt; ls -ld /data /bind; id'
say "host sees bind file: $(cat "$BIND/b.txt" 2>&1) | owner/mode: $(ls -ln "$BIND/b.txt" | awk '{print $1, $3, $4}') | host uid=$(id -u)"

hdr "host writes into the bind mount while the container runs"
echo host-1 > "$BIND/h.txt"
say "guest sees host file: $(cexec $N 'cat /bind/h.txt' 2>&1)"
echo host-2 >> "$BIND/h.txt"
say "guest sees host append: $(cexec $N 'cat /bind/h.txt | tr "\n" " "' 2>&1)"

hdr "stop and start (same container)"
container stop $N >/dev/null 2>&1; container start $N >/dev/null 2>&1
say "rootfs: $(cexec $N 'cat /root/r.txt' 2>&1) | volume: $(cexec $N 'cat /data/v.txt' 2>&1) | bind: $(cexec $N 'cat /bind/b.txt' 2>&1)"

hdr "delete and recreate (rebuild) with the same mounts"
container rm -f $N >/dev/null 2>&1
run
say "rootfs: $(cexec $N 'cat /root/r.txt' 2>&1 | head -1) | volume: $(cexec $N 'cat /data/v.txt' 2>&1) | bind: $(cexec $N 'cat /bind/b.txt' 2>&1)"

hdr "symlink inside the bind mount pointing at a host path (must not reach the host)"
ln -s "$HOME" "$BIND/home-link"
say "guest sees link: $(cexec $N 'ls -ld /bind/home-link; ls /bind/home-link 2>&1 | head -2' 2>&1)"
rm -f "$BIND/home-link"

hdr "I/O: 3000 small files, then 200 MB sequential write (rootfs / volume / bind)"
for target in /root/io /data/io /bind/io; do
  t0=$(now_ms)
  cexec $N "mkdir -p $target && cd $target && for i in \$(seq 1 3000); do echo x > f\$i; done; sync"
  t1=$(now_ms)
  cexec $N "dd if=/dev/zero of=$target/big bs=1M count=200 conv=fsync 2>/dev/null; sync"
  t2=$(now_ms)
  say "$target: small files $((t1-t0)) ms | 200 MB write $((t2-t1)) ms"
done
cexec $N "cd /bind/io && time cat f* > /dev/null" 2>&1 | grep real | sed 's/^/bind read 3000 files: /'
cexec $N "cd /data/io && time cat f* > /dev/null" 2>&1 | grep real | sed 's/^/volume read 3000 files: /'

hdr "disk use"
container system df 2>&1 | head -8
container volume inspect $VOL 2>&1 | grep -iE 'size|source|mount|path' | head -5
