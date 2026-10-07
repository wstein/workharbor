#!/bin/sh
# Decision step of `make land SHA=<7+ hex>` (#315). The Makefile runs it from
# main's blob (git show refs/heads/main:scripts/land.sh), never from a candidate's
# or the caller's own checkout, so neither can change the decision about itself.
# Usage: land.sh resolve|preview <hex>, or wizard|list|next|all.
# inspect <hex> <branch> is the read-only per-branch listing operation.
# record <sha> <branch> <mode> <answer> <at> <review-object> <base> <class> <generated>
# writes a local confirmation note after the fast-forward.
# Resolves the abbreviated commit id, finds the one local branch whose tip it is,
# checks the review note, derives the path class from the diff against main (never
# from the note), shows it all on stderr and asks the human (a terminal is
# required). On success stdout carries the candidate, branch and confirmation
# fields for the Makefile to retain through the checks.
# Accepted limitations (#315): the Makefile that runs this comes from the working
# directory, so run make land from the shared checkout (a candidate worktree's own
# Makefile is the candidate's); and the prompt is a UX safeguard, not a boundary
# against an agent running as the same user.
set -u
export GIT_NO_REPLACE_OBJECTS=1
die() { echo "land: $*" >&2; exit 1; }
# Match design §7.1 / internal/textsafe for every display field (branch names, review
# notes, titles). Raw values stay in variables for Git; only display copies change.
# "sanitize_display text" keeps newlines for multi-line text.
sanitize_display() {
  command -v python3 >/dev/null 2>&1 || {
    if [ "$#" -gt 0 ]; then LC_ALL=C tr -c '[:print:]\t\n' '?'; else LC_ALL=C tr -c '[:print:]\t' '?'; fi
    return
  }
  python3 -I -c '
import sys
text = sys.stdin.buffer.read().decode("utf-8", "replace")
keep = {9, 10} if len(sys.argv) > 1 else {9}
def unsafe(c):
    n = ord(c)
    return ((n < 0x20 and n not in keep) or 0x7f <= n <= 0x9f or
            0x202a <= n <= 0x202e or 0x2066 <= n <= 0x2069 or
            n in (0x061c, 0x200e, 0x200f, 0x2028, 0x2029))
sys.stdout.buffer.write("".join("?" if unsafe(c) else c for c in text).encode("utf-8"))
' "$@"
}
command="${1:-}"
case "$command" in
wizard)
  [ -t 0 ] && [ -t 2 ] || die "not a terminal: run make land in a terminal; use make land-list or make land-preview SHA=<sha> to inspect candidates"
  shared="$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")" || die "cannot find the shared checkout"
  [ "$(git -C "$shared" symbolic-ref -q HEAD)" = refs/heads/main ] || die "the shared checkout is not on main: ask the human to restore main there, then run make land again; no checkout was switched"
  here="$(git rev-parse --show-toplevel)" || die "run make land from a checkout"
  if [ "$here" != "$shared" ]; then
    echo "land: you are in a worktree; landing uses the shared checkout's current-main recipe." >&2
    printf 'land: use the shared checkout for this run? [y/N] ' >&2
    read -r answer || die "no answer: run make land from the shared checkout"
    case "$answer" in y | Y) cd "$shared" || die "cannot enter the shared checkout" ;;
      *) die "cancelled: run make land from the shared checkout when ready; no checkout was switched" ;; esac
  fi
  lsh="$(git --no-replace-objects show refs/heads/main:scripts/land.sh)" || die "cannot read main's resolver"
  queue="$(git for-each-ref --sort=refname --no-merged=refs/heads/main --format='%(objectname) %(refname:lstrip=2)' refs/heads/)" || die "cannot list candidates"
  candidates=""; display_candidates=""; count=0
  while IFS= read -r row; do
    [ -n "$row" ] || continue
    tip="${row%% *}"; branch="${row#* }"
    git notes --ref=review show "$tip" >/dev/null 2>&1 || continue
    count=$((count + 1))
    candidates="${candidates}${count} ${row}
"
    title="$(git --no-pager show -s --format=%s "$tip")" || die "cannot read candidate title"
    title="$(printf '%s' "$title" | sanitize_display)" || die "cannot sanitize candidate title: python3 is required"
    display_branch="$(printf '%s' "$branch" | sanitize_display)" || die "cannot sanitize candidate branch: python3 is required"
    display_candidates="${display_candidates}${count} ${tip} ${display_branch}
"
    printf '\nland: %s) %s — %s\n' "$count" "$display_branch" "$title" >&2
    sh -c "$lsh" land.sh inspect "$tip" "$branch" || exit 1
  done <<EOT
