#!/bin/sh
# Print the messages gitleaks cannot see: `gitleaks git` scans patches only, so
# a secret in a commit message or an annotated tag's message passes it (#196).
# Usage: scripts/messages.sh <tip or ""> <git log arguments...>
# Prints the message of every commit git log selects, the message of the
# annotated tag at <tip> (and of any tag it points to), and, for --all, of every
# annotated tag in refs/tags. A failure of git is a failure of the script, and
# the caller must treat it as a scan that could not run.
set -eu
tip=$1
shift
tag_chain() { # the messages of a tag object and of the tags it points to
  s=$1
  while [ "$(git cat-file -t "$s")" = tag ]; do
    git cat-file -p "$s"
    s=$(git cat-file -p "$s" | sed -n 's/^object //p;q')
  done
}
git log --format=%B "$@"
[ -z "$tip" ] || tag_chain "$tip"
for a in "$@"; do
  if [ "$a" = --all ]; then
    git for-each-ref --format='%(objectname)' refs/tags | while read -r t; do tag_chain "$t"; done
  fi
done
