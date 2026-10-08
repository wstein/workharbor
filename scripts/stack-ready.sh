#!/bin/sh
# One command from reviewer verdicts to a READY entry (#406).
# Usage: stack-ready.sh <tip> --opus <sha>... --sonnet <sha>...
# The verdicts come only from the arguments; nothing is judged here. For every
# sha it appends the review line `CLEAR <sha> role=review model=<tier>` (the
# format of .agents/review.md, read by scripts/land.sh) unless the note already
# has exactly that line. Then it runs `make land-preview SHA=<tip>` and prints
# the READY block for .work/TO_LAND.md. It never pushes and never lands.
set -u
die() { echo "stack-ready: $*" >&2; exit 1; }
usage() { die "usage: stack-ready.sh <tip> [--opus <sha>...] [--sonnet <sha>...] (full 40-hex shas)"; }
full_re='^[0-9a-f]{40}$'
is_full() { printf '%s\n' "$1" | grep -Eq "$full_re"; }

[ "$#" -ge 2 ] || usage
tip="$1"; shift
is_full "$tip" || die "tip is not a full 40-hex sha: $tip"
[ "$(git cat-file -t "$tip" 2>/dev/null)" = commit ] || die "tip is not a commit: $tip"

pairs=""
tier=""
for arg in "$@"; do
  case "$arg" in
  --opus) tier=opus ;;
  --sonnet) tier=sonnet ;;
  -*) usage ;;
  *)
    [ -n "$tier" ] || usage
    is_full "$arg" || die "not a full 40-hex sha: $arg"
    [ "$(git cat-file -t "$arg" 2>/dev/null)" = commit ] || die "not a commit: $arg"
    git merge-base --is-ancestor "$arg" "$tip" || die "$arg is not an ancestor of the tip $tip"
    pairs="${pairs}${arg} ${tier}
"
    ;;
  esac
done
[ -n "$pairs" ] || usage

# All arguments are valid: only now write notes.
printf '%s' "$pairs" | while read -r sha model; do
  line="CLEAR $sha role=review model=$model"
  if git notes --ref=review show "$sha" 2>/dev/null | grep -Fxq "$line"; then
    echo "stack-ready: note present: $line" >&2
  else
    git notes --ref=review append -m "$line" "$sha" || die "cannot append the review note for $sha"
    echo "stack-ready: note added: $line" >&2
  fi
done || exit 1

make land-preview SHA="$tip" || die "make land-preview failed for $tip"

branch="$(git for-each-ref --points-at="$tip" --format='%(refname:lstrip=2)' refs/heads/ | head -n 1)"
short="$(printf '%s' "$tip" | cut -c1-7)"
echo "## READY entry for .work/TO_LAND.md"
echo "- ${branch:-<branch>} $tip: make land SHA=$short"