$queue
EOT
  [ "$count" -gt 0 ] || die "no stamped candidates: obtain an independent review of the exact branch tip, then run make land again"
  echo "land: checks run before landing: check-local, commitlint, consumer regressions, secrets-range; generated files when changed." >&2
  echo "land: a matching review stamp is required; a displayed mismatch cannot land." >&2
  printf 'land: choose a candidate number (q cancels' >&2
  if command -v fzf >/dev/null 2>&1; then printf ', f opens fzf' >&2; fi
  printf '): ' >&2
  read -r choice || die "no selection: run make land again"
  case "$choice" in q | Q | "") die "cancelled: no candidate selected" ;;
    f) command -v fzf >/dev/null 2>&1 || die "fzf is unavailable: run make land again and choose a number"
      selected="$(printf '%s' "$display_candidates" | fzf --prompt='Candidate> ')" || die "cancelled: no candidate selected"
      choice="${selected%% *}" ;; esac
  case "$choice" in *[!0-9]* | "" | 0*) die "invalid selection: run make land again and choose a displayed number" ;; esac
  row="$(printf '%s' "$candidates" | awk -v n="$choice" '$1 == n {print; exit}')"
  [ -n "$row" ] || die "invalid selection: run make land again and choose a displayed number"
  row="${row#* }"; tip="${row%% *}"; branch="${row#* }"
  [ "$(git rev-parse --verify "refs/heads/$branch^{commit}")" = "$tip" ] || die "selected SHA is stale: the branch moved; obtain review of its new tip and run make land again"
  display_branch="$(printf '%s' "$branch" | sanitize_display)" || die "cannot sanitize candidate branch: python3 is required"
  git merge-base --is-ancestor refs/heads/main "$tip" || die "$display_branch is not on top of main: in its worktree run git rebase main, obtain review of the new SHA, then run make land again"
  echo "land: selected candidate summary; confirmation follows." >&2
  # Do not pass BRANCH: the short form retains the review and human-answer gate.
  /usr/bin/env -u MAKEFLAGS -u MFLAGS -u GNUMAKEFLAGS -u MAKEFILES make -s land SHA="$tip" || exit 1
  echo "land: landed the selected candidate on local main. Push only after all intended commits are reviewed: git push origin main" >&2
  exit 0 ;;
record)
  [ "$#" = 10 ] || die "record requires the retained confirmation fields"
  full="$2"; branch="$3"; mode="$4"; answer="$5"; at="$6"
  review="$7"; base="$8"; class="$9"; shift 9; generated="$1"
  [ "$(git rev-parse --verify refs/heads/main^{commit})" = "$full" ] || die "main is not the confirmed commit: no confirmation recorded"
  git check-ref-format "refs/heads/$branch" || die "invalid confirmation branch"
  # The resolver and this writer are the same blob captured from main before
  # landing. Never execute a candidate's writer after the fast-forward.
  # assurance local is an honest log, not proof against same-user agents.
  record="$(python3 -I - "$full" "$branch" "$mode" "$answer" "$at" "$review" "$base" "$class" "$generated" <<'PYRECORD'
import datetime
import json
import re
import sys

full, branch, mode, answer, at, review, base, path_class, generated = sys.argv[1:]
for obj in (full, review, base):
    if not re.fullmatch(r"[0-9a-f]{40}", obj):
        sys.exit("land: invalid confirmation object")
branch.encode("utf-8")  # Reject invalid UTF-8 refnames rather than emit surrogates.
if path_class == "carve-out":
    valid_answer = mode == "typed_sha" and answer == full[:7]
elif path_class == "ordinary":
    valid_answer = mode == "yn" and answer == "yes"
else:
    valid_answer = False
if not valid_answer or generated not in ("0", "1"):
    sys.exit("land: invalid retained confirmation")
if datetime.datetime.strptime(at, "%Y-%m-%dT%H:%M:%SZ").strftime("%Y-%m-%dT%H:%M:%SZ") != at:
    sys.exit("land: invalid confirmation time")
checks = ["check-local", "commitlint", "test-commitlint-consumers", "secrets-range"]
if generated == "1":
    checks.append("check-generated")
evidence = [{"kind": "base", "object": base},
            {"kind": "path-class", "value": path_class},
            {"kind": "review-note", "object": review, "ref": "refs/notes/review"}]
evidence += [{"kind": "check", "name": name, "result": "pass"} for name in checks]
# Only validated refnames and fixed ASCII fields enter this encoder. Refnames
# cannot contain ASCII controls, whose json.dumps short escapes would differ
# from v1. Do not add free text here without using the v1 control escaping.
def canonical(value):
    return json.dumps(value, sort_keys=True, ensure_ascii=True, separators=(",", ":"))
