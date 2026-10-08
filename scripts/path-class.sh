#!/bin/sh
# Path class of a change (#410, design pr-flow-landing "Class logic and its single copy").
# Reads changed paths on stdin, ONE PER LINE (newline-separated, like land.sh's
# `git diff --name-only -z | tr '\0' '\n'`), and prints `ordinary` or `carve-out`.
# A path that itself contains a newline therefore arrives as several fragments;
# a fragment matches no allow-list entry unless it is one, so it falls to
# carve-out, the safe side. NUL input is not supported. Empty lines are skipped,
# a final line without newline counts, and empty input is `ordinary`.
# Each path is lowercased (ASCII, as before) and matched in order; ordinary is an
# allow-list, carve-outs are matched first. One carve-out path decides the whole
# change. The path is only ever data: never an argument, glob or eval input.
# Run it from the base branch's blob (`git show main:scripts/path-class.sh`), never
# from the PR head, so a change cannot alter the rule applied to itself.
set -u
class=ordinary
while IFS= read -r p || [ -n "$p" ]; do
  [ -n "$p" ] || continue
  # shellcheck disable=SC2018,SC2019 # ASCII-only on purpose, as before
  lp="$(printf '%s' "$p" | LC_ALL=C tr 'A-Z' 'a-z')"
  case "$lp" in
  agents.md | */agents.md | claude.md | */claude.md | .claude/* | */.claude/* | .agents/* | */.agents/* | .github/*) class=carve-out ;;
  docs/content/*design* | docs/content/*threat*) class=carve-out ;;
  internal/exitcode/* | internal/version/* | internal/docscheck/*) ;;
  docs/*.md) ;;
  readme.md | changelog.md | contributing.md | license) ;;
  *) class=carve-out ;;
  esac
done
printf '%s\n' "$class"
