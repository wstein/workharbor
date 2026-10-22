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
# its own pre-releases) and COMMIT, grouped by Conventional Commit type, or just "First release."
# when no earlier release tag exists (the first release is v0.1.0).

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
section() { # section TITLE ERE
	lines=$(printf '%s\n' "$subjects" | grep -E "$2" || true)
	if [ -n "$lines" ]; then
		printf '### %s\n\n' "$1"
		printf '%s\n' "$lines" | sed 's/^/- /'
		printf '\n'
	fi
}
section Features '^feat(\(.+\))?!?:'
section Fixes '^fix(\(.+\))?!?:'
section Documentation '^docs(\(.+\))?!?:'
rest=$(printf '%s\n' "$subjects" | grep -Ev '^(feat|fix|docs)(\(.+\))?!?:' || true)
if [ -n "$rest" ]; then
	printf '### Other\n\n'
	printf '%s\n' "$rest" | sed 's/^/- /'
fi
