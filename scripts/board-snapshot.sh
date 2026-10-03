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
#   board-snapshot.sh ready <number>           set Ready to push (wh/review only)
#
# move sets only Todo, In progress, Blocked and In review: Ready to push
# (wh/review) and Done (closing the issue, the human) are refused before any gh
# call. `ready` sets Ready to push and is for wh/review only (the review gate,
# AGENTS.md); nobody else runs it. Done is set by the human or by closing the issue.
#
# Writes use the item-ID route, never `gh project item-edit --url`, whose
# project-wide item lookup trips GitHub's secondary rate limit (#165): one call
# finds the issue's project item (repository.issue.projectItems), one mutation
# sets the field (updateProjectV2ItemFieldValue); add is a lookup of the issue's
# node ID and addProjectV2ItemById. The project, field and option IDs are cached
# in board-fields.json next to the snapshot (one query when missing; refetched
# once if an option is not found). A write is at most two GraphQL calls, no
# retry. Only when GitHub accepts the write is that one card
# patched in the cache (under the lock, fetched_at unchanged: a write does not
# make old data fresh). A failed write leaves the cache untouched and exits 1.
# With no cache, or a stale one, the write still happens and the cache is left
# alone. Values are checked against fixed lists before any gh call.
#
# Before any call to GitHub the script reads `gh api rate_limit` (free: it does
# not count) and warns on stderr when the GraphQL budget is under 20 % (#165).
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
move | session | priority | add | ready)
  case ${args[1]:-} in '' | *[!0-9]* | 0*) die "usage: board-snapshot.sh $mode <number> ..." ;; esac
  [ ${#args[1]} -le 9 ] || die "the issue number is too long"
  case $mode in
  move)
    case ${args[2]:-} in
    "Todo" | "In progress" | "Blocked" | "In review") ;;
    "Ready to push" | "Done")
      die "move does not set \"${args[2]}\": Ready to push is set only by wh/review (board-snapshot.sh ready <number>) and Done by closing the issue or by the human" ;;
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
  add | ready) [ -z "${args[2]:-}" ] || die "usage: board-snapshot.sh $mode <number>" ;;
  esac
  [ "$mode" = add ] || [ "$mode" = ready ] || [ ${#args[@]} -eq 3 ] || die "usage: board-snapshot.sh $mode <number> <value>"
  ;;
*) die "unknown mode $mode (print, queue <lane>, card <number>, move, session, priority, add, ready)" ;;
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

# rate_warn prints one stderr line when the GraphQL budget is under 20 %. It
# reads `gh api rate_limit`, which costs nothing; any failure is silent.
rate_warn() {
  local out rem lim rst when
  out=$(gh api rate_limit 2>/dev/null) || return 0
  read -r rem lim rst < <(printf '%s' "$out" | jq -r '.resources.graphql | "\(.remaining) \(.limit) \(.reset)"' 2>/dev/null) || return 0
  case $rem$lim$rst in '' | *[!0-9]*) return 0 ;; esac
  [ "$lim" -gt 0 ] || return 0
  if [ $((rem * 5)) -lt "$lim" ]; then
    when=$(date -r "$rst" +%H:%M 2>/dev/null || date -d "@$rst" +%H:%M 2>/dev/null || echo "$rst")
    echo "board-snapshot: warning: GitHub GraphQL budget is low: $rem of $lim left, resets at $when" >&2
  fi
}

project=PVT_kwHNjWrOAZVCuA
fields=$dir/board-fields.json

