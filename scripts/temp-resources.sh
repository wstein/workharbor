#!/usr/bin/env bash
# List, or with --delete remove, the temporary Apple Container resources that
# spikes, live tests, verification and debugging made (AGENTS.md, Temporary
# containers): containers, volumes and networks labelled workharbor.temp=true,
# and images named whtmp/*. Resources of a running supervisor (labelled by its
# owner) are never touched, because they do not carry the temp label.
#
#   scripts/temp-resources.sh [--lane <lane>] [--delete]
set -euo pipefail

lane=""
delete=0
while [ $# -gt 0 ]; do
  case "$1" in
    --lane) lane="${2:-}"; shift 2 ;;
    --delete) delete=1; shift ;;
    *) echo "usage: $0 [--lane <lane>] [--delete]" >&2; exit 2 ;;
  esac
done
command -v container >/dev/null || { echo "temp-resources: no container CLI" >&2; exit 1; }

# pick reads `container <kind> ls --format json` on stdin and prints the IDs of
# entries labelled workharbor.temp=true (and the lane, when given).
pick() {
  LANE="$lane" python3 -c '
import json, os, sys
lane = os.environ["LANE"]
for e in json.load(sys.stdin) or []:
    c = e.get("configuration", e)
    labels = c.get("labels") or e.get("labels") or {}
    if labels.get("workharbor.temp") != "true":
        continue
    if lane and labels.get("workharbor.lane") != lane:
        continue
    print(e.get("id") or c.get("id") or c.get("name"))
'
}

containers=$(container ls --all --format json | pick)
volumes=$(container volume ls --format json | pick)
networks=$(container network ls --format json | pick)
images=$(container image ls --format json | python3 -c '
import json, sys
for e in json.load(sys.stdin) or []:
    n = e.get("configuration", {}).get("name", "")
    if n.split("/")[-2:-1] == ["whtmp"] or n.startswith("whtmp/"):
        print(n)
')
[ -n "$lane" ] && images="" # images carry no lane label; clean them without --lane

show() { [ -n "$2" ] && printf '%s\n' "$2" | sed "s/^/$1 /"; return 0; }
show container "$containers"
show volume "$volumes"
show network "$networks"
show image "$images"
if [ -z "$containers$volumes$networks$images" ]; then
  echo "temp-resources: nothing to clean" >&2
  exit 0
fi
[ "$delete" = 1 ] || { echo "temp-resources: listed only; run with --delete to remove them" >&2; exit 0; }

for c in $containers; do container stop "$c" >/dev/null 2>&1 || true; container delete "$c" >/dev/null 2>&1 || true; done
for v in $volumes; do container volume delete "$v" >/dev/null; done
for n in $networks; do container network delete "$n" >/dev/null; done
for i in $images; do container image delete "$i" >/dev/null; done
echo "temp-resources: removed what was listed" >&2