evidence.sort(key=canonical)
record = {
    "v": 1, "schema": "workharbor.confirmation", "action": "land",
    "subject": {"commit": full, "branch": branch},
    "answer": {"mode": mode, "value": answer}, "at": at,
    "channel": "cli", "by": "human", "assurance": "local",
    "evidence": evidence,
}
print(canonical(record), end="")
PYRECORD
)" || die "cannot encode the confirmation"
  # No -f: an existing record is never silently overwritten; a byte-identical one
  # (a rerun after a partial failure) is accepted.
  existing="$(git notes --ref=confirm list "$full" 2>/dev/null)" || existing=""
  if [ -n "$existing" ]; then
    [ "$(git cat-file blob "$existing" 2>/dev/null)" = "$record" ] && exit 0
    die "a different confirmation note already exists"
  fi
  printf '%s' "$record" | git notes --ref=confirm add -F - "$full" || die "cannot write the confirmation note"
  exit 0 ;;
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
    [ "$(git rev-parse --verify "refs/heads/$branch^{commit}")" = "$tip" ] || die "queue branch $(printf '%s' "$branch" | sanitize_display) moved: run the queue again"
    selected=1
    # Use phase 1, without BRANCH (which would bypass the review decision).
    /usr/bin/env -u MAKEFLAGS -u MFLAGS -u GNUMAKEFLAGS -u MAKEFILES make -s land SHA="$tip" || exit 1
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
# The branch named exactly `landing` is excluded from the "several branches" check; when only `landing` points at the SHA it is the branch (#365).
others="$(printf '%s\n' "$branches" | grep -vx landing || true)"
[ -z "$others" ] || branches="$others"
if [ "$command" = inspect ]; then
  branches="${3:-}"
  [ "$(git rev-parse --verify "refs/heads/$branches^{commit}")" = "$full" ] || die "queue branch moved"
fi
if [ "$(printf '%s\n' "$branches" | wc -l | tr -d ' ')" != 1 ]; then
  echo "land: several branches have $full as their tip:" >&2
  printf '  %s\n' $branches | sanitize_display text >&2
  die "say which one is meant: remove the extra branches or use BRANCH=<name> SHA=<40 hex>"
fi
[ "$branches" != main ] || die "$full is main itself: nothing to land"
review="$(git notes --ref=review list "$full" 2>/dev/null)" || review=""
note="$(git cat-file blob "$review" 2>/dev/null)" || {
  [ "$command" = inspect ] || die "$full has no review note (git notes --ref=review): refusing"
  note="(no review note)"
}
# Review lines (crewbook#67): <CLEAR|NOT CLEAR> <full sha> role=<role> model=<model>, one per line.
note="$(printf '%s\n' "$note" | sed "s/$(printf '\r')\$//")"
clear="$(printf '%s\n' "$note" | grep -E "^CLEAR $full role=[^ ]+ model=[^ ]+\$" || true)"
# A refusal is any line starting with NOT CLEAR (any case, any blank run, optional
# leading blanks) that names this sha or no full sha at all; malformed variants count.
notclear_in() { # $1 = sha, stdin = note
  nl="$(grep -Ei '^[[:space:]]*not[[:space:]]+clear([^0-9a-z]|$)' || true)"
  [ -n "$nl" ] || return 1
  printf '%s\n' "$nl" | grep -Fiq "$1" || printf '%s\n' "$nl" | grep -Evq '[0-9a-fA-F]{40}'
}
notclear=""
! printf '%s\n' "$note" | notclear_in "$full" || notclear=yes
stamp=matched
if [ -z "$clear" ]; then
  stamp=mismatch
  [ "$command" = inspect ] || die "the review note is not for $full (it has no 'CLEAR $full role=<role> model=<model>' line): refusing"
elif [ -n "$notclear" ]; then
  stamp=mismatch
  [ "$command" = inspect ] || die "the review note says NOT CLEAR for $full: refusing"
