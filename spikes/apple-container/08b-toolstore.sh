#!/usr/bin/env bash
# Item 8: stock images plus a shared read-only tool store (NixOS-like).
set -uo pipefail
. "$(dirname "$0")/lib.sh"
STORE=${STORE:?set STORE to the tool store built by build-store.sh}
PULLED=${PULLED:-$STORE/../pulled-images.txt}
trap cleanup EXIT
mnt="--mount type=bind,source=$STORE,target=/opt/store,readonly"
BIN=/opt/store/profiles
IMAGES="docker.io/library/fedora:latest docker.io/library/debian:bookworm-slim docker.io/library/ubuntu:24.04 docker.io/library/alpine:3"

hdr "stock images (pull the ones that are missing, remember them for removal)"
container image ls --quiet 2>/dev/null | sort > "$STORE/../images-before.txt"
for i in $IMAGES; do
  if container image ls 2>/dev/null | awk '{print $1":"$2}' | grep -q "^${i#docker.io/library/}$"; then say "present: $i"; else
    container image pull "$i" >/dev/null 2>&1 && { say "pulled:  $i"; echo "$i" >> "$PULLED"; }
  fi
done
container image ls | grep -E 'NAME|fedora|debian|ubuntu|alpine'

hdr "does the same store run in each stock image? (profile default: glibc claude + musl codex; profile musl: musl claude)"
i=0
for img in $IMAGES; do
  i=$((i+1)); N=${PREFIX}-m$i
  container run -d --name $N --init $mnt $img sleep 600 >/dev/null 2>&1
  libc=$(container exec $N sh -c 'ls /lib/ld-musl* /lib/aarch64-linux-gnu/libc.so.6 /lib64/libc.so.6 2>/dev/null | head -1')
  say "--- ${img#docker.io/library/}  (libc: ${libc:-?})"
  for prof_tool in default/claude default/codex musl/claude; do
    out=$(container exec $N sh -c "$BIN/${prof_tool%/*}/bin/${prof_tool#*/} --version 2>&1 | head -2" 2>&1 | head -2 | tr '\n' ' ')
    printf '    %-15s %s\n' "$prof_tool" "${out:0:100}"
  done
  container rm -f $N >/dev/null 2>&1
done

F=docker.io/library/fedora:latest
hdr "startup cost of claude --version on fedora: bind mount vs rootfs copy vs ext4 volume"
N=${PREFIX}-p1
container run -d --name $N --init $mnt $F sleep 900 >/dev/null 2>&1
bench() { local label=$1 cmd=$2 t0 t1 n=0 out=""; for k in 1 2 3; do t0=$(now_ms); container exec $N sh -c "$cmd" >/dev/null 2>&1; t1=$(now_ms); out="$out $((t1-t0))"; done; say "$label: runs (ms):$out"; }
bench "bind mount (virtiofs), ro" "$BIN/default/bin/claude --version"
t0=$(now_ms); container exec $N sh -c "cp -L $BIN/default/bin/claude /tmp/claude-copy" ; t1=$(now_ms)
say "copy 230 MB into the rootfs: $((t1-t0)) ms"
bench "rootfs copy" "/tmp/claude-copy --version"
container rm -f $N >/dev/null 2>&1

VOL=${PREFIX}-tools
container volume create $VOL >/dev/null 2>&1
t0=$(now_ms)
container run --rm --name ${PREFIX}-pop --init $mnt -v $VOL:/dst $F sh -c 'cp -a /opt/store/. /dst/ && sync' >/dev/null 2>&1
t1=$(now_ms); say "populate a volume from the store (949 MB): $((t1-t0)) ms"
container run -d --name $N --init -v $VOL:/opt/store:ro $F sleep 900 >/dev/null 2>&1 \
  && say "volume mounted read-only: ok" || { say "volume ':ro' syntax failed, trying without"; container run -d --name $N --init -v $VOL:/opt/store $F sleep 900 >/dev/null 2>&1; }
bench "ext4 volume" "$BIN/default/bin/claude --version"
say "volume read-only enforced? $(container exec $N sh -c 'touch /opt/store/x 2>&1 | head -1')"

