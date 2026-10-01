#!/usr/bin/env bash
# Build a shared, read-only tool store on the host, laid out like a tiny Nix store:
#
#   $STORE/store/<hash8>-<name>-<version>-<platform>/bin/<name>   content-addressed, immutable
#   $STORE/profiles/<profile>/bin/<name> -> ../../../store/.../bin/<name>
#
# Downloads are verified against the vendor's SHA-256 manifest where one exists.
set -euo pipefail
STORE=${STORE:?set STORE to the directory that will hold the tool store}
CLAUDE_BASE=https://downloads.claude.ai/claude-code-releases
mkdir -p "$STORE/store" "$STORE/profiles" "$STORE/.tmp"

sha() { shasum -a 256 "$1" | cut -d' ' -f1; }

# add_claude <version> <platform>  e.g. 2.1.286 linux-arm64
add_claude() {
  local ver=$1 plat=$2 tmp="$STORE/.tmp/claude-$1-$2" want got h dir
  want=$(curl -fsSL "$CLAUDE_BASE/$ver/manifest.json" | jq -r ".platforms[\"$plat\"].checksum")
  curl -fsSL "$CLAUDE_BASE/$ver/$plat/claude" -o "$tmp"
  got=$(sha "$tmp")
  [ "$got" = "$want" ] || { echo "checksum mismatch for claude $ver $plat" >&2; return 1; }
  h=${got:0:8}; dir="$STORE/store/$h-claude-$ver-$plat"
  mkdir -p "$dir/bin" && mv "$tmp" "$dir/bin/claude" && chmod 755 "$dir/bin/claude"
  echo "$dir"
}

# add_codex <tag> <version>  (static musl build from the GitHub release)
add_codex() {
  local tag=$1 ver=$2 tmp="$STORE/.tmp/codex" h dir
  rm -rf "$tmp" && mkdir -p "$tmp"
  gh release download "$tag" -R openai/codex -p 'codex-aarch64-unknown-linux-musl.tar.gz' -D "$tmp"
  tar -xzf "$tmp/codex-aarch64-unknown-linux-musl.tar.gz" -C "$tmp"
  local bin; bin=$(find "$tmp" -type f -name 'codex-aarch64-unknown-linux-musl' | head -1)
  h=$(sha "$bin"); h=${h:0:8}; dir="$STORE/store/$h-codex-$ver-linux-arm64-musl"
  mkdir -p "$dir/bin" && mv "$bin" "$dir/bin/codex" && chmod 755 "$dir/bin/codex"
  rm -rf "$tmp"
  echo "$dir"
}

# link <profile> <store dir>...: add each tool of a store dir to a profile
link() {
  local prof=$1; shift
  mkdir -p "$STORE/profiles/$prof/bin"
  for d in "$@"; do
    for f in "$d"/bin/*; do
      ln -sfn "../../../store/$(basename "$d")/bin/$(basename "$f")" "$STORE/profiles/$prof/bin/$(basename "$f")"
    done
  done
}

CL_NEW=$(add_claude 2.1.286 linux-arm64)
CL_NEW_MUSL=$(add_claude 2.1.286 linux-arm64-musl)
CL_OLD=$(add_claude 2.1.285 linux-arm64)
CX=$(add_codex rust-v0.159.3 0.159.3)
link default   "$CL_NEW" "$CX"
link musl      "$CL_NEW_MUSL" "$CX"
link pinned    "$CL_OLD" "$CX"
rm -rf "$STORE/.tmp"
echo "--- store:"; ls -1 "$STORE/store"; du -sh "$STORE/store" | cut -f1 | sed 's/^/total: /'
echo "--- profiles:"; ls -l "$STORE"/profiles/*/bin/* | awk '{print $9, $10, $11}' | sed "s|$STORE/||"