fi
if [ "$command" = resolve ]; then
  # Refuse before anything lands: the record step runs after the fast-forward and
  # must not meet an existing note or a locked notes ref there.
  [ -z "$(git notes --ref=confirm list "$full" 2>/dev/null)" ] || die "$full already has a confirmation note (git notes --ref=confirm): refusing before main moves"
  cgit="$(git rev-parse --path-format=absolute --git-common-dir)" || die "cannot find the git directory"
  [ ! -e "$cgit/refs/notes/confirm.lock" ] || die "the confirmation notes ref is locked ($cgit/refs/notes/confirm.lock): refusing before main moves"
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
# Landing order and patch-id inheritance (#365). A commit is covered when it has
# its own CLEAR of the needed tier (Opus for a carve-out, any model otherwise) or its
# verbatim patch-id (whitespace counts) equals that of another noted commit with such
# a CLEAR. No model is involved. A commit with its own NOT CLEAR line, or one equal to
# an original whose note says NOT CLEAR, is never covered. A tip with a CLEAR of the
# needed tier is covered entirely: a linear tip contains its earlier commits, also on
# `landing`. A carve-out tip with a lower tier (Sonnet) passes only when EVERY commit
# of main..tip is covered; otherwise it needs Opus (on `landing`: landing order).
need=any
[ "$class" = carve-out ] && need=opus
cr="$(printf '\r')"
opus_re=' model=(claude-)?opus(-[0-9]+([.-][0-9]+)*)?$'
body_of() {
  n="$(git notes --ref=review list "$1" 2>/dev/null)" || return 1
  git cat-file blob "$n" 2>/dev/null | sed "s/$cr\$//"
}
clear_at() { # callers refuse a NOT CLEAR commit first (covered)
  b="$(body_of "$1")" || return 1
  l="$(printf '%s\n' "$b" | grep -E "^CLEAR $1 role=[^ ]+ model=[^ ]+\$")" || return 1
  [ "$need" = any ] || printf '%s\n' "$l" | grep -Eq "$opus_re"
}
notclear_at() { body_of "$1" | notclear_in "$1"; }
pid() { git show --format= --full-index --binary "$1" | git patch-id --verbatim | cut -d' ' -f1; }
covered() {
  notclear_at "$1" && return 1
  clear_at "$1" && return 0
  p="$(pid "$1")"; [ -n "$p" ] || return 1
  hit=""
  while read -r q o; do
    [ "$q" = "$p" ] && [ "$o" != "$1" ] || continue
    notclear_at "$o" && return 1
    [ -n "$hit" ] || { clear_at "$o" && hit="$o"; }
  done <<EOT
$noted_ids
EOT
  [ -n "$hit" ] || return 1
  covby="$covby covered by $(printf '%s' "$hit" | cut -c1-7) (patch-id): $(printf '%s' "$1" | cut -c1-7)
"
  return 0
}
# allcov: every commit of main..tip is covered. Each noted commit's patch-id is
# computed once.
covby=""
allcov() {
  noted_ids=""
  for o in $(git notes --ref=review list 2>/dev/null | cut -d' ' -f2); do
    [ "$(git cat-file -t "$o" 2>/dev/null)" = commit ] || continue
    noted_ids="$noted_ids$(pid "$o") $o
"
  done
  revs="$(git rev-list "$base..$full")" || return 1
  for c in $revs; do covered "$c" || return 1; done
}
if [ "$stamp" = matched ] && [ "$class" = carve-out ] && ! printf '%s\n' "$clear" | grep -Eq "$opus_re" && ! allcov; then
  stamp=mismatch
  if git rev-parse -q --verify refs/heads/landing >/dev/null && git merge-base --is-ancestor "$full" refs/heads/landing; then
    [ "$command" = inspect ] || die "$full is on landing, but a commit of main..$full has no required CLEAR or equivalent original (landing order): refusing"
  else
    [ "$command" = inspect ] || die "security-relevant change: no CLEAR line from an Opus model: refusing"
  fi
fi
{
  echo "land: candidate $full"
  echo "land: branch    $(printf '%s' "$branches" | sanitize_display)"
  echo "land: review stamp: $stamp"
  echo "land: review note:"
  printf '%s\n' "$note" | sanitize_display text | sed 's/^/  /'
  echo "land: path class: $class (from the changed paths, not from the note)"
  [ -z "$covby" ] || printf '%s' "$covby" | sed 's/^/land: /' | sanitize_display text
  git -c core.quotepath=on --no-pager diff --stat "$base" "$full" | sanitize_display text
} >&2
case "$command" in preview | inspect) exit 0 ;; esac
command -v python3 >/dev/null 2>&1 || die "python3 is required to record the confirmation"
[ -t 0 ] && [ -t 2 ] || die "not a terminal: run it where the human can answer"
short="$(printf '%s' "$full" | cut -c1-7)"
if [ "$class" = carve-out ]; then
  printf 'land: security-relevant change: type %s to land it: ' "$short" >&2
  read -r ans || die "no answer"
  [ "$ans" = "$short" ] || die "answer does not match $short: not landing"
  mode=typed_sha; answer="$ans"
else
  printf 'land: land %s onto main? [y/N] ' "$short" >&2
  read -r ans || die "no answer"
  case "$ans" in y | Y) ;; *) die "not landing" ;; esac
  mode=yn; answer=yes
fi
at="$(date -u +%Y-%m-%dT%H:%M:%SZ)" || die "cannot read the confirmation time"
printf '%s %s %s %s %s %s %s %s\n' "$full" "$branches" "$mode" "$answer" "$at" "$review" "$base" "$class"
