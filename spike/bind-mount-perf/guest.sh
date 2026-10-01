#!/bin/sh
# Runs in the guest against $1 (a workspace) and prints one "name seconds" line per measurement.
cd "$1" || exit 1
t() { # name cmd...
  n=$1; shift
  s=$(date +%s.%N); "$@" >/dev/null 2>&1; e=$(date +%s.%N)
  printf '%s %.2f\n' "$n" "$(echo "$e - $s" | bc)"
}
git config --global --add safe.directory '*'
t git_status_cold   git status --porcelain
t git_status_warm1  git status --porcelain
t git_status_warm2  git status --porcelain
t read_all          tar cf /dev/null .
t copy_tree         cp -a . ../copy-of-tree
t remove_tree       rm -rf ../copy-of-tree
if [ -d src ]; then
  t build_j4        sh -c 'ls src/*.c | xargs -P4 -n1 gcc -c -O0 -w -o /dev/null'
  t build_one_by_one sh -c 'for f in src/u1*.c; do gcc -c -O0 -w -o /dev/null $f; done'
fi
