#!/usr/bin/env bash
# board-snapshot.sh: the shared, on-request snapshot of the project board (#132).
#
#   board-snapshot.sh [--refresh]            print the snapshot JSON
#   board-snapshot.sh queue <lane> [--refresh]  the lane's Todo cards, P1 first, then by issue number
#   board-snapshot.sh card <number> [--refresh] one card: number, status, session, priority, title
#
# The snapshot is {"fetched_at": <unix>, "items": [...]} in one file outside the
# repository. While it is younger than WHR_BOARD_MAX_AGE seconds (default 300)
# nothing calls GitHub. Past that, one caller takes a lock, makes ONE board
# query and rewrites the file; the others wait and read the result. --refresh
# forces the query (use it after you moved a card, or your write is not seen).
# A failed query (rate limit) keeps the old file, prints it, and says "stale"
# on stderr. The script holds no token: it uses the existing `gh` login.
#
#   WHR_BOARD_SNAPSHOT  absolute path of the file (default
#                       ${XDG_CACHE_HOME:-$HOME/.cache}/workharbor/board.json)
#   WHR_BOARD_MAX_AGE   seconds (default 300)
#
# Needs bash, jq and gh.
set -euo pipefail
umask 077

die() { echo "board-snapshot: $*" >&2; exit 1; }

command -v jq >/dev/null 2>&1 || die "jq is required (brew install jq)"

max_age=${WHR_BOARD_MAX_AGE:-300}
case $max_age in '' | *[!0-9]*) die "WHR_BOARD_MAX_AGE must be a number of seconds" ;; esac

if [ -n "${WHR_BOARD_SNAPSHOT:-}" ]; then
  file=$WHR_BOARD_SNAPSHOT
else
  [ -n "${XDG_CACHE_HOME:-}" ] || [ -n "${HOME:-}" ] || die "HOME is not set"
  file=${XDG_CACHE_HOME:-$HOME/.cache}/workharbor/board.json
fi
case $file in /*) ;; *) die "the snapshot path must be absolute" ;; esac
case $file in *$'\n'*) die "the snapshot path must not contain a newline" ;; esac

refresh=0
args=()
for a in "$@"; do
  if [ "$a" = "--refresh" ]; then refresh=1; else args+=("$a"); fi
done
mode=${args[0]:-print}
case $mode in
print) ;;
queue) [ -n "${args[1]:-}" ] || die "usage: board-snapshot.sh queue <lane>" ;;
card)
  case ${args[1]:-} in '' | *[!0-9]*) die "usage: board-snapshot.sh card <number>" ;; esac
  ;;
*) die "unknown mode $mode (print, queue <lane>, card <number>)" ;;
esac

dir=$(dirname "$file")
lock=$dir/.board.lock
mkdir -p "$dir"

# fresh succeeds when the file holds a snapshot younger than max_age.
fresh() {
  local at now
  [ -f "$file" ] || return 1
  at=$(jq -er '.fetched_at | numbers' "$file" 2>/dev/null) || return 1
  now=$(date +%s)
  [ $((now - at)) -lt "$max_age" ] && [ "$at" -le "$now" ]
}

query() {
  local out tmp
  out=$(gh project item-list 6 --owner wstein --format json --limit 300) || return 1
  tmp=$(mktemp "$dir/.board.XXXXXX")
  if ! printf '%s' "$out" | jq --argjson now "$(date +%s)" '{
    fetched_at: $now,
    items: [.items[] | {
      number: .content.number,
      title: (.title // .content.title),
      status: .status,
      session: .session,
      priority: .priority,
      labels: (.labels // []),
      type: .content.type,
      url: .content.url
    }]
  }' >"$tmp"; then
    rm -f "$tmp"
    return 1
  fi
  chmod 600 "$tmp"
  mv "$tmp" "$file"
}

# lock_take takes the lock directory; a lock older than 120 seconds is stale.
lock_take() {
  local i=0 t now
  waited=0
  while ! mkdir "$lock" 2>/dev/null; do
    now=$(date +%s)
    t=$(cat "$lock/ts" 2>/dev/null || echo "$now")
    case $t in '' | *[!0-9]*) t=$now ;; esac
    if [ $((now - t)) -gt 120 ]; then
      rm -rf "$lock"
      continue
    fi
    waited=1
    i=$((i + 1))
    [ $i -le 600 ] || return 1
    sleep 0.1
  done
  date +%s >"$lock/ts"
}

if [ "$refresh" = 1 ] || ! fresh; then
  started=$(date +%s)
  if lock_take; then
    trap 'rm -rf "$lock"' EXIT
    # Another caller may have refreshed while this one waited; a forced
    # refresh still queries unless that happened within this very call.
    if [ -f "$file" ] && fresh && {
      [ "$refresh" = 0 ] || { [ "$waited" = 1 ] && [ "$(jq -r '.fetched_at' "$file")" -ge "$started" ]; }
    }; then
      :
    elif ! query; then
      if [ -f "$file" ]; then
        echo "board-snapshot: stale: the board query failed (rate limit?), using the snapshot of $(jq -r '.fetched_at' "$file" 2>/dev/null || echo unknown)" >&2
      else
        die "the board query failed and there is no snapshot"
      fi
    fi
    rm -rf "$lock"
    trap - EXIT
  elif [ ! -f "$file" ]; then
    die "could not take the lock and there is no snapshot"
  fi
fi

[ -f "$file" ] || die "no snapshot"

case $mode in
print) cat "$file" ;;
queue)
  jq -r --arg lane "${args[1]}" '
    def rank: ({"P1": 1, "P2": 2, "P3": 3}[.] // 9);
    [.items[] | select(.status == "Todo" and .session == $lane)]
    | sort_by([(.priority | rank), .number])[]
    | [(.priority // "-"), "#\(.number)", .title, .url] | @tsv' "$file"
  ;;
card)
  jq -r --argjson n "${args[1]}" '
    .items[] | select(.number == $n)
    | [("#\(.number)"), .status, (.session // "-"), (.priority // "-"), .title] | @tsv' "$file"
  ;;
esac
