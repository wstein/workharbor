#!/usr/bin/env bash
# board-snapshot.sh: the shared, on-request snapshot of the project board (#132).
#
#   board-snapshot.sh [--refresh]            print the snapshot JSON
#   board-snapshot.sh queue <lane> [--refresh]  the lane's Todo cards, P1 first, then by issue number
#   board-snapshot.sh card <number> [--refresh] one card: number, status, session, priority, title
#   board-snapshot.sh move <number>... <status>   set each card's Status
#   board-snapshot.sh session <number>... <lane>  set each card's Session
#   board-snapshot.sh priority <number>... <P1|P2|P3>  set each card's Priority
#   board-snapshot.sh add <number>...             add each issue to the board
#   board-snapshot.sh budget                      lowest remaining and total cost, last 24 hours (no gh call)
#
#   board-snapshot.sh ready <number>...           set Ready to push (wh/dispatch for wh/review)
#
# Every write mode takes several issues (at most 50) in one call: one value
# for all, one cache patch for the ones that succeeded. Who may write, and what
# asks for permission, is set in AGENTS.md (GitHub rate limit). A failure on one issue is reported on stderr, the rest
# still run, and the exit status is 1 if any failed. Input is validated before any gh call.
#
# move sets only Todo, In progress, Blocked and In review: Ready to push
# (wh/review) and Done (closing the issue, the human) are refused before any gh
# call. `ready` sets Ready to push through wh/dispatch on behalf of wh/review
# for the reviewed sha (the review gate, AGENTS.md). Done is set by the human or by closing the issue.
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
# The board query asks for rateLimit { cost remaining limit resetAt } too (no
# extra points) and each refresh appends one JSON line (at, cost, remaining,
# limit) to board-budget.log next to the snapshot (0600, no tokens; trimmed to
# its last 1000 lines past 2000); `budget` reads it (#186). The 20 % warning on
# stderr (#165) uses the query's own remaining value, which is current;
# `gh api rate_limit` (free, but it lags the GraphQL counter) is only the
# fallback when a query fails or returns no rateLimit, and the source for
# writes, whose mutations carry no rateLimit.
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

