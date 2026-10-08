#!/bin/sh
# Show, plan, apply or restore the reviewed ruleset for main (#413). The HUMAN
# runs this; an agent never does anything but `show` and `plan`.
#
#   scripts/ruleset-apply.sh show
#   scripts/ruleset-apply.sh plan  [--stage active|previous]
#   scripts/ruleset-apply.sh apply [--stage active|previous] [--backup-dir DIR] [--apply]
#   scripts/ruleset-apply.sh backup [--backup-dir DIR]   (read-only on GitHub; apply does it first)
#   scripts/ruleset-apply.sh restore <backup-file> [--apply]
#
# Stages are the committed payloads in .github/rulesets/: active (default, the
# target) and previous (the three old rules). plan, apply and restore are dry
# runs by default. The only mutating call is `gh api -X PUT`, made when --apply
# is given AND a terminal answers the typed confirmation. There is no
# environment variable that skips either. Before apply writes anything it saves
# the live ruleset as a new timestamped file (never overwritten) in --backup-dir,
# default ${XDG_STATE_HOME:-$HOME/.local/state}/workharbor, and prints its path.
# Needs: gh (authenticated), jq, diff.
set -eu

repo="wstein/workharbor"
dir=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
stage="active"
do_apply=0
backup_dir=""
backup_file=""

die() { echo "ruleset-apply: $*" >&2; exit 1; }

[ "$#" -ge 1 ] || die "usage: ruleset-apply.sh show|plan|apply|restore <file> [--stage active|previous] [--backup-dir DIR] [--apply]"
cmd="$1"
shift
case "$cmd" in
show | plan | apply | backup) ;;
restore)
  [ "$#" -ge 1 ] || die "restore needs a backup file"
  backup_file="$1"
  shift
  ;;
*) die "unknown subcommand: $cmd" ;;
esac
while [ "$#" -gt 0 ]; do
  case "$1" in
  --apply) do_apply=1 ;;
  --dry-run) do_apply=0 ;;
  --stage)
    [ "$#" -ge 2 ] || die "--stage needs a value"
    stage="$2"
    shift
    ;;
  --backup-dir)
    [ "$#" -ge 2 ] || die "--backup-dir needs a value"
    backup_dir="$2"
    shift
    ;;
  *) die "unknown argument: $1" ;;
  esac
  shift
done
case "$stage" in
active | previous) ;;
*) die "unknown stage: $stage" ;;
esac
if [ "$do_apply" = 1 ] && [ "$cmd" != apply ] && [ "$cmd" != restore ]; then
  die "--apply is only valid with apply or restore"
fi

if [ "$cmd" = restore ]; then
  payload="$backup_file"
  [ -f "$payload" ] || die "no such backup file: $payload"
  label="backup $(basename -- "$payload")"
  stage="backup"
else
  payload="$dir/.github/rulesets/main.$stage.json"
  [ -f "$payload" ] || die "missing payload $payload"
  label="target $stage"
fi
id=$(jq -r '.id' "$payload") || die "cannot parse $payload"
case "$id" in '' | *[!0-9]*) die "payload has no numeric id" ;; esac

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
norm='{name, target, enforcement, conditions, bypass_actors, rules}'

jq -S "$norm" "$payload" >"$tmp/target.json"
gh api "repos/$repo/rulesets/$id" >"$tmp/live.raw" || die "cannot read the live ruleset $id"
jq -S "$norm" "$tmp/live.raw" >"$tmp/live.json" || die "live ruleset is not JSON"

diffs=0
diff -u --label "live ruleset $id" --label "$label" "$tmp/live.json" "$tmp/target.json" >"$tmp/diff.txt" || diffs=1

save_backup() {
  : "${backup_dir:=${XDG_STATE_HOME:-${HOME:?HOME is not set}/.local/state}/workharbor}"
  mkdir -p -- "$backup_dir"
  saved="$backup_dir/ruleset-$id-$(date -u +%Y%m%dT%H%M%SZ).json"
  (set -C && jq -S . "$tmp/live.raw" >"$saved") || die "cannot create a new backup file $saved (never overwritten); nothing changed"
  echo "backup saved: $saved"
  echo "restore with: scripts/ruleset-apply.sh restore $saved --apply"
}

if [ "$cmd" = backup ]; then
  save_backup
  exit 0
fi

note_integration() {
  if jq -e '.rules[]? | select(.type == "required_status_checks") | .parameters.required_status_checks[] | select(.integration_id != null)' "$payload" >/dev/null 2>&1; then
    echo "note: gate integration_id 15368 (GitHub Actions) was looked up read-only on 2026-10-08 with: gh api /apps/github-actions --jq '{id,slug,name}'; re-check before apply"
  fi
}

if [ "$cmd" = show ]; then
  if [ "$diffs" = 0 ]; then echo "live ruleset $id equals $label"; else cat "$tmp/diff.txt"; fi
  note_integration
  exit 0
fi

echo "would run: gh api -X PUT repos/$repo/rulesets/$id --input <$label without id>"
if [ "$diffs" = 0 ]; then echo "no change: live ruleset already equals $label"; else cat "$tmp/diff.txt"; fi
note_integration
if [ "$do_apply" = 0 ]; then
  [ "$cmd" = apply ] && echo "it would first save the live ruleset as a backup file"
  echo "dry run: nothing was changed (needs: $cmd --apply, on a terminal)"
  exit 0
fi

[ -t 0 ] && [ -t 1 ] || die "refusing to $cmd without a terminal"
want="$cmd $stage to ruleset $id"
printf 'Type "%s" to change the live ruleset: ' "$want"
IFS= read -r answer || die "no answer"
[ "$answer" = "$want" ] || die "confirmation did not match; nothing changed"

[ "$cmd" != apply ] || save_backup
jq "$norm" "$payload" >"$tmp/put.json"
gh api -X PUT "repos/$repo/rulesets/$id" --input "$tmp/put.json" >/dev/null
echo "done ($cmd $stage, ruleset $id); check: scripts/ruleset-apply.sh show"
