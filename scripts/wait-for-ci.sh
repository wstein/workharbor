#!/usr/bin/env bash
# Wait for the ci workflow on one commit and succeed only if it passed. The
# release workflow runs it for a tag that was pushed together with main, when CI
# may still be queued or running (#145).
#
#   SHA=<40 hex> REPO=<owner/name> GH_TOKEN=<token> scripts/wait-for-ci.sh
#
# Rule, evaluated at every poll over the push-event runs of ci.yml on exactly
# that SHA (a re-run keeps its run id and shows its latest attempt):
#   - any run not yet completed (queued, in progress, waiting, ...): keep waiting;
#   - otherwise the newest run decides: conclusion success passes, any other
#     conclusion (failure, cancelled, timed_out, ...) fails at once;
#   - no run at all: fail once the grace period has passed;
#   - still waiting when the timeout is reached: fail.
# Time is counted in whole intervals slept, so the limits are exact and the
# tests need no clock. WAIT_INTERVAL_SECONDS (15), WAIT_TIMEOUT_SECONDS (900) and
# NO_RUN_GRACE_SECONDS (60) are digits only; the defaults are the production ones.
set -euo pipefail

interval="${WAIT_INTERVAL_SECONDS:-15}"
timeout="${WAIT_TIMEOUT_SECONDS:-900}"
grace="${NO_RUN_GRACE_SECONDS:-60}"
sha="${SHA:-}"
repo="${REPO:-}"

fail() { echo "::error::$*"; exit 1; }

[[ "$sha" =~ ^[0-9a-f]{40}$ ]] || fail "SHA must be 40 lowercase hex characters"
[[ "$repo" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || fail "REPO must be owner/name"
for v in "$interval" "$timeout" "$grace"; do
  [[ "$v" =~ ^[0-9]{1,6}$ ]] || fail "WAIT_INTERVAL_SECONDS, WAIT_TIMEOUT_SECONDS and NO_RUN_GRACE_SECONDS must be digits only (at most 6)"
done
interval=$((10#$interval)) timeout=$((10#$timeout)) grace=$((10#$grace)) # no octal surprises
[ "$interval" -ge 1 ] || fail "WAIT_INTERVAL_SECONDS must be at least 1"

elapsed=0
while :; do
  # One line per run, newest first: id, status, conclusion ("-" while none), URL.
  runs="$(gh api "repos/$repo/actions/workflows/ci.yml/runs?head_sha=$sha&event=push&per_page=20" \
    -q '.workflow_runs | sort_by(.created_at) | reverse | .[] | [.id, .status, (.conclusion // "-"), .html_url] | @tsv')" \
    || fail "could not list the ci runs of $sha"

  if [ -z "$runs" ]; then
    if [ $((elapsed + interval)) -ge "$grace" ]; then
      fail "no ci run exists for $sha after ${elapsed}s (grace ${grace}s): was CI skipped?"
    fi
    echo "no ci run for $sha yet, waiting (${elapsed}s)"
  else
    pending=""
    while IFS=$'\t' read -r id status _ url; do
      if [ "$status" != completed ]; then pending="$id $status $url"; fi
    done <<<"$runs"
    if [ -n "$pending" ]; then
      echo "ci still running, waiting (${elapsed}s): $pending"
    else
      IFS=$'\t' read -r id _ conclusion url <<<"$(head -n 1 <<<"$runs")"
      if [ "$conclusion" = success ]; then
        echo "ci passed on $sha after ${elapsed}s: run $id $url"
        exit 0
      fi
      fail "ci did not pass on $sha: run $id concluded $conclusion $url"
    fi
  fi

  if [ $((elapsed + interval)) -gt "$timeout" ]; then
    fail "ci did not finish on $sha within ${timeout}s"
  fi
  sleep "$interval"
  elapsed=$((elapsed + interval))
done
