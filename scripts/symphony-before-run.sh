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
make check
