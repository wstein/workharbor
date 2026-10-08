#!/bin/sh
# One command from reviewer verdicts to a READY entry (#406).
# Usage: stack-ready.sh <tip> [--issues "#401 #404"] [--branch <name>] --opus <sha>... --sonnet <sha>...
# The bullet names the one local branch that points at the tip; when several do,
# pass --branch <name> (it must point at the tip), otherwise the script refuses.
# It also refuses (nothing appended) when a note already has a `NOT CLEAR` line (any case, as land.sh reads it).
# The verdicts come only from the arguments; nothing is judged here. For every
# sha it appends the review line `CLEAR <sha> role=review model=<tier>` (the
# format of .agents/review.md, read by scripts/land.sh) unless the note already
# has exactly that line. Then it runs `make land-preview SHA=<tip>` and prints
# the READY bullet for .work/TO_LAND.md (stdout; it edits no file). It never pushes and never lands.
set -u
die() { echo "stack-ready: $*" >&2; exit 1; }
usage() { die "usage: stack-ready.sh <tip> [--issues \"#1 #2\"] [--branch <name>] [--opus <sha>...] [--sonnet <sha>...] (full 40-hex shas)"; }
full_re='^[0-9a-f]{40}$'
is_full() { printf '%s\n' "$1" | grep -Eq "$full_re"; }

[ "$#" -ge 2 ] || usage
tip="$1"; shift
is_full "$tip" || die "tip is not a full 40-hex sha: $tip"
[ "$(git cat-file -t "$tip" 2>/dev/null)" = commit ] || die "tip is not a commit: $tip"

pairs=""
tier=""
issues=""
want_issues=0
want_branch=0
branch=""
opus_shas=""
tiers=""
for arg in "$@"; do
  case "$arg" in
  --opus) [ "$want_issues$want_branch" = 00 ] || usage; tier=opus ;;
  --sonnet) [ "$want_issues$want_branch" = 00 ] || usage; tier=sonnet ;;
  --issues) [ "$want_issues$want_branch" = 00 ] || usage; want_issues=1 ;;
  --branch) [ "$want_issues$want_branch" = 00 ] || usage; want_branch=1 ;;
  -*) usage ;;
  *)
    if [ "$want_issues" = 1 ]; then issues="$arg"; want_issues=0; continue; fi
    if [ "$want_branch" = 1 ]; then branch="$arg"; want_branch=0; continue; fi
    [ -n "$tier" ] || usage
    is_full "$arg" || die "not a full 40-hex sha: $arg"
    [ "$(git cat-file -t "$arg" 2>/dev/null)" = commit ] || die "not a commit: $arg"
    git merge-base --is-ancestor "$arg" "$tip" || die "$arg is not an ancestor of the tip $tip"
    pairs="${pairs}${arg} ${tier}
"
    case " $tiers " in *" $tier "*) ;; *) tiers="${tiers:+$tiers }$tier" ;; esac
    [ "$tier" != opus ] || opus_shas="${opus_shas}${arg} "
    ;;
  esac
done
[ "$want_issues$want_branch" = 00 ] || usage
[ -n "$pairs" ] || usage

at_tip="$(git for-each-ref --points-at "$tip" --format='%(refname:short)' refs/heads)"
if [ -n "$branch" ]; then
  printf '%s\n' "$at_tip" | grep -Fxq -- "$branch" || die "branch $branch does not point at the tip $tip"
else
  case "$at_tip" in
  "") die "no local branch points at the tip $tip; pass --branch <name>" ;;
  *"
"*) die "several local branches point at the tip: $(printf '%s' "$at_tip" | tr '\n' ' '); pass --branch <name>" ;;
  esac
  branch="$at_tip"
fi
printf '%s' "$pairs" | while read -r sha _; do
  if git notes --ref=review show "$sha" 2>/dev/null | grep -Eiq '^[[:space:]]*not[[:space:]]+clear([^0-9a-z]|$)'; then
    echo "stack-ready: $sha has a NOT CLEAR line in its note; refusing to append CLEAR" >&2
    exit 1
  fi
done || exit 1

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

preview="$(make land-preview SHA="$tip" 2>&1)" || { printf '%s\n' "$preview" >&2; die "make land-preview failed for $tip"; }
printf '%s\n' "$preview" >&2

short="$(printf '%s' "$tip" | cut -c1-7)"
n="$(git rev-list --count "refs/heads/main..$tip")" || die "cannot count commits on main"
base="$(git rev-parse --short=7 refs/heads/main)" || die "cannot read main"
carve=""
if printf '%s\n' "$preview" | grep -Eq '^land: path class: carve-out( |$)'; then
  carve=" (carve-out: type the short SHA \`$short\`)"
fi
list=""
[ -z "$issues" ] || list=" ($issues)"
if case " $opus_shas" in *" $tip "*) true ;; *) false ;; esac; then
  claim="Opus CLEAR on the tip and on every changed or hand-merged commit; the rest is patch-identical to CLEAR originals."
else
  claim="No Opus CLEAR on the tip; tiers given: $tiers."
fi
echo "- workharbor STACK on \`$branch\`, tip $short, $n commits on main $base$list. $claim \`make land-preview\` exit 0, verified by the desk$carve:"
echo "  \`cd /Users/werner/workspaces/workharbor/workharbor && make land SHA=$tip\`"
