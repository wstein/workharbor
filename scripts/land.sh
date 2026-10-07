#!/bin/sh
# Decision step of `make land SHA=<7+ hex>` (#315). The Makefile runs it from
# main's blob (git show refs/heads/main:scripts/land.sh), never from a candidate's
# or the caller's own checkout, so neither can change the decision about itself.
# Usage: land.sh resolve|preview <hex>, or list|next|all.
# inspect <hex> <branch> is the read-only per-branch listing operation.
# Resolves the abbreviated commit id, finds the one local branch whose tip it is,
# checks the review note, derives the path class from the diff against main (never
# from the note), shows it all on stderr and asks the human (a terminal is
# required). On success it prints "<full sha> <branch>" on stdout.
# Accepted limitations (#315): the Makefile that runs this comes from the working
# directory, so run make land from the shared checkout (a candidate worktree's own
# Makefile is the candidate's); and the prompt is a UX safeguard, not a boundary
# against an agent running as the same user.
set -u
export GIT_NO_REPLACE_OBJECTS=1
die() { echo "land: $*" >&2; exit 1; }
command="${1:-}"
case "$command" in
list | next | all)
  # Snapshot branch tips in lexical refname order. Notes index this queue; the
  # exact tip and its note are checked again by resolve before every landing.
  queue="$(git for-each-ref --sort=refname --no-merged=refs/heads/main --format='%(objectname) %(refname:lstrip=2)' refs/heads/)" || die "cannot list the queue"
  selected=0
  while IFS= read -r row <&3; do
    [ -n "$row" ] || continue
    tip="${row%% *}"; branch="${row#* }"
    if [ "$command" = list ]; then
      sh -c "$(git --no-replace-objects show refs/heads/main:scripts/land.sh)" land.sh inspect "$tip" "$branch" || exit 1
      continue
    fi
    # Unstamped branches are visible in land-list but do not enter the queue.
    git notes --ref=review show "$tip" >/dev/null 2>&1 || continue
    [ "$(git rev-parse --verify "refs/heads/$branch^{commit}")" = "$tip" ] || die "queue branch $branch moved: run the queue again"
    selected=1
    # Use phase 1, without BRANCH (which would bypass the review decision).
    env -u MAKEFLAGS -u MFLAGS -u GNUMAKEFLAGS -u MAKEFILES make -s land SHA="$tip" || exit 1
    [ "$command" = next ] && break
  done 3<<EOT
$queue
EOT
  if [ "$command" != list ] && [ "$selected" = 0 ]; then echo "land: no stamped candidates" >&2; fi
  exit 0 ;;
resolve | preview | inspect) ;;
*) die "unknown land.sh command" ;;
esac
x="${2:-}"
case "$x" in "" | *[!0-9a-f]*) die "SHA must be 7 to 40 lowercase hex characters" ;; esac
[ "${#x}" -ge 7 ] && [ "${#x}" -le 40 ] || die "SHA must be 7 to 40 lowercase hex characters"
ids="$(git rev-parse --disambiguate="$x")" || die "cannot look up $x"
[ -n "$ids" ] || die "unknown SHA $x: no such commit"
[ "$(printf '%s\n' "$ids" | wc -l | tr -d ' ')" = 1 ] || die "ambiguous SHA $x: it matches several objects, give more digits"
full="$(git rev-parse --verify -q "$ids^{commit}")" || die "$x is not a commit"
base="$(git rev-parse --verify -q refs/heads/main^{commit})" || die "cannot read main"
branches="$(git for-each-ref --points-at "$full" --format='%(refname:lstrip=2)' refs/heads/)" || die "cannot list the branches"
if [ -z "$branches" ]; then
  if [ -n "$(git for-each-ref --contains "$full" --format=x refs/heads/)" ]; then
    die "$full is not the tip of any local branch: land the branch tip, not an inner commit"
  fi
  die "no local branch has $full as its tip"
fi
if [ "$command" = inspect ]; then
  branches="${3:-}"
  [ "$(git rev-parse --verify "refs/heads/$branches^{commit}")" = "$full" ] || die "queue branch moved"
fi
if [ "$(printf '%s\n' "$branches" | wc -l | tr -d ' ')" != 1 ]; then
  echo "land: several branches have $full as their tip:" >&2
  printf '  %s\n' $branches >&2
  die "say which one is meant: remove the extra branches or use BRANCH=<name> SHA=<40 hex>"
fi
[ "$branches" != main ] || die "$full is main itself: nothing to land"
note="$(git notes --ref=review show "$full" 2>/dev/null)" || {
  [ "$command" = inspect ] || die "$full has no review note (git notes --ref=review): refusing"
  note="(no review note)"
}
at="$(printf '%s\n' "$note" | grep -Eo '(^|[^0-9a-f])at [0-9a-f]{40}([^0-9a-f]|$)' | grep -Eo '[0-9a-f]{40}' | sort -u)"
stamp=matched
if [ "$at" != "$full" ]; then
  stamp=mismatch
  [ "$command" = inspect ] || die "the review note is not for $full (it says: ${at:-no 'at <sha>'}): refusing"
fi
files="$(git -c core.quotepath=off diff --no-renames --name-only -z "$base" "$full" | tr '\0' '\n')" || die "cannot diff against main"
class=ordinary
while IFS= read -r p; do
  [ -n "$p" ] || continue
  lp="$(printf '%s' "$p" | tr 'A-Z' 'a-z')"
  # Ordinary is an allow-list on the lowercased path; carve-outs are matched first.
  case "$lp" in
  agents.md | */agents.md | claude.md | */claude.md | .claude/* | */.claude/* | .agents/* | */.agents/* | .github/*) class=carve-out ;;
  docs/content/*design* | docs/content/*threat*) class=carve-out ;;
  internal/exitcode/* | internal/version/* | internal/docscheck/*) ;;
  docs/*.md) ;;
  readme.md | changelog.md | contributing.md | license) ;;
  *) class=carve-out ;;
  esac
done <<EOT
$files
EOT
{
  echo "land: candidate $full"
  echo "land: branch    $branches"
  echo "land: review stamp: $stamp"
  echo "land: review note:"
  printf '%s\n' "$note" | sed 's/^/  /'
  echo "land: path class: $class (from the changed paths, not from the note)"
  git --no-pager diff --stat "$base" "$full"
} >&2
case "$command" in preview | inspect) exit 0 ;; esac
[ -t 0 ] && [ -t 2 ] || die "not a terminal: run it where the human can answer"
short="$(printf '%s' "$full" | cut -c1-7)"
if [ "$class" = carve-out ]; then
  printf 'land: security-relevant change: type %s to land it: ' "$short" >&2
  read -r ans || die "no answer"
  [ "$ans" = "$short" ] || die "answer does not match $short: not landing"
else
  printf 'land: land %s onto main? [y/N] ' "$short" >&2
  read -r ans || die "no answer"
  case "$ans" in y | Y) ;; *) die "not landing" ;; esac
fi
printf '%s %s\n' "$full" "$branches"
