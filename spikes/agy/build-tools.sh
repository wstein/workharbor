#!/usr/bin/env bash
# Builds whr-proxy, whr-shim, and downloads Antigravity linux-arm64 into $TOOLS_DIR.
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TOOLS_DIR="${TOOLS_DIR:-$HERE/bin}"
mkdir -p "$TOOLS_DIR"

echo "Building linux-arm64 whr-proxy..."
(cd "$HERE/../../cmd/whr-proxy" && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o "$TOOLS_DIR/whr-proxy" .)

echo "Building linux-arm64 whr-shim..."
(cd "$HERE/../../cmd/whr-shim" && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o "$TOOLS_DIR/whr-shim" .)

AGY_VER="1.2.14"
AGY_BUILD="4571742832820224"
AGY_HASH="200d31754d8a527e6736ff61fc5c5bb0b7282b2c54dfbb65a625e61209b3c687"
TAR_HASH="d96a67d6952a8ec1da16c8b6515f1393ed0260658cf104d9b57b8d13c92c591322ed5e08a62894842be81888116da0f5f80def7971c913d1f462300103426954"

if [ ! -f "$TOOLS_DIR/antigravity" ] || [ "$(shasum -a 256 "$TOOLS_DIR/antigravity" | cut -d' ' -f1)" != "$AGY_HASH" ]; then
  echo "Downloading Antigravity $AGY_VER linux-arm64..."
  TMP_TAR="$(mktemp /tmp/agy-tar.XXXXXX)"
  curl -fsSL "https://storage.googleapis.com/antigravity-public/antigravity-cli/$AGY_VER-$AGY_BUILD/linux-arm/cli_linux_arm64.tar.gz" -o "$TMP_TAR"
  got_tar=$(shasum -a 512 "$TMP_TAR" | cut -d' ' -f1)
  if [ "$got_tar" != "$TAR_HASH" ]; then
    echo "Tar checksum mismatch: expected $TAR_HASH, got $got_tar" >&2
    rm -f "$TMP_TAR"
    exit 1
  fi
  tar -xzf "$TMP_TAR" -C "$TOOLS_DIR"
  rm -f "$TMP_TAR"
  chmod +x "$TOOLS_DIR/antigravity"
  got_bin=$(shasum -a 256 "$TOOLS_DIR/antigravity" | cut -d' ' -f1)
  if [ "$got_bin" != "$AGY_HASH" ]; then
    echo "Binary checksum mismatch: expected $AGY_HASH, got $got_bin" >&2
    exit 1
  fi
fi

echo "Tools built and ready in $TOOLS_DIR:"
ls -lh "$TOOLS_DIR"
