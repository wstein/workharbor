#!/bin/sh
# Report the index state of a checkout without writing its index (make land).
# Usage: index-state.sh <checkout>
# Exit 0: the index equals HEAD. Exit 3: stale index, every path that differs
# from HEAD has a working file equal to HEAD (a leftover of an earlier land).
# Exit 4: real staged work (some differing path's file differs from HEAD).
set -u
dir="${1:?usage: index-state.sh <checkout>}"
g() { git --no-optional-locks -C "$dir" "$@"; }
paths="$(g diff-index --cached --name-only -z HEAD | tr '\0' '\n')" || exit 2
[ -n "$paths" ] || exit 0
stale=1
while IFS= read -r p; do
  want="$(g rev-parse -q --verify "HEAD:$p" 2>/dev/null || true)"
  have=""
  if [ -f "$dir/$p" ]; then have="$(g hash-object -- "$p")"; fi
  [ "$want" = "$have" ] || stale=0
done <<EOT
$paths
EOT
if [ "$stale" = 1 ]; then exit 3; fi
exit 4
