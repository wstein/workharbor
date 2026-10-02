#!/usr/bin/env bash
# Print the commit a release tag points to, and fail unless it is the commit the
# workflow run is for. The release job builds github.sha, so a v* tag that was
# re-pushed to another commit between the jobs must not be verified or released.
#
#   TAG=<tag> GITHUB_SHA=<40 hex> scripts/verify-tag-commit.sh
#
# The tag is peeled (^{commit}), so an annotated tag works. The commit goes to
# stdout, the rest to stderr.
set -euo pipefail

tag="${TAG:-}"
run_sha="${GITHUB_SHA:-}"

fail() { echo "::error::$*" >&2; exit 1; }

[ -n "$tag" ] || fail "TAG is not set"
[[ "$run_sha" =~ ^[0-9a-f]{40}$ ]] || fail "GITHUB_SHA must be 40 lowercase hex characters"

resolved="$(git rev-parse --verify --quiet "refs/tags/$tag^{commit}")" \
  || fail "tag $tag does not resolve to a commit"
[ "$resolved" = "$run_sha" ] \
  || fail "tag $tag now points at $resolved but this run is for $run_sha (github.sha): the tag was moved after the run started"
echo "$resolved"
