#!/usr/bin/env bash
# Install whr, whr-shim and whr-proxy from a release of this repository, a
# draft included (design D24, D34, §13 Releases). Run it as the administrator:
# a draft is downloadable only by a repository writer, and PREFIX should be one
# the whr user cannot write.
#
#   scripts/install-release.sh <tag> [prefix]
#
# It downloads the macOS archive, the guest archive and checksums.txt with gh,
# checks both archives against checksums.txt and against the build-provenance
# attestation of this repository's release workflow, and installs nothing unless
# every check passes. WHR_RELEASE_DIR names a folder that already holds the three
# files, to skip the download; the checks still run.
set -euo pipefail

repo="${WHR_RELEASE_REPO:-wstein/workharbor}"
tag="${1:-}"
prefix="${2:-/opt/whr}"

die() {
  echo "install-release: $*" >&2
  exit 1
}

[[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || die "usage: $0 vX.Y.Z[-pre] [prefix] (got '$tag')"
[[ "$prefix" = /* ]] || die "the prefix must be an absolute path (got '$prefix')"
[ "$(uname -s)/$(uname -m)" = Darwin/arm64 ] || die "whr releases are for macOS on Apple silicon"
command -v gh >/dev/null || die "needs gh (brew install gh), signed in as a writer of $repo"

version="${tag#v}"
mac="whr_${version}_darwin_arm64.tar.gz"
guest="whr-guest_${version}_linux_arm64.tar.gz"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

if [ -n "${WHR_RELEASE_DIR:-}" ]; then
  for f in "$mac" "$guest" checksums.txt; do
    cp "$WHR_RELEASE_DIR/$f" "$work/" || die "$f is not in $WHR_RELEASE_DIR"
  done
else
  gh release download "$tag" --repo "$repo" --dir "$work" \
    --pattern "$mac" --pattern "$guest" --pattern checksums.txt ||
    die "cannot download $tag from $repo (a draft needs a writer's gh login)"
fi

# Exactly one line per archive, checked with the file names fixed above.
(
  cd "$work"
  for f in "$mac" "$guest"; do
    [ "$(awk -v f="$f" '$2 == f' checksums.txt | wc -l)" -eq 1 ] || die "checksums.txt has no single line for $f"
    awk -v f="$f" '$2 == f' checksums.txt | shasum -a 256 -c - >/dev/null || die "$f does not match checksums.txt"
  done
)

for f in "$mac" "$guest"; do
  gh attestation verify "$work/$f" --repo "$repo" \
    --signer-workflow "$repo/.github/workflows/release.yml" >/dev/null ||
    die "$f has no valid build-provenance attestation from $repo's release workflow"
done

mkdir -p "$work/mac" "$work/guest"
tar -xzf "$work/$mac" -C "$work/mac" whr
tar -xzf "$work/$guest" -C "$work/guest" whr-shim whr-proxy

install -d -m 0755 "$prefix/bin" "$prefix/libexec/whr"
install -m 0755 "$work/mac/whr" "$prefix/bin/whr"
install -m 0755 "$work/guest/whr-shim" "$prefix/libexec/whr/whr-shim-linux-arm64"
install -m 0755 "$work/guest/whr-proxy" "$prefix/libexec/whr/whr-proxy-linux-arm64"

echo "installed whr $("$prefix/bin/whr" version), whr-shim and whr-proxy (linux-arm64) under $prefix" >&2
echo "next, as whr: $prefix/bin/whr tools build -store <tool store> -shim $prefix/libexec/whr/whr-shim-linux-arm64" >&2
