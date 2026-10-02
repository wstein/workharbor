#!/usr/bin/env bash
# board-snapshot.sh: the shared, on-request snapshot of the project board (#132).
#
#   board-snapshot.sh [--refresh]            print the snapshot JSON
#   board-snapshot.sh queue <lane> [--refresh]  the lane's Todo cards, P1 first, then by issue number
#   board-snapshot.sh card <number> [--refresh] one card: number, status, session, priority, title
#   board-snapshot.sh move <number> <status>   set a card's Status
#   board-snapshot.sh session <number> <lane>  set a card's Session
#   board-snapshot.sh priority <number> <P1|P2|P3>  set a card's Priority
#   board-snapshot.sh add <number>             add an issue to the board
#
# move sets only Todo, In progress, Blocked and In review: Ready to push
# (wh/review) and Done (closing the issue, the human) are refused before any gh
# call; they are set through `gh project item-edit` directly.
#
# The four writes run `gh project item-edit` / `item-add` by the issue's URL
# and make no board query (add runs one `item-add --format json`, the only call). Only when GitHub accepts the write is that one card
# patched in the cache (under the lock, fetched_at unchanged: a write does not
# make old data fresh). A failed write leaves the cache untouched and exits 1.
# With no cache, or a stale one, the write still happens and the cache is left
# alone. Values are checked against fixed lists before any gh call.
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
move | session | priority | add)
  case ${args[1]:-} in '' | *[!0-9]* | 0*) die "usage: board-snapshot.sh $mode <number> ..." ;; esac
  [ ${#args[1]} -le 9 ] || die "the issue number is too long"
  case $mode in
  move)
    case ${args[2]:-} in
    "Todo" | "In progress" | "Blocked" | "In review") ;;
    "Ready to push" | "Done")
      die "move does not set \"${args[2]}\": Ready to push is set only by wh/review and Done by closing the issue or by the human, through gh project item-edit directly" ;;
    *) die "status must be one of: Todo, In progress, Blocked, In review" ;;
    esac
    ;;
  session)
    case ${args[2]:-} in
    "wh/design" | "wh/platform" | "wh/runtime" | "wh/review" | "wh/verify" | "wh/docs" | "wh/spikes" | "wh/desk" | "Werner") ;;
    *) die "lane must be one of: wh/design, wh/platform, wh/runtime, wh/review, wh/verify, wh/docs, wh/spikes, wh/desk, Werner" ;;
    esac
    ;;
  priority)
    case ${args[2]:-} in P1 | P2 | P3) ;; *) die "priority must be one of: P1, P2, P3" ;; esac
    ;;
  add) [ -z "${args[2]:-}" ] || die "usage: board-snapshot.sh add <number>" ;;
  esac
  [ "$mode" = add ] || [ ${#args[@]} -eq 3 ] || die "usage: board-snapshot.sh $mode <number> <value>"
  ;;
*) die "unknown mode $mode (print, queue <lane>, card <number>, move, session, priority, add)" ;;
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

# lock_takeover removes a stale lock. Only the holder of $lock.takeover (a
# mkdir mutex) checks again and renames the stale directory away, so one caller
# cannot remove a lock another has just taken; the rename is atomic and the
# stale copy is deleted afterwards. A takeover mutex left by a dead caller is
# dropped after 120 seconds.
lock_takeover() {
  local t now
  if mkdir "$lock.takeover" 2>/dev/null; then
    date +%s >"$lock.takeover/ts"
    now=$(date +%s)
    t=$(cat "$lock/ts" 2>/dev/null || echo "$now")
    case $t in '' | *[!0-9]*) t=$now ;; esac
    if [ $((now - t)) -gt 120 ] && mv "$lock" "$lock.stale.$$" 2>/dev/null; then
      rm -rf "$lock.stale.$$"
    fi
    rm -rf "$lock.takeover"
  else
    now=$(date +%s)
    t=$(cat "$lock.takeover/ts" 2>/dev/null || echo "$now")
    case $t in '' | *[!0-9]*) t=$now ;; esac
    [ $((now - t)) -le 120 ] || rm -rf "$lock.takeover"
    sleep 0.05
  fi
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
      lock_takeover
      continue
    fi
    waited=1
    i=$((i + 1))
    [ $i -le 600 ] || return 1
    sleep 0.1
  done
  date +%s >"$lock/ts"
}

# patch rewrites the cache with one jq filter ($@ are its jq arguments), only
# when the cache is fresh, under the lock, re-reading the file under the lock.
patch() {
  local filter=$1 tmp
  shift
  if ! lock_take; then
    echo "board-snapshot: the write is done, but the cache lock was busy: the cache is not patched (--refresh shows it)" >&2
    return 0
  fi
  if fresh; then
    tmp=$(mktemp "$dir/.board.XXXXXX")
    if jq "$@" "$filter" "$file" >"$tmp"; then
      chmod 600 "$tmp"
      mv "$tmp" "$file"
    else
      rm -f "$tmp"
      echo "board-snapshot: the write is done, but the cache could not be patched (--refresh shows it)" >&2
    fi
  else
    echo "board-snapshot: the write is done; there is no fresh cache, so none was patched" >&2
  fi
  rm -rf "$lock"
}

case $mode in
move | session | priority | add)
  n=${args[1]}
  url=https://github.com/wstein/workharbor/issues/$n
  case $mode in
  move) field=Status key=status ;;
  session) field=Session key=session ;;
  priority) field=Priority key=priority ;;
  esac
  if [ "$mode" = add ]; then
    out=$(gh project item-add 6 --owner wstein --url "$url" --format json) || die "GitHub refused the add; the cache is unchanged"
    # One call: its JSON names the new item; the title is taken from it when
    # there (unverified: not measured on the live API), else the card is
    # cached with a null title (--refresh fills it).
    title=$(printf '%s' "$out" | jq -r '.title // empty' 2>/dev/null || true)
    patch '.items |= (if any(.[]; .number == $n) then . else . + [{number: $n, title: (if $title == "" then null else $title end), status: null, session: null, priority: null, labels: [], type: "Issue", url: $url}] end)' \
      --argjson n "$n" --arg title "$title" --arg url "$url"
  else
    value=${args[2]}
    gh project item-edit 6 --owner wstein --url "$url" --field "$field" --value "$value" >/dev/null ||
      die "GitHub refused the write; the cache is unchanged"
    patch '.items |= map(if .number == $n then .[$k] = $v else . end)' \
      --argjson n "$n" --arg k "$key" --arg v "$value"
  fi
  exit 0
  ;;
esac

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
