#!/usr/bin/env bash
# Wait until a commit is reachable from origin/main. The release workflow runs it
# for a tag that was pushed just before (or together with) main, when main may not
# have reached GitHub yet (#145).
#
#   SHA=<40 hex> scripts/wait-for-main.sh
#
# Every poll re-fetches main and asks `git merge-base --is-ancestor`: the commit
# must be reachable from origin/main, not just equal to it. The checkout must hold
# the history (the release workflow uses fetch-depth 0). A failed fetch counts as
# a poll that did not find the commit. Time is counted in whole intervals slept,
# so the limits are exact and the tests need no clock. WAIT_INTERVAL_SECONDS (15)
# and WAIT_TIMEOUT_SECONDS (300) are digits only; the defaults are the production
# ones.
set -euo pipefail

interval="${WAIT_INTERVAL_SECONDS:-15}"
timeout="${WAIT_TIMEOUT_SECONDS:-300}"
sha="${SHA:-}"

fail() { echo "::error::$*"; exit 1; }

[[ "$sha" =~ ^[0-9a-f]{40}$ ]] || fail "SHA must be 40 lowercase hex characters"
for v in "$interval" "$timeout"; do
  [[ "$v" =~ ^[0-9]{1,6}$ ]] || fail "WAIT_INTERVAL_SECONDS and WAIT_TIMEOUT_SECONDS must be digits only (at most 6)"
done
interval=$((10#$interval)) timeout=$((10#$timeout)) # no octal surprises
[ "$interval" -ge 1 ] || fail "WAIT_INTERVAL_SECONDS must be at least 1"

elapsed=0
while :; do
  if git fetch --no-tags --quiet origin '+refs/heads/main:refs/remotes/origin/main' \
    && git merge-base --is-ancestor "$sha" origin/main; then
    echo "$sha is on main after ${elapsed}s"
    exit 0
  fi
  if [ $((elapsed + interval)) -gt "$timeout" ]; then
    fail "$sha is not on main after waiting ${elapsed}s (limit ${timeout}s): was main pushed?"
  fi
  echo "$sha is not on origin/main yet, waiting (${elapsed}s)"
  sleep "$interval"
  elapsed=$((elapsed + interval))
done
