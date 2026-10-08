#!/bin/sh
# Re-post review statuses on the current heads of a stack and rerun the gate (#424).
# Usage: stack-status.sh --tier sonnet|opus [--apply] [--reviewed <pr>=<base>..<head>]... <pr>...
# <pr>... are the open PR numbers of the stack, bottom first. Dry run by default:
# it prints who it runs as and what it would post and rerun; nothing is written
# without --apply. For each PR it posts review/<tier> (success) on the CURRENT head
# SHA via the REST statuses API, then reruns the latest `gate` run of that head
# (a status post does not start the gate). A still queued or running gate run is
# refused (non-zero exit); every PR is checked before the first post.
# --reviewed <pr>=<base>..<head> names the previously reviewed range of that PR
# (two full 40-hex SHAs, present locally; fetch them first). When the PR head
# differs from <head>, `git range-diff <base>..<head> <newbase>..<newhead>` must
# show every patch as `=`, otherwise the script refuses: a changed patch needs a new
# review. The new range is the PR's baseRefOid..headRefOid. An unchanged head needs
# no proof; a PR without --reviewed is stamped as a fresh review of its head.
# It never merges, never touches the ruleset or a token, and evaluates nothing from
# GitHub output. It posts as the current gh login (the allow-listed identity).
set -u
REPO=wstein/workharbor
die() { echo "stack-status: $*" >&2; exit 1; }
usage() { echo "usage: stack-status.sh --tier sonnet|opus [--apply] [--reviewed <pr>=<base>..<head>]... <pr>..." >&2; exit 2; }
is_pr() { printf '%s\n' "$1" | grep -Eq '^[0-9]+$'; }
is_sha() { printf '%s\n' "$1" | grep -Eq '^[0-9a-f]{40}$'; }

tier=""
apply=0
reviewed=""
prs=""
while [ "$#" -gt 0 ]; do
  case "$1" in
  --tier)
    [ "$#" -ge 2 ] || usage
    tier=$2
    shift 2
    ;;
  --apply) apply=1; shift ;;
  --reviewed)
    [ "$#" -ge 2 ] || usage
    spec=$2
    printf '%s\n' "$spec" | grep -Eq '^[0-9]+=[0-9a-f]{40}\.\.[0-9a-f]{40}$' || die "bad --reviewed (want <pr>=<40hex>..<40hex>): $spec"
    reviewed="${reviewed}${spec}
"
    shift 2
    ;;
  -h | --help) sed -n '2,17p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
  -*) usage ;;
  *)
    is_pr "$1" || die "PR number is not all digits: $1"
    prs="$prs $1"
    shift
    ;;
  esac
done
case "$tier" in sonnet | opus) ;; *) usage ;; esac
[ -n "$prs" ] || usage

me=$(gh api user --jq .login) || die "cannot read the gh login"
echo "stack-status: running as $me; tier review/$tier; $([ "$apply" = 1 ] && echo APPLY || echo 'dry run (pass --apply to post)')"

plan=""
for pr in $prs; do
  line=$(gh pr view "$pr" -R "$REPO" --json state,headRefOid,baseRefOid --jq '[.state,.headRefOid,.baseRefOid]|@tsv') || die "cannot read PR #$pr"
  state=$(printf '%s' "$line" | cut -f1)
  head=$(printf '%s' "$line" | cut -f2)
  base=$(printf '%s' "$line" | cut -f3)
  [ "$state" = OPEN ] || die "PR #$pr is not open ($state)"
  is_sha "$head" || die "PR #$pr: head is not a 40-hex sha"
  is_sha "$base" || die "PR #$pr: base is not a 40-hex sha"

  spec=$(printf '%s' "$reviewed" | grep "^$pr=" | tail -n 1)
  if [ -n "$spec" ]; then
    range=${spec#*=}
    oldbase=${range%%..*}
    oldhead=${range##*..}
    if [ "$oldhead" = "$head" ]; then
      echo "PR #$pr: head $head unchanged, no proof needed"
    else
      for c in $oldbase $oldhead $base $head; do
        [ "$(git cat-file -t "$c" 2>/dev/null)" = commit ] || die "PR #$pr: commit $c is not present locally (fetch it first)"
      done
      rd=$(git range-diff "$oldbase..$oldhead" "$base..$head") || die "PR #$pr: git range-diff failed"
      [ -n "$rd" ] || die "PR #$pr: range-diff is empty, no proof"
      if printf '%s\n' "$rd" | grep -Ev '^ *[0-9-]+: +[0-9a-f-]+ = [0-9-]+: +[0-9a-f-]+ ' | grep -Eq '[^[:space:]]'; then
        printf '%s\n' "$rd" >&2
        die "PR #$pr: patches differ since $oldhead; a changed patch needs a new review"
      fi
      echo "PR #$pr: range-diff all '=' ($oldhead -> $head)"
    fi
  else
    echo "PR #$pr: no --reviewed given, stamping head $head as a fresh review"
  fi

  run=$(gh run list -R "$REPO" --workflow gate --commit "$head" --limit 1 --json databaseId,status --jq '.[0] | [.databaseId,.status] | @tsv') || die "PR #$pr: cannot list gate runs"
  runid=$(printf '%s' "$run" | cut -f1)
  rstatus=$(printf '%s' "$run" | cut -f2)
  is_pr "$runid" || die "PR #$pr: no gate run found for $head"
  [ "$rstatus" = completed ] || die "PR #$pr: gate run $runid is $rstatus; wait for it to finish (a running run cannot be rerun)"
  plan="${plan}${pr} ${head} ${runid}
"
done

printf '%s' "$plan" | while read -r pr head runid; do
  desc="review/$tier re-posted on the current head by stack-status.sh"
  if [ "$apply" = 1 ]; then
    gh api "repos/$REPO/statuses/$head" -f state=success -f "context=review/$tier" -f "description=$desc" >/dev/null || die "PR #$pr: status post failed"
    gh run rerun -R "$REPO" "$runid" || die "PR #$pr: rerun of $runid failed"
    echo "PR #$pr: posted review/$tier on $head, reran gate run $runid"
  else
    echo "would post review/$tier success on PR #$pr head $head, then rerun gate run $runid"
  fi
done