hdr "one volume attached to two running containers at once"
N2=${PREFIX}-p2
container run -d --name $N2 --init -v $VOL:/opt/store:ro $F sleep 900 2>&1 | tail -1
say "second container sees the store: $(container exec $N2 sh -c "$BIN/default/bin/claude --version" 2>&1 | head -1)"
say "first container still fine:      $(container exec $N sh -c "$BIN/default/bin/claude --version" 2>&1 | head -1)"
container rm -f $N $N2 >/dev/null 2>&1

hdr "immutability of the bind-mounted store from inside"
N=${PREFIX}-i1
container run -d --name $N --init $mnt $F sleep 900 >/dev/null 2>&1
container exec $N sh -c 'touch /opt/store/x 2>&1 | head -1; rm -f /opt/store/profiles/default/bin/claude 2>&1 | head -1; echo x >> /opt/store/profiles/default/bin/claude 2>&1 | head -1; chmod 777 /opt/store/store 2>&1 | head -1; ln -sfn /bin/sh /opt/store/profiles/default/bin/claude 2>&1 | head -1'
say "host store untouched: $(ls -l "$STORE/profiles/default/bin/claude" | awk '{print $9,$10,$11}' | sed "s|$STORE/||")"
container rm -f $N >/dev/null 2>&1

hdr "four concurrent containers sharing the one store"
t0=$(now_ms)
for k in 1 2 3 4; do
  ( container run --rm --name ${PREFIX}-c$k --init $mnt $F sh -c "$BIN/default/bin/claude --version; $BIN/default/bin/codex --version" > "$STORE/../conc$k.out" 2>/dev/null ) &
done; wait
t1=$(now_ms); say "4 containers start, run both tools and exit: $((t1-t0)) ms total"
cat "$STORE"/../conc*.out | sort | uniq -c | sed -E 's/^ +/  /'; rm -f "$STORE"/../conc*.out

hdr "two versions side by side in different environments, separate writable homes"
A=${PREFIX}-v1; B=${PREFIX}-v2
container volume create ${PREFIX}-h1 >/dev/null 2>&1; container volume create ${PREFIX}-h2 >/dev/null 2>&1
container run -d --name $A --init $mnt -v ${PREFIX}-h1:/home/agent -e HOME=/home/agent -e PATH=$BIN/default/bin:/usr/bin:/bin $F sleep 900 >/dev/null 2>&1
container run -d --name $B --init $mnt -v ${PREFIX}-h2:/home/agent -e HOME=/home/agent -e PATH=$BIN/pinned/bin:/usr/bin:/bin $F sleep 900 >/dev/null 2>&1
say "env A (profile default): $(container exec $A claude --version 2>&1 | head -1)"
say "env B (profile pinned) : $(container exec $B claude --version 2>&1 | head -1)"
say "which claude in A: $(container exec $A sh -c 'readlink -f $(command -v claude)' 2>&1)"
say "which claude in B: $(container exec $B sh -c 'readlink -f $(command -v claude)' 2>&1)"
container exec $A sh -c 'echo A-only > /home/agent/who' ; container exec $B sh -c 'echo B-only > /home/agent/who'
say "homes are separate: A=$(container exec $A cat /home/agent/who) B=$(container exec $B cat /home/agent/who)"
container exec $A sh -c 'cd /tmp && echo "hi" | timeout 30 claude -p --model haiku 2>&1 | head -2'
say "claude wrote its state into the home, not the store: $(container exec $A sh -c 'ls -A /home/agent | tr "\n" " "')"
hdr "store integrity after all of the above (re-hash each binary against its content address)"
bad=0
for d in "$STORE"/store/*; do
  f=$(ls "$d"/bin/* | head -1); want=$(basename "$d" | cut -d- -f1); got=$(shasum -a 256 "$f" | cut -c1-8)
  # the claude entries are addressed by the vendor checksum of the download; codex by the extracted binary
  [ "$got" = "$want" ] && say "ok   $(basename "$d")" || { say "DIFF $(basename "$d") (hash $got)"; bad=1; }
done
say "store integrity: $([ $bad = 0 ] && echo unchanged || echo CHANGED)"
