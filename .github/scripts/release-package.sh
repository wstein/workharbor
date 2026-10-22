#!/bin/sh
# SPDX-License-Identifier: EUPL-1.2
#
# Package one release binary (docs/releasing.md).
#
# Usage: SOURCE_DATE_EPOCH=N sh release-package.sh VERSION TARGET BINARY OUTDIR
#
# VERSION is vMAJOR.MINOR.PATCH, optionally with a pre-release suffix (-alpha.N, -beta.N, -rc.N or
# the dry-run suffix -dryrun[.X]*). TARGET is the archive platform, <os>_<arch>, for the shipped
# operating systems linux and darwin only (whr uses Unix file-system and terminal primitives).
# Run in the repository root (LICENSE, README.md and third_party/ are read from there).
# Writes OUTDIR/whr_<version without v>_<target>.tar.gz and its .sha256 sidecar. The archive
# holds one top directory with whr (0755), LICENSE, README.md and the third_party/ notices of
# the modules linked into whr (README.md, cobra, pflag, x-sys; files 0644, directories 0755),
# in sorted order, owner 0, mtime SOURCE_DATE_EPOCH, no gzip name or time, so the same inputs
# always give the same bytes. Needs GNU tar.

set -eu

fail() {
	printf 'release-package: %s\n' "$*" >&2
	exit 1
}

[ "$#" -eq 4 ] || {
	printf 'usage: SOURCE_DATE_EPOCH=N sh release-package.sh VERSION TARGET BINARY OUTDIR\n' >&2
	exit 2
}
version=$1
target=$2
binary=$3
out=$4

case $version$target in
*'
'*) fail "newline in an argument" ;;
esac
printf '%s\n' "$version" | grep -Eq '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-((alpha|beta|rc)\.(0|[1-9][0-9]*)|dryrun(\.[0-9A-Za-z]+)*))?$' ||
	fail "version must look like v1.2.3, v1.2.3-alpha.1 or v1.2.3-dryrun: $version"
printf '%s\n' "$target" | grep -Eq '^(linux|darwin)_[a-z0-9]+$' || fail "bad target (want linux_<arch> or darwin_<arch>): $target"
: "${SOURCE_DATE_EPOCH:?release-package: SOURCE_DATE_EPOCH is required}"
printf '%s\n' "$SOURCE_DATE_EPOCH" | grep -Eq '^[0-9]+$' || fail "SOURCE_DATE_EPOCH must be digits"
[ -f "$binary" ] || fail "binary not found: $binary"
notices="third_party/README.md third_party/cobra third_party/pflag third_party/x-sys"
for f in LICENSE README.md third_party/README.md third_party/cobra/LICENSE third_party/pflag/LICENSE third_party/x-sys/LICENSE; do
	[ -f "$f" ] || fail "$f not found in $(pwd)"
done

tar --version 2>/dev/null | grep -q 'GNU tar' || fail "GNU tar is required"

name=whr_${version#v}_${target}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

mkdir "$work/$name" "$work/$name/third_party"
cp "$binary" "$work/$name/whr"
cp LICENSE README.md "$work/$name/"
for n in $notices; do
	cp -R "$n" "$work/$name/third_party/"
done
find "$work/$name" -type d -exec chmod 0755 {} +
find "$work/$name" -type f -exec chmod 0644 {} +
chmod 0755 "$work/$name/whr"

mkdir -p "$out"
out=$(cd "$out" && pwd)
archive=$out/$name.tar.gz
rm -f "$archive"
tar --sort=name --owner=0 --group=0 --numeric-owner --mtime="@$SOURCE_DATE_EPOCH" \
	--format=ustar -C "$work" -cf - "$name" | gzip -n -9 >"$archive"
(cd "$out" && sha256sum "$name.tar.gz" >"$name.tar.gz.sha256")
printf '%s\n' "$archive"