repository=${WHR_BOARD_REPOSITORY:-wstein/workharbor}
owner=${WHR_BOARD_OWNER:-${repository%%/*}}
project=${WHR_BOARD_PROJECT_ID:-}
project_number=${WHR_BOARD_PROJECT_NUMBER:-}
lane_prefix=${WHR_BOARD_LANE_PREFIX:-wh}
roles=${WHR_BOARD_ROLES:-desk,dispatch,design,review,platform,runtime,docs,verify,spikes}
[[ $repository =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || die "invalid repository"
[[ $owner =~ ^[A-Za-z0-9_-]+$ ]] || die "invalid owner"
[[ $lane_prefix =~ ^[a-z][a-z0-9_-]*$ ]] || die "invalid lane prefix"
[[ $roles =~ ^[a-z]+(,[a-z]+)*$ ]] || die "invalid roles"
jq -en --arg roles "$roles" '$roles | split(",") | length == (unique | length)' >/dev/null || die "duplicate roles"
[[ -z "$project_number" || $project_number =~ ^[1-9][0-9]*$ ]] || die "invalid project number"
case $project in '' | PVT_*) ;; *) die "invalid project ID" ;; esac
[[ -z "$project" || $project =~ ^PVT_[A-Za-z0-9_-]+$ ]] || die "invalid project ID"
if [ "$repository" != wstein/workharbor ] && { [ "$project" = PVT_kwHNjWrOAZVCuA ] || { [ "$owner" = wstein ] && [ "$project_number" = 6 ]; }; }; then
  die "nondefault repository cannot target workharbor project 6"
fi
if [ "$repository" != wstein/workharbor ] || [ "$owner" != wstein ]; then
  [ -n "$project$project_number" ] || die "nondefault repository requires an explicit project"
else
  if [ -z "$project$project_number" ]; then project=PVT_kwHNjWrOAZVCuA; fi
fi
target="$owner/$repository/${project:-$project_number}/$lane_prefix/$roles"

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
cache_root=$(dirname "$file")
if [ "$target" != "wstein/wstein/workharbor/PVT_kwHNjWrOAZVCuA/wh/desk,dispatch,design,review,platform,runtime,docs,verify,spikes" ]; then
  scope=$(printf '%s' "$target" | shasum -a 256 | cut -d ' ' -f 1)
  file="${file}.scopes/$scope/board.json"
fi

refresh=0
args=()
for a in "$@"; do
  if [ "$a" = "--refresh" ]; then refresh=1; else args+=("$a"); fi
done
mode=${args[0]:-print}
case $mode in
print) ;;
metadata)
  [ ${#args[@]} -eq 1 ] || { [ ${#args[@]} -eq 2 ] && [ "${args[1]}" = schema ]; } || die "usage: board-snapshot.sh metadata [schema]"
  ;;
configure | configure-fields)
  [ ${#args[@]} -eq 2 ] && [[ ${args[1]} =~ ^PVT_[A-Za-z0-9_-]+$ ]] || die "usage: board-snapshot.sh configure <confirmed-project-ID>"
  [ -n "${WHR_BOARD_REPOSITORY:-}" ] && [ -n "${WHR_BOARD_OWNER:-}" ] && [ -n "$project$project_number" ] || die "configure requires explicit repository, owner and project"
  [ "${args[1]}" != PVT_kwHNjWrOAZVCuA ] || die "configure does not change workharbor project 6"
  ;;
queue) [ -n "${args[1]:-}" ] || die "usage: board-snapshot.sh queue <lane>" ;;
card)
  case ${args[1]:-} in '' | *[!0-9]*) die "usage: board-snapshot.sh card <number>" ;; esac
  ;;
move | session | priority | add | ready)
  # move, session and priority end with one value shared by every issue; add
  # and ready take only issue numbers. All input is checked before any gh call.
  nums=("${args[@]:1}")
  value=""
  case $mode in
  move | session | priority)
    [ ${#nums[@]} -ge 2 ] || die "usage: board-snapshot.sh $mode <number> [<number> ...] <value>"
    value=${nums[${#nums[@]} - 1]}
    unset 'nums[${#nums[@]}-1]'
    ;;
  esac
  [ ${#nums[@]} -ge 1 ] || die "usage: board-snapshot.sh $mode <number> [<number> ...]"
  [ ${#nums[@]} -le 50 ] || die "at most 50 issues in one call"
  for n in "${nums[@]}"; do
    case $n in '' | *[!0-9]* | 0*) die "usage: board-snapshot.sh $mode <number> [<number> ...]${value:+ <value>}: \"$n\" is not an issue number" ;; esac
    [ ${#n} -le 9 ] || die "the issue number is too long"
  done
  case $mode in
  move)
    case $value in
    "Todo" | "In progress" | "Blocked" | "In review") ;;
    "Ready to push" | "Done")
      die "move does not set \"$value\": Ready to push is set by board-snapshot.sh ready <number> ... (approved by wh/review, written by wh/dispatch for the reviewed sha) and Done by closing the issue or by the human" ;;
    *) die "status must be one of: Todo, In progress, Blocked, In review" ;;
    esac
    ;;
  session)
    if [ "$value" != Werner ] || [ "$lane_prefix" != wh ]; then
      case ",$roles," in *",${value#"$lane_prefix/"},"*) [[ $value == "$lane_prefix/"* ]] || die "invalid lane" ;;
      *) die "lane must use $lane_prefix and a configured role" ;; esac
    fi
    ;;
  priority)
    case $value in P1 | P2 | P3) ;; *) die "priority must be one of: P1, P2, P3" ;; esac
    ;;
  esac
  ;;
budget) ;;
*) die "unknown mode $mode (print, queue <lane>, card <number>, move, session, priority, add, ready, budget)" ;;
esac

dir=$(dirname "$file")
lock=$dir/.board.lock
mkdir -p "$dir"

if [ "$mode" != budget ] && { [ -n "${WHR_BOARD_OWNER+x}${WHR_BOARD_PROJECT_NUMBER+x}${WHR_BOARD_PROJECT_ID+x}" ] || [ "$repository" != wstein/workharbor ] || [ "$mode" = metadata ]; }; then
  repo_name=${repository#*/}
  repo_owner=${repository%%/*}
  if [ -n "$project_number" ]; then
    metadata=$(gh api graphql -f query='query($owner:String!,$number:Int!,$repoOwner:String!,$repoName:String!){repository(owner:$repoOwner,name:$repoName){nameWithOwner} user(login:$owner){projectV2(number:$number){id number url owner{... on User{login} ... on Organization{login}}}}}' -f owner="$owner" -F number="$project_number" -f repoOwner="$repo_owner" -f repoName="$repo_name") || die "project lookup failed"
    resolved=$(printf '%s' "$metadata" | jq -er '.data.user.projectV2.id') || die "project not found"
    [ -z "$project" ] || [ "$project" = "$resolved" ] || die "project ID and number disagree"
    project=$resolved
    metadata=$(printf '%s' "$metadata" | jq '.data.node = .data.user.projectV2')
  else
    metadata=$(gh api graphql -f query='query($p:ID!,$repoOwner:String!,$repoName:String!){repository(owner:$repoOwner,name:$repoName){nameWithOwner} node(id:$p){... on ProjectV2{id number url owner{... on User{login} ... on Organization{login}}}}}' -f p="$project" -f repoOwner="$repo_owner" -f repoName="$repo_name") || die "project lookup failed"
  fi
  printf '%s' "$metadata" | jq -e --arg owner "$owner" --arg repo "$repository" --arg project "$project" '.data.repository.nameWithOwner == $repo and .data.node.owner.login == $owner and .data.node.id == $project' >/dev/null || die "project/repository identity mismatch"
  if [ "$mode" = configure ] || [ "$mode" = configure-fields ]; then [ "${args[1]}" = "$project" ] || die "confirmed project ID does not match the resolved target"; fi
  if [ "$mode" = metadata ] && [ ${#args[@]} -eq 1 ]; then printf '%s\n' "$metadata"; exit 0; fi
fi

# fresh succeeds when the file holds a snapshot younger than max_age.
matches_target() {
  [ -f "$file" ] && jq -e --arg target "$target" '.target == $target' "$file" >/dev/null 2>&1
}

fresh() {
  local at now
  [ -f "$file" ] || return 1
  matches_target || return 1
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

budget_log=$dir/board-budget.log

# budget_note logs one line per refresh ($1 cost summed over the pages, $2 the
# last remaining, $3 limit) and warns under 20 % from that value. A failure to
# log never fails the refresh.
budget_note() {
  local rem=$2 lim=$3 reset=$4 when
  case $1 in '' | *[!0-9]*) return 1 ;; esac
  case $2 in '' | *[!0-9]*) return 1 ;; esac
  case $3 in '' | *[!0-9]*) return 1 ;; esac
  {
    touch "$budget_log" && chmod 600 "$budget_log" &&
      printf '{"at":%s,"cost":%s,"remaining":%s,"limit":%s}\n' "$(date +%s)" "$1" "$2" "$3" >>"$budget_log" &&
      if [ "$(wc -l <"$budget_log")" -gt 2000 ]; then
        tail -n 1000 "$budget_log" >"$budget_log.tmp" && chmod 600 "$budget_log.tmp" && mv "$budget_log.tmp" "$budget_log"
      fi
  } 2>/dev/null || true
  if [ "$lim" -gt 0 ] && [ $((rem * 5)) -lt "$lim" ]; then
    when=$(jq -rn --arg r "$reset" '$r | try (fromdateiso8601 | strflocaltime("%H:%M")) catch $r' 2>/dev/null || echo "$reset")
    echo "board-snapshot: warning: GitHub GraphQL budget is low: $rem of $lim left, resets at $when" >&2
  fi
}

fields=$dir/board-fields.json

# fields_fetch caches the field and option IDs of Status, Session and Priority.
fields_fetch() {
  local out tmp
  out=$(connection_read fields) || return 1
  tmp=$(mktemp "$dir/.board.XXXXXX")
  if ! printf '%s' "$out" | jq '[.[] | select(.name == "Status" or .name == "Session" or .name == "Priority")
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
items_query='query($p:ID!,$after:String){rateLimit{cost remaining limit resetAt} node(id:$p){... on ProjectV2{items(first:100,after:$after){pageInfo{hasNextPage endCursor} nodes{content{__typename ... on Issue{number title url repository{nameWithOwner} labels(first:20){nodes{name}}}} status:fieldValueByName(name:"Status"){... on ProjectV2ItemFieldSingleSelectValue{name}} session:fieldValueByName(name:"Session"){... on ProjectV2ItemFieldSingleSelectValue{name}} priority:fieldValueByName(name:"Priority"){... on ProjectV2ItemFieldSingleSelectValue{name}}}}}}}'

# query reads every page of the board with first:100 and an after: cursor and
# rewrites the snapshot only when all pages came back; one failed page leaves
# the old file alone and fails the query.
query() {
  local out cursor="" more=true tmp pages cost=0 rem="" lim="" reset="" c
  pages=$(mktemp "$dir/.board.XXXXXX")
  while [ "$more" = true ]; do
    if [ -n "$cursor" ]; then
      out=$(gh api graphql -f query="$items_query" -f p="$project" -f after="$cursor") || { rm -f "$pages"; rate_warn; return 1; }
    else
      out=$(gh api graphql -f query="$items_query" -f p="$project") || { rm -f "$pages"; rate_warn; return 1; }
    fi
    if ! printf '%s' "$out" | jq -e '.data.node.items.nodes | arrays' >/dev/null 2>&1; then
      rm -f "$pages"
      return 1
    fi
    c=$(printf '%s' "$out" | jq -r '.data.rateLimit.cost // empty' 2>/dev/null || true)
    case $c in '' | *[!0-9]*) ;; *) cost=$((cost + c)) ;; esac
    rem=$(printf '%s' "$out" | jq -r '.data.rateLimit.remaining // empty' 2>/dev/null || true)
    lim=$(printf '%s' "$out" | jq -r '.data.rateLimit.limit // empty' 2>/dev/null || true)
    reset=$(printf '%s' "$out" | jq -r '.data.rateLimit.resetAt // empty' 2>/dev/null || true)
    printf '%s' "$out" | jq -c '.data.node.items.nodes[]' >>"$pages" || { rm -f "$pages"; return 1; }
    more=$(printf '%s' "$out" | jq -r '.data.node.items.pageInfo.hasNextPage // false')
    cursor=$(printf '%s' "$out" | jq -r '.data.node.items.pageInfo.endCursor // empty')
    if [ "$more" = true ] && [ -z "$cursor" ]; then
      rm -f "$pages"
      return 1
    fi
  done
  tmp=$(mktemp "$dir/.board.XXXXXX")
  if ! jq -s --arg target "$target" --arg repo "$repository" --argjson now "$(date +%s)" '{
    target: $target,
    fetched_at: $now,
    items: [.[] | select(.content.__typename == "Issue" and .content.repository.nameWithOwner == $repo) | {
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
  budget_note "$cost" "$rem" "$lim" "$reset" || rate_warn
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

configure_lock_take() {
  local attempt=0 owner_pid owner_token extra process_status process_output
  configure_token=$(mktemp "$cache_root/.board-owner.XXXXXX") || return 1
  while [ "$attempt" -lt 600 ]; do
    attempt=$((attempt + 1))
    if mkdir "$lock.guard" 2>/dev/null; then
      if [ -d "$lock" ]; then
        owner_pid='' owner_token='' extra=''
        read -r owner_pid owner_token extra <"$lock/owner" 2>/dev/null || true
        case $owner_pid in
        '' | 0 | *[!0-9]*) owner_pid='' ;;
        esac
        if [ -n "$owner_pid" ] && [ -n "$owner_token" ] && [ -z "$extra" ] && ! kill -0 "$owner_pid" 2>/dev/null; then
          process_status=0
          process_output=$(ps -p "$owner_pid" -o pid= 2>/dev/null) || process_status=$?
          if [ "$process_status" -eq 1 ] && [ -z "$process_output" ]; then
            rm -rf "$lock"
          fi
        fi
      fi
      if mkdir "$lock" 2>/dev/null; then
        printf '%s %s\n' "$$" "$configure_token" >"$lock/owner"
        rmdir "$lock.guard"
        return 0
      fi
      rmdir "$lock.guard"
    fi
    sleep 0.1
  done
  rm -f "$configure_token"
  return 1
}

configure_lock_release() {
  local attempt=0
  while [ "$attempt" -lt 600 ]; do
    attempt=$((attempt + 1))
    if mkdir "$lock.guard" 2>/dev/null; then
      if [ "$(cat "$lock/owner" 2>/dev/null)" = "$$ $configure_token" ]; then
        rm -rf "$lock"
      fi
      rmdir "$lock.guard"
      rm -f "$configure_token"
      return
    fi
    sleep 0.1
  done
  rm -f "$configure_token"
  echo "board-snapshot: configuration lock cleanup is busy; lock retained" >&2
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

connection_read() {
  local connection=$1 selection query_text out cursor="" more=true result='[]' seen='|' pages=0
  case $connection in
  fields) selection='... on ProjectV2Field{id name dataType} ... on ProjectV2SingleSelectField{id name dataType options{id name color description}} ... on ProjectV2IterationField{id name dataType}' ;;
  views) selection='id name layout filter configuration{visibleFields(first:100){nodes{... on ProjectV2FieldCommon{id}} pageInfo{hasNextPage}}}' ;;
  workflows) selection='id name enabled' ;;
  *) return 1 ;;
  esac
  query_text="query(\$p:ID!,\$after:String){node(id:\$p){... on ProjectV2{id $connection(first:100,after:\$after){nodes{$selection} pageInfo{hasNextPage endCursor}}}}}"
  while [ "$more" = true ]; do
    pages=$((pages + 1))
    [ "$pages" -le 100 ] || return 1
    out=$(gh api graphql -f query="$query_text" -f p="$project" -f after="$cursor") || return 1
    printf '%s' "$out" | jq -e --arg p "$project" --arg c "$connection" '
      ((.errors // []) | length) == 0 and .data.node.id == $p and
      (.data.node[$c].nodes | type) == "array" and
      (.data.node[$c].pageInfo.hasNextPage | type) == "boolean"' >/dev/null || return 1
    if [ "$connection" = views ]; then
      printf '%s' "$out" | jq -e 'all(.data.node.views.nodes[]; .configuration.visibleFields.pageInfo.hasNextPage == false)' >/dev/null || return 1
    fi
    result=$(printf '%s' "$out" | jq -c --arg c "$connection" --argjson previous "$result" '$previous + .data.node[$c].nodes') || return 1
    more=$(printf '%s' "$out" | jq -r --arg c "$connection" '.data.node[$c].pageInfo.hasNextPage')
    cursor=$(printf '%s' "$out" | jq -r --arg c "$connection" '.data.node[$c].pageInfo.endCursor // empty')
    if [ "$more" = true ]; then
      [ -n "$cursor" ] || return 1
      case $seen in *"|$cursor|"*) return 1 ;; esac
      seen="$seen$cursor|"
    fi
  done
  printf '%s\n' "$result"
}

schema_fetch() {
  schema_fields=$(connection_read fields) || return 1
  schema_views=$(connection_read views) || return 1
  schema_workflows=$(connection_read workflows) || return 1
}

schema_print() {
  printf '%s' "$metadata" | jq --argjson fields "$schema_fields" --argjson views "$schema_views" --argjson workflows "$schema_workflows" '{
    repository: .data.repository.nameWithOwner, project: .data.node,
    fields: $fields, views: $views, workflows: $workflows,
    automation: {configuration: "UI verification required", workflowMutations: false},
    viewOrdering: "Set Priority ascending and milestone grouping in the UI"
  }'
}

schema_mutate() {
  local request out
  request=$(mktemp "$dir/.board-request.XXXXXX") || return 1
  if ! jq -n --arg query "$1" --argjson input "$2" '{query:$query,variables:{input:$input}}' >"$request"; then
    rm -f "$request"; return 1
  fi
  out=$(gh api graphql --input "$request") || { rm -f "$request"; return 1; }
  rm -f "$request"
  printf '%s' "$out" | jq -e --arg operation "$3" --arg result "$4" '((.errors // []) | length) == 0 and (.data[$operation][$result].id | type) == "string"' >/dev/null
}

if [ "$mode" = metadata ]; then
  schema_fetch || die "could not read complete schema metadata"
  schema_print
  exit 0
fi

if [ "$mode" = configure ] || [ "$mode" = configure-fields ]; then
  [[ $project =~ ^PVT_[A-Za-z0-9_-]+$ ]] || die "invalid resolved project ID"
  lock=$cache_root/.board-configure-$project.lock
  configure_lock_take || die "configuration lock is busy (an incomplete lock or abandoned guard requires manual inspection)"
  trap 'configure_lock_release' EXIT
  if [ "$mode" = configure-fields ]; then
    schema_fields=$(connection_read fields) || die "could not read complete field metadata; no configuration changed"
    schema_views='[]' schema_workflows='[]'
  else
    schema_fetch || die "could not read complete schema metadata; no configuration changed"
  fi
  desired=$(jq -cn --arg prefix "$lane_prefix" --arg roles "$roles" --argjson fields "$schema_fields" '{
    Status:["Todo","In progress","Blocked","In review","Ready to push","Done"],
    Priority:["P1","P2","P3"],
    Session:($roles | split(",") | map($prefix + "/" + .))
  } | if $prefix == "wh" or any($fields[]; .name == "Session" and any(.options[]?; .name == "Werner")) then .Session += ["Werner"] else . end')
  normalized_fields=$(printf '%s' "$schema_fields" | jq -c --arg prefix "$lane_prefix" 'map(if .name == "Session" then
    .options |= map(if (.name | startswith("wh/")) then .name = ($prefix + "/" + (.name | ltrimstr("wh/"))) else . end)
    else . end)')
  printf '%s' "$normalized_fields" | jq -e --argjson desired "$desired" '
    . as $fields | all(["Title","Assignees","Milestone","Status"][]; . as $name | [$fields[] | select(.name == $name)] | length == 1) and
    all(["Status","Priority","Session"][]; . as $name |
      [$fields[] | select(.name == $name)] as $matches | ($matches | length) <= 1 and
      all($matches[]; .dataType == "SINGLE_SELECT" and
        ([.options[].name] | length) == ([.options[].name] | unique | length) and
        all(.options[]; .name as $option | $desired[$name] | index($option) != null)))' >/dev/null || die "schema conflicts with required fields/options; resolve manually before configuration"
  printf '%s' "$schema_views" | jq -e 'group_by(.name) | all(.[]; length == 1)' >/dev/null || die "duplicate view names; resolve manually before configuration"
  printf '%s' "$schema_fields" | jq -e 'all(.[]; if .name == "Title" then .dataType == "TITLE" elif .name == "Assignees" then .dataType == "ASSIGNEES" elif .name == "Milestone" then .dataType == "MILESTONE" else true end)' >/dev/null || die "built-in field types do not match"
  rm -f "$fields"
  for name in Status Priority Session; do
    current=$(printf '%s' "$schema_fields" | jq -c --arg name "$name" '[.[] | select(.name == $name)][0] // {}')
    options=$(printf '%s' "$current" | jq -c --arg prefix "$lane_prefix" --arg name "$name" --argjson desired "$desired" '
      (if $name == "Session" then .options |= ((. // []) | map(if (.name | startswith("wh/")) then .name = ($prefix + "/" + (.name | ltrimstr("wh/"))) else . end)) else . end)
      | . as $field | $desired[$name] | map(. as $option |
      ([$field.options[]? | select(.name == $option)][0] // {name:$option,color:"GRAY",description:""}) | {name,color,description} + (if .id then {id} else {} end))')
    if [ "$(printf '%s' "$current" | jq -c '[.options[]?.name]')" = "$(printf '%s' "$desired" | jq -c --arg name "$name" '.[$name]')" ]; then continue; fi
    fid=$(printf '%s' "$current" | jq -r '.id // empty')
    if [ -n "$fid" ]; then
      input=$(jq -cn --arg id "$fid" --argjson options "$options" '{fieldId:$id,singleSelectOptions:$options}')
      schema_mutate 'mutation($input:UpdateProjectV2FieldInput!){updateProjectV2Field(input:$input){projectV2Field{... on ProjectV2FieldCommon{id}}}}' "$input" updateProjectV2Field projectV2Field || die "configuration may be partial: $name update failed; read metadata schema before retry"
    else
      input=$(jq -cn --arg p "$project" --arg name "$name" --argjson options "$options" '{projectId:$p,name:$name,dataType:"SINGLE_SELECT",singleSelectOptions:$options}')
      schema_mutate 'mutation($input:CreateProjectV2FieldInput!){createProjectV2Field(input:$input){projectV2Field{... on ProjectV2FieldCommon{id}}}}' "$input" createProjectV2Field projectV2Field || die "configuration may be partial: $name creation failed; read metadata schema before retry"
    fi
  done
  schema_fields=$(connection_read fields) || die "configuration may be partial: field readback failed"
  printf '%s' "$schema_fields" | jq -e --argjson desired "$desired" '. as $fields | all(["Status","Priority","Session"][]; . as $name | [$fields[] | select(.name == $name)] | length == 1 and (.[0].options | map(.name)) == $desired[$name])' >/dev/null || die "configuration readback does not match required options"
  if [ "$mode" = configure-fields ]; then
    schema_views=null schema_workflows=null
    schema_print
    exit 0
  fi
  visible=$(printf '%s' "$schema_fields" | jq -c '. as $fields | ["Title","Status","Priority","Assignees","Session","Milestone"] | map(. as $name | [$fields[] | select(.name == $name)][0].id)')
  printf '%s' "$visible" | jq -e 'all(.[]; type == "string")' >/dev/null || die "required field missing from readback"
  for name in "Dispatch queue" "Active work" "Review queue" "Blocked work" "Release and milestones"; do
    case $name in
    "Dispatch queue") status=Todo ;;
    "Active work") status="In progress" ;;
    "Review queue") status="In review" ;;
    "Blocked work") status=Blocked ;;
    "Release and milestones") status="Ready to push" ;;
    esac
    filter="repo:$repository is:issue -is:closed status:\"$status\""
    current=$(printf '%s' "$schema_views" | jq -c --arg name "$name" '[.[] | select(.name == $name)][0] // {}')
    vid=$(printf '%s' "$current" | jq -r '.id // empty')
    if [ -z "$vid" ]; then
      input=$(jq -cn --arg p "$project" --arg name "$name" --argjson visible "$visible" '{projectId:$p,name:$name,layout:"TABLE_LAYOUT",configuration:{visibleFieldIds:$visible}}')
      schema_mutate 'mutation($input:CreateProjectV2ViewInput!){createProjectV2View(input:$input){projectV2View{id}}}' "$input" createProjectV2View projectV2View || die "configuration may be partial: $name creation failed; read metadata schema before retry"
      schema_views=$(connection_read views) || die "configuration may be partial: view readback failed"
      vid=$(printf '%s' "$schema_views" | jq -er --arg name "$name" '[.[] | select(.name == $name)] | select(length == 1) | .[0].id') || die "created view missing from readback"
    elif printf '%s' "$current" | jq -e --arg filter "$filter" --argjson visible "$visible" '.filter == $filter and .layout == "TABLE_LAYOUT" and ([.configuration.visibleFields.nodes[].id] == $visible)' >/dev/null; then continue
    fi
    input=$(jq -cn --arg id "$vid" --arg filter "$filter" --argjson visible "$visible" '{viewId:$id,filter:$filter,layout:"TABLE_LAYOUT",configuration:{visibleFieldIds:$visible}}')
    schema_mutate 'mutation($input:UpdateProjectV2ViewInput!){updateProjectV2View(input:$input){projectV2View{id}}}' "$input" updateProjectV2View projectV2View || die "configuration may be partial: $name update failed; read metadata schema before retry"
  done
  schema_fetch || die "configuration may be partial: final metadata readback failed"
  printf '%s' "$schema_fields" | jq -e --argjson desired "$desired" '. as $fields | all(["Status","Priority","Session"][]; . as $name | [$fields[] | select(.name == $name)] | length == 1 and (.[0].options | map(.name)) == $desired[$name])' >/dev/null || die "configuration readback does not match required options"
  printf '%s' "$schema_views" | jq -e --arg repo "$repository" --argjson visible "$visible" '. as $views |
    {"Dispatch queue":"Todo","Active work":"In progress","Review queue":"In review","Blocked work":"Blocked","Release and milestones":"Ready to push"} | to_entries |
    all(.[]; . as $expected | [$views[] | select(.name == $expected.key)] | length == 1 and
      .[0].filter == ("repo:" + $repo + " is:issue -is:closed status:\"" + $expected.value + "\"") and .[0].layout == "TABLE_LAYOUT" and
      ([.[0].configuration.visibleFields.nodes[].id] == $visible))' >/dev/null || die "configuration readback does not match required views"
  schema_print
  exit 0
fi

case $mode in
move | session | priority | add | ready)
  rate_warn
  failed=0 done_nums=() done_json="[]"
  if [ "$mode" != add ]; then
    case $mode in
    move) field=Status key=status ;;
    ready) field=Status key=status value="Ready to push" ;;
    session) field=Session key=session ;;
    priority) field=Priority key=priority ;;
    esac
    ids=$(field_ids "$field" "$value") || die "could not find the $field field or the option \"$value\" on the board; the cache is unchanged"
    fid=${ids%% *} oid=${ids#* }
  fi
  seen=" "
  for n in "${nums[@]}"; do
    case $seen in *" $n "*) continue ;; esac
    seen="$seen$n "
    if [ "$mode" = add ]; then
      # Two calls: the issue's node ID and title, then addProjectV2ItemById.
      info=$(gh api graphql -f query='query($n:Int!,$owner:String!,$repo:String!){repository(owner:$owner,name:$repo){issue(number:$n){id title}}}' -f owner="${repository%%/*}" -f repo="${repository#*/}" -F n="$n") || {
        echo "board-snapshot: #$n: GitHub refused the add" >&2; failed=1; continue; }
      cid=$(printf '%s' "$info" | jq -r '.data.repository.issue.id // empty')
      title=$(printf '%s' "$info" | jq -r '.data.repository.issue.title // empty')
      [ -n "$cid" ] || { echo "board-snapshot: #$n: issue not found" >&2; failed=1; continue; }
      gh api graphql -f query='mutation($p:ID!,$c:ID!){addProjectV2ItemById(input:{projectId:$p,contentId:$c}){item{id}}}' -f p="$project" -f c="$cid" >/dev/null || {
        echo "board-snapshot: #$n: GitHub refused the add" >&2; failed=1; continue; }
      done_json=$(printf '%s' "$done_json" | jq -c --argjson n "$n" --arg t "$title" '. + [{n: $n, title: $t}]')
    else
      info=$(gh api graphql -f query='query($n:Int!,$owner:String!,$repo:String!){repository(owner:$owner,name:$repo){issue(number:$n){projectItems(first:100){pageInfo{hasNextPage} nodes{id project{id}}}}}}' -f owner="${repository%%/*}" -f repo="${repository#*/}" -F n="$n") || {
        echo "board-snapshot: #$n: GitHub refused the lookup" >&2; failed=1; continue; }
      item=$(printf '%s' "$info" | jq -r --arg p "$project" '[.data.repository.issue.projectItems.nodes[]? | select(.project.id == $p) | .id][0] // empty')
      [ -n "$item" ] || { echo "board-snapshot: #$n: not on the board (use: board-snapshot.sh add $n)" >&2; failed=1; continue; }
      gh api graphql -f query='mutation($p:ID!,$i:ID!,$f:ID!,$o:String!){updateProjectV2ItemFieldValue(input:{projectId:$p,itemId:$i,fieldId:$f,value:{singleSelectOptionId:$o}}){projectV2Item{id}}}' \
        -f p="$project" -f i="$item" -f f="$fid" -f o="$oid" >/dev/null || {
        echo "board-snapshot: #$n: GitHub refused the write" >&2; failed=1; continue; }
      done_nums+=("$n")
    fi
  done
  # One cache patch for every issue that succeeded.
  if [ "$mode" = add ]; then
    [ "$done_json" = "[]" ] ||
      patch '.items |= (reduce $adds[] as $a (.; if any(.[]; .number == $a.n) then . else . + [{number: $a.n, title: (if $a.title == "" then null else $a.title end), status: null, session: null, priority: null, labels: [], type: "Issue", url: ("https://github.com/" + $repo + "/issues/" + ($a.n | tostring))}] end))' \
        --arg repo "$repository" --argjson adds "$done_json"
  elif [ ${#done_nums[@]} -gt 0 ]; then
    nj=$(printf '%s\n' "${done_nums[@]}" | jq -sc 'map(tonumber)')
    patch '.items |= map(if (.number as $x | $ns | index($x)) != null then .[$k] = $v else . end)' \
      --argjson ns "$nj" --arg k "$key" --arg v "$value"
  fi
  [ "$failed" = 0 ] || die "some issues failed (the cache holds only the ones that succeeded)"
  exit 0
  ;;
esac

if [ "$mode" = budget ]; then
  [ -f "$budget_log" ] || { echo "no refresh logged yet"; exit 0; }
  jq -Rnr --argjson now "$(date +%s)" '
    [inputs | fromjson? | select(type == "object" and (.at | numbers) and .at >= $now - 86400)] as $r
    | if ($r | length) == 0 then "no refresh logged in the last 24 hours"
      else "refreshes: \($r | length), total cost: \($r | map(.cost) | add), lowest remaining: \($r | map(.remaining) | min) of \($r[-1].limit)" end' "$budget_log"
  exit 0
fi

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
      if matches_target; then
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
matches_target || die "snapshot target identity mismatch"

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
