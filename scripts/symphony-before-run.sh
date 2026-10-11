#!/usr/bin/env bash
set -euo pipefail
# A retry keeps its current branch and working tree; never reset or clean here.
branch=$(git symbolic-ref --quiet --short HEAD) || {
  echo 'Detached checkout: choose an issue branch before starting.' >&2
  exit 1
}
if [ "$branch" = main ]; then
  key=$(basename "$PWD")
  case "$key" in
    ''|*[!A-Za-z0-9._-]*) echo 'Invalid issue workspace key.' >&2; exit 1 ;;
  esac
  git switch -c "work/$key"
fi
# Quality failures are repair input, not a dispatch prerequisite.
# Hook output supplies the diagnosis; handoff still requires passing checks.
if make check; then
  echo 'Pre-run quality checks passed.'
else
  status=$?
  echo "Pre-run quality checks failed (exit $status); repair these failures and rerun make check before handoff." >&2
fi
