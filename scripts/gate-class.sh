#!/bin/sh
# Class of a change for the gate (#411). Reads NUL-separated paths on stdin
# (git diff --name-only --no-renames -z) and prints `ordinary` or `carve-out`.
# A path containing a newline (or byte 0x01) fails closed to carve-out, because
# path-class.sh is newline-based and would see allow-listed fragments. Other
# paths are passed to the path-class.sh next to this script.
set -u
dir=$(dirname "$0")
# Map newline inside a path to 0x01 and the NUL separator to newline.
lines=$(LC_ALL=C tr '\n\0' '\001\n'; printf x)
lines=${lines%x}
case "$lines" in
*"$(printf '\001')"*)
  echo carve-out
  exit 0
  ;;
esac
printf '%s' "$lines" | sh "$dir/path-class.sh"
