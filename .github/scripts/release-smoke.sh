#!/bin/sh
# SPDX-License-Identifier: EUPL-1.2
#
# Exercise a shipped release archive on its host (docs/releasing.md).
#
# Usage: sh release-smoke.sh VERSION ARCHIVE [COMMIT]
#
# ARCHIVE is whr_<version>_<os>_<arch>.tar.gz next to its .sha256 sidecar. Run in the
# repository root: the expected third_party/ members are fixed below. Checks, in order: the
# checksum; the archive members; that `whr version` and `whr --version` print "whr VERSION"
# (VERSION without the leading v) and that `whr version --json` reports the same version;
# `whr --help`; and, when COMMIT is given, that the stamped revision is COMMIT. whr runs with
# an empty HOME and no network; nothing else is started (no Docker, no workspace). Works with
# POSIX tools on Linux and macOS.

set -eu

fail() {
	printf 'release-smoke: %s\n' "$*" >&2
	exit 1
}

[ "$#" -eq 2 ] || [ "$#" -eq 3 ] || {
	printf 'usage: sh release-smoke.sh VERSION ARCHIVE [COMMIT]\n' >&2
	exit 2
}
version=$1
archive=$2
commit=${3:-}
plain=${version#v}

[ -f "$archive" ] || fail "archive not found: $archive"
archive=$(cd "$(dirname "$archive")" && pwd)/$(basename "$archive")
file=$(basename "$archive")
case $file in
*.tar.gz) name=${file%.tar.gz} ;;
*) fail "not a .tar.gz: $file" ;;
esac

# Checksum written at build time (sha256sum on Linux, shasum on macOS).
[ -f "$archive.sha256" ] || fail "checksum sidecar missing: $file.sha256"
if command -v sha256sum >/dev/null 2>&1; then
	(cd "$(dirname "$archive")" && sha256sum -c "$file.sha256" >/dev/null) || fail "checksum mismatch for $file"
else
	(cd "$(dirname "$archive")" && shasum -a 256 -c "$file.sha256" >/dev/null) || fail "checksum mismatch for $file"
fi

# Physical path: macOS TMPDIR is under /var -> /private/var.
d=$(mktemp -d) || fail "cannot create a temp dir"
trap 'rm -rf "$d"' EXIT
tmp=$(cd "$d" && pwd -P) || fail "cannot resolve the temp dir"
tar -xzf "$archive" -C "$tmp" || fail "cannot extract $file"

members=$(cd "$tmp" && find . -mindepth 1 | LC_ALL=C sort | tr '\n' ' ')
expected=$(
	printf '%s\n' "./$name" "./$name/whr" "./$name/LICENSE" "./$name/README.md" "./$name/third_party" \
		"./$name/third_party/README.md" "./$name/third_party/cobra" "./$name/third_party/cobra/LICENSE" \
		"./$name/third_party/cobra/PROVENANCE.md" "./$name/third_party/pflag" "./$name/third_party/pflag/LICENSE" \
		"./$name/third_party/x-sys" "./$name/third_party/x-sys/LICENSE" "./$name/third_party/x-sys/PATENTS" \
		"./$name/third_party/x-sys/PROVENANCE.md" | LC_ALL=C sort | tr '\n' ' '
)
[ "$members" = "$expected" ] || fail "unexpected archive members: $members"
bin=$tmp/$name/whr
[ -x "$bin" ] || fail "whr is not executable"

mkdir "$tmp/home"
whr() { env -i PATH="$PATH" HOME="$tmp/home" "$bin" "$@"; }

got=$(whr version | sed -n 1p) || fail "whr version failed"
[ "$got" = "whr $plain" ] || fail "whr version printed '$got', want 'whr $plain'"
got=$(whr --version | sed -n 1p) || fail "whr --version failed"
[ "$got" = "whr $plain" ] || fail "whr --version printed '$got', want 'whr $plain'"
json=$(whr version --json | tr -d ' \n') || fail "whr version --json failed"
case $json in
*"\"version\":\"$plain\""*) ;;
*) fail "whr version --json printed '$json', want version $plain" ;;
esac
whr --help | grep -q 'Usage:' || fail "whr --help printed no usage"
if [ -n "$commit" ]; then
	whr version | grep -q "^revision: $commit\$" || fail "whr version does not report revision $commit"
fi
printf 'release-smoke: %s ok\n' "$file"
