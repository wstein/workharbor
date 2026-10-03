#!/bin/sh
# Print the messages gitleaks cannot see: `gitleaks git` scans patches only, so
# a secret in a commit message or an annotated tag's message passes it (#196).
# Usage: scripts/messages.sh <tip or ""> <git log arguments...>
# Prints the message of every commit git log selects, the message of the
# annotated tag at <tip> (and of any tag it points to), and, for --all, of every
# annotated tag in refs/tags. A failure of git is a failure of the script, and
# the caller must treat it as a scan that could not run: no git call here sits
# in a pipeline (a pipe keeps only the last status under sh), each reads into a
# file or a variable and is checked by set -e.
# A commit is printed whole, headers included: a merge of a signed tag copies
# the tag's message into a mergetag header.
# The objects are read raw: a replace ref (refs/replace) must not hide the
# original message that `git push` still sends, and %B would stop at a NUL byte
# while the text after it still goes out.
set -eu
GIT_NO_REPLACE_OBJECTS=1
export GIT_NO_REPLACE_OBJECTS
tip=$1
shift
d=$(mktemp -d)
trap 'rm -rf "$d"' EXIT
trap 'exit 1' HUP INT TERM
tag_chain() { # the messages of a tag object and of the tags it points to
  s=$1
  while :; do
    kind=$(git cat-file -t "$s")
    [ "$kind" = tag ] || break
    git cat-file tag "$s" >"$d/obj"
    cat "$d/obj"
    s=$(sed -n 's/^object //p;q' "$d/obj")
    [ -n "$s" ] || { echo "messages.sh: a tag object without an object line" >&2; exit 1; }
  done
}
git rev-list "$@" >"$d/commits"
while read -r c; do
  git cat-file commit "$c" >"$d/obj"
  cat "$d/obj" # the whole object: mergetag and gpgsig headers go out with a push too
done <"$d/commits"
[ -z "$tip" ] || tag_chain "$tip"
for a in "$@"; do
  if [ "$a" = --all ]; then
    git for-each-ref --format='%(objectname)' refs/tags >"$d/tags"
    while read -r t; do tag_chain "$t"; done <"$d/tags"
  fi
done
