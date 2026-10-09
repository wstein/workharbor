#!/usr/bin/env bash
# Run the pinned prebuilt Hugo extended release: scripts/hugo.sh <hugo args>.
#
# The release asset is downloaded only when the cache lacks it, checked against
# .config/hugo.sha256 with shasum -a 256 -c before anything is unpacked, and the
# binary is never run unverified. Compiling Hugo through `go run` filled the Go
# build cache with gigabytes (issue #468).
#
# HUGO_CACHE_DIR overrides the cache (default: .cache/hugo, git-ignored) and
# HUGO_BASE_URL the download location (tests use a file:// URL).
set -euo pipefail

version=0.165.0
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cache=${HUGO_CACHE_DIR:-$root/.cache/hugo}
base=${HUGO_BASE_URL:-https://github.com/gohugoio/hugo/releases/download/v$version}
sums=$root/.config/hugo.sha256

case "$(uname -s)-$(uname -m)" in
Linux-x86_64) asset=hugo_extended_${version}_linux-amd64.tar.gz ;;
Linux-aarch64 | Linux-arm64) asset=hugo_extended_${version}_linux-arm64.tar.gz ;;
Darwin-*) asset=hugo_extended_${version}_darwin-universal.pkg ;;
*)
  echo "hugo.sh: no pinned Hugo asset for $(uname -s)-$(uname -m)" >&2
  exit 1
  ;;
esac

# A cached binary is trusted on -x alone: the cache is a local directory the user owns.
bin=$cache/v$version/hugo
if [ ! -x "$bin" ]; then
  line=$(grep -F "  $asset" "$sums") || {
    echo "hugo.sh: $asset has no checksum in $sums" >&2
    exit 1
  }
  mkdir -p "$cache"
  tmp=$(mktemp -d "$cache/tmp.XXXXXX")
  trap 'rm -rf "$tmp"' EXIT
  proto=(--proto '=https' --tlsv1.2)
  [ -z "${HUGO_BASE_URL:-}" ] || proto=()
  # An empty array is unbound under set -u in bash 3.2 (macOS), hence the ${proto[@]+...} form.
  curl ${proto[@]+"${proto[@]}"} -fsSL --retry 3 -o "$tmp/$asset" "$base/$asset"
  (cd "$tmp" && printf '%s\n' "$line" | shasum -a 256 -c - >&2)
  mkdir "$tmp/out"
  case $asset in
  *.tar.gz) tar -xzf "$tmp/$asset" -C "$tmp/out" hugo ;;
  *.pkg)
    pkgutil --expand-full "$tmp/$asset" "$tmp/pkg" >/dev/null
    cp "$tmp/pkg/Payload/hugo" "$tmp/out/hugo"
    ;;
  esac
  chmod 0755 "$tmp/out/hugo"
  mkdir -p "$cache/v$version"
  mv "$tmp/out/hugo" "$bin"
  rm -rf "$tmp"
  trap - EXIT
fi
exec "$bin" "$@"
