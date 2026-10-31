#!/bin/sh
# SPDX-License-Identifier: EUPL-1.2
#
# Release notes for a tag on stdout (docs/releasing.md). There is no CHANGELOG file: the change
# list is generated from the commit subjects.
#
# Usage: sh release-notes.sh TAG COMMIT GO_VERSION
#
# Run in a repository with full history and tags. The change list is the non-merge commit
# subjects between the nearest earlier release tag (by version order; a final release sorts above
# its own pre-releases) and COMMIT, or just "First release." when no earlier release tag exists
# (the first release is v0.1.0).
#
# Listed: every feat commit (Features), every breaking commit (!) of any other type (Breaking
# changes), every fix commit (Fixes, in a collapsed <details> block) and perf and refactor commits
# (Other changes, only when present). Documentation and all remaining commits (test, style, build,
# ci, chore, revert) are only counted, followed by a link to the full comparison.

set -eu

[ "$#" -eq 3 ] || {
	printf 'usage: sh release-notes.sh TAG COMMIT GO_VERSION\n' >&2
	exit 2
}
tag=$1
commit=$2
gover=$3
plain=${tag#v}

prev=
if git rev-parse -q --verify "$commit^" >/dev/null; then
	prev=$(git -c versionsort.suffix=- tag --merged "$commit^" --list 'v*' --sort=-version:refname |
		grep -E '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-(alpha|beta|rc)\.(0|[1-9][0-9]*))?$' | grep -vx "$tag" | head -n 1 || true)
fi

cat <<TEXT
WorkHarbor $tag

Commit: $commit
Built with Go $gover, CGO disabled, -trimpath, version stamped as $plain.

Archives are named whr_${plain}_<os>_<arch>.tar.gz. Each holds whr, LICENSE, README.md and
the third_party/ license notices of the linked modules.

- Run natively on a GitHub runner before release: linux/amd64, linux/arm64, darwin/arm64.
- Windows, the BSDs and darwin/amd64 are not shipped: whr is built and tested on Linux and
  Apple-silicon macOS only.

Verify a download in the directory that holds the archives and SHA256SUMS:

    sha256sum --check --ignore-missing SHA256SUMS                       # Linux
    grep ' ARCHIVE\$' SHA256SUMS | shasum -a 256 --check               # macOS, one archive
    gh attestation verify ARCHIVE --repo wstein/workharbor

then extract and run \`whr version\`, which prints \`whr $plain\`.

The binaries are not signed with a certificate or notarized. The build provenance
attestation shows that GitHub Actions built the archive from this repository;
SHA256SUMS detects a damaged or wrong download.

TEXT
if [ -z "$prev" ]; then
	printf '## Changes\n\nFirst release.\n'
	exit 0
fi
printf '## Changes since %s\n\n' "$prev"
range=$prev..$commit
subjects=$(git log --no-merges --format='%s (%h)' "$range")
if [ -z "$subjects" ]; then
	printf 'No changes.\n'
	exit 0
fi
pick() { # pick ERE: the subjects matching ERE, empty when none
	printf '%s\n' "$subjects" | grep -E "$1" || true
}
count() { # count LINES: the number of lines, 0 for none
	if [ -z "$1" ]; then
		printf '0'
	else
		printf '%s\n' "$1" | wc -l | tr -d ' '
	fi
}
bullets() { # bullets LINES
	printf '%s\n' "$1" | sed 's/^/- /'
}
feats=$(pick '^feat(\(.+\))?!?:')
breaking=$(pick '^(fix|docs|style|refactor|perf|test|build|ci|chore|revert)(\(.+\))?!:')
fixes=$(pick '^fix(\(.+\))?:')
tidy=$(pick '^(perf|refactor)(\(.+\))?:')
docs=$(pick '^docs(\(.+\))?:')
ndocs=$(count "$docs")
nall=$(count "$subjects")
nother=$((nall - $(count "$feats") - $(count "$breaking") - $(count "$fixes") - $(count "$tidy") - ndocs))
if [ -n "$feats" ]; then
	printf '### Features (%s)\n\n' "$(count "$feats")"
	bullets "$feats"
	printf '\n'
fi
if [ -n "$breaking" ]; then
	printf '### Breaking changes (%s)\n\n' "$(count "$breaking")"
	bullets "$breaking"
	printf '\n'
fi
if [ -n "$fixes" ]; then
	printf '<details><summary>Fixes (%s)</summary>\n\n' "$(count "$fixes")"
	bullets "$fixes"
	printf '\n</details>\n\n'
fi
if [ -n "$tidy" ]; then
	printf '### Other changes (%s)\n\n' "$(count "$tidy")"
	bullets "$tidy"
	printf '\n'
fi
printf 'Not listed here: %s documentation and %s other commits (tests, style, CI, chores). Full comparison: https://github.com/wstein/workharbor/compare/%s...%s\n' "$ndocs" "$nother" "$prev" "$tag"
