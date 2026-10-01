#!/bin/sh
# Spike #40: git status and build times, bind mount versus volume.
# Usage: run.sh <workdir> ; needs the container CLI, python3, git and a local fedora image.
set -eu
mkdir -p "$1"
W=$(cd "$1" && pwd)
HERE=$(cd "$(dirname "$0")" && pwd)
OUT=$HERE/results.txt
: >"$OUT"
for which in nodemods large; do
  [ -d "$W/$which/repo" ] || python3 "$HERE/gen.py" "$W" "$which"
  echo "# $which: $(find "$W/$which/repo" -path '*/.git' -prune -o -type f -print | wc -l | tr -d ' ') files, $(du -sk "$W/$which/repo" | cut -f1) KiB" | tee -a "$OUT"
  for mode in bind volume; do
    name=whr-spike40-$which-$mode
    container delete --force "$name" >/dev/null 2>&1 || true
    container volume delete "$name-vol" >/dev/null 2>&1 || true
    if [ "$mode" = bind ]; then
      container run -d --name "$name" -c 4 -m 4G -v "$W/$which:/ws" fedora sleep 3600 >/dev/null
    else
      container volume create -s 4G "$name-vol" >/dev/null
      container run -d --name "$name" -c 4 -m 4G -v "$name-vol:/ws" -v "$W/$which:/seed:ro" fedora sleep 3600 >/dev/null
    fi
    container exec "$name" sh -c 'dnf -q -y install git gcc bc tar findutils >/dev/null 2>&1; test -x /usr/bin/git'
    if [ "$mode" = volume ]; then
      s=$(date +%s); container exec "$name" cp -a /seed/repo /ws/repo; e=$(date +%s)
      echo "$which $mode seed_copy $((e - s))" | tee -a "$OUT"
    fi
    container exec -i "$name" sh -c 'cat > /tmp/guest.sh' <"$HERE/guest.sh"
    for run in 1 2; do # run 2 repeats everything with caches warm
      container exec "$name" sh /tmp/guest.sh /ws/repo | sed "s/^/$which $mode run$run /" | tee -a "$OUT"
    done
    container stop "$name" >/dev/null; container delete "$name" >/dev/null
    if [ "$mode" = volume ]; then container volume delete "$name-vol" >/dev/null; fi
  done
done