# fields_fetch caches the field and option IDs of Status, Session and Priority.
fields_fetch() {
  local out tmp
  out=$(gh api graphql -f query='query($p:ID!){node(id:$p){... on ProjectV2{fields(first:30){nodes{... on ProjectV2SingleSelectField{id name options{id name}}}}}}}' -f p="$project") || return 1
  tmp=$(mktemp "$dir/.board.XXXXXX")
  if ! printf '%s' "$out" | jq '[.data.node.fields.nodes[] | select(.name == "Status" or .name == "Session" or .name == "Priority")
      | {key: .name, value: {id: .id, options: ((.options // []) | map({key: .name, value: .id}) | from_entries)}}] | from_entries' >"$tmp"; then
    rm -f "$tmp"
    return 1
  fi
  chmod 600 "$tmp"
  mv "$tmp" "$fields"
}

# field_ids prints "<field id> <option id>" for a field and value.
field_ids() {
  local r
  if [ -f "$fields" ]; then
    r=$(jq -r --arg f "$1" --arg v "$2" '.[$f] | select(.) | [.id, (.options[$v] // empty)] | join(" ")' "$fields" 2>/dev/null || true)
    case $r in *" "?*) printf '%s\n' "$r"; return 0 ;; esac
  fi
  fields_fetch || return 1
  r=$(jq -r --arg f "$1" --arg v "$2" '.[$f] | select(.) | [.id, (.options[$v] // empty)] | join(" ")' "$fields") || return 1
  case $r in *" "?*) printf '%s\n' "$r" ;; *) return 1 ;; esac
}

# items_query asks only for what the snapshot holds, 100 items a page (#165).
items_query='query($p:ID!,$after:String){node(id:$p){... on ProjectV2{items(first:100,after:$after){pageInfo{hasNextPage endCursor} nodes{content{__typename ... on Issue{number title url labels(first:20){nodes{name}}} ... on PullRequest{number title url labels(first:20){nodes{name}}} ... on DraftIssue{title}} status:fieldValueByName(name:"Status"){... on ProjectV2ItemFieldSingleSelectValue{name}} session:fieldValueByName(name:"Session"){... on ProjectV2ItemFieldSingleSelectValue{name}} priority:fieldValueByName(name:"Priority"){... on ProjectV2ItemFieldSingleSelectValue{name}}}}}}}'

# query reads every page of the board with first:100 and an after: cursor and
# rewrites the snapshot only when all pages came back; one failed page leaves
# the old file alone and fails the query.
query() {
  local out cursor="" more=true tmp pages
  rate_warn
  pages=$(mktemp "$dir/.board.XXXXXX")
  while [ "$more" = true ]; do
    if [ -n "$cursor" ]; then
      out=$(gh api graphql -f query="$items_query" -f p="$project" -f after="$cursor") || { rm -f "$pages"; return 1; }
    else
      out=$(gh api graphql -f query="$items_query" -f p="$project") || { rm -f "$pages"; return 1; }
    fi
    if ! printf '%s' "$out" | jq -e '.data.node.items.nodes | arrays' >/dev/null 2>&1; then
      rm -f "$pages"
      return 1
    fi
    printf '%s' "$out" | jq -c '.data.node.items.nodes[]' >>"$pages" || { rm -f "$pages"; return 1; }
    more=$(printf '%s' "$out" | jq -r '.data.node.items.pageInfo.hasNextPage // false')
    cursor=$(printf '%s' "$out" | jq -r '.data.node.items.pageInfo.endCursor // empty')
    if [ "$more" = true ] && [ -z "$cursor" ]; then
      rm -f "$pages"
      return 1
    fi
  done
  tmp=$(mktemp "$dir/.board.XXXXXX")
  if ! jq -s --argjson now "$(date +%s)" '{
    fetched_at: $now,
    items: [.[] | {
      number: .content.number,
      title: .content.title,
      status: .status.name,
      session: .session.name,
      priority: .priority.name,
      labels: [.content.labels.nodes[]?.name],
      type: .content.__typename,
      url: .content.url
    }]
  }' "$pages" >"$tmp"; then
    rm -f "$tmp" "$pages"
    return 1
  fi
  rm -f "$pages"
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
move | session | priority | add | ready)
  n=${args[1]}
  url=https://github.com/wstein/workharbor/issues/$n
  rate_warn
  if [ "$mode" = add ]; then
    # Two calls: the issue's node ID and title, then addProjectV2ItemById.
    info=$(gh api graphql -f query='query($n:Int!){repository(owner:"wstein",name:"workharbor"){issue(number:$n){id title}}}' -F n="$n") ||
      die "GitHub refused the add; the cache is unchanged"
    cid=$(printf '%s' "$info" | jq -r '.data.repository.issue.id // empty')
    title=$(printf '%s' "$info" | jq -r '.data.repository.issue.title // empty')
    [ -n "$cid" ] || die "issue #$n not found; the cache is unchanged"
    gh api graphql -f query='mutation($p:ID!,$c:ID!){addProjectV2ItemById(input:{projectId:$p,contentId:$c}){item{id}}}' -f p="$project" -f c="$cid" >/dev/null ||
      die "GitHub refused the add; the cache is unchanged"
    patch '.items |= (if any(.[]; .number == $n) then . else . + [{number: $n, title: (if $title == "" then null else $title end), status: null, session: null, priority: null, labels: [], type: "Issue", url: $url}] end)' \
      --argjson n "$n" --arg title "$title" --arg url "$url"
  else
    case $mode in
    move) field=Status key=status value=${args[2]} ;;
    ready) field=Status key=status value="Ready to push" ;;
    session) field=Session key=session value=${args[2]} ;;
    priority) field=Priority key=priority value=${args[2]} ;;
    esac
    ids=$(field_ids "$field" "$value") || die "could not find the $field field or the option \"$value\" on the board; the cache is unchanged"
    fid=${ids%% *} oid=${ids#* }
    info=$(gh api graphql -f query='query($n:Int!){repository(owner:"wstein",name:"workharbor"){issue(number:$n){projectItems(first:10){nodes{id project{id}}}}}}' -F n="$n") ||
      die "GitHub refused the lookup; the cache is unchanged"
    item=$(printf '%s' "$info" | jq -r --arg p "$project" '[.data.repository.issue.projectItems.nodes[]? | select(.project.id == $p) | .id][0] // empty')
    [ -n "$item" ] || die "issue #$n is not on the board (use: board-snapshot.sh add $n); the cache is unchanged"
    gh api graphql -f query='mutation($p:ID!,$i:ID!,$f:ID!,$o:String!){updateProjectV2ItemFieldValue(input:{projectId:$p,itemId:$i,fieldId:$f,value:{singleSelectOptionId:$o}}){projectV2Item{id}}}' \
      -f p="$project" -f i="$item" -f f="$fid" -f o="$oid" >/dev/null ||
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
