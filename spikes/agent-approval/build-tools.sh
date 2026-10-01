#!/usr/bin/env bash
# Builds probe, whr-shim, and downloads Claude Code linux-arm64 into $TOOLS_DIR.
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TOOLS_DIR="${TOOLS_DIR:-$HERE/bin}"
mkdir -p "$TOOLS_DIR"

echo "Building linux-arm64 probe..."
(cd "$HERE/probe" && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o "$TOOLS_DIR/probe" .)

echo "Building linux-arm64 whr-shim..."
(cd "$HERE/../../cmd/whr-shim" && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o "$TOOLS_DIR/whr-shim" .)

CLAUDE_VER="2.1.285"
CLAUDE_HASH="24fac77749bed3d91365d6b6915aa4b824e14318ecb6bc17adbc192f01c9173d"
if [ ! -f "$TOOLS_DIR/claude" ] || [ "$(shasum -a 256 "$TOOLS_DIR/claude" | cut -d' ' -f1)" != "$CLAUDE_HASH" ]; then
  echo "Downloading Claude Code $CLAUDE_VER linux-arm64..."
  curl -fsSL "https://downloads.claude.ai/claude-code-releases/$CLAUDE_VER/linux-arm64/claude" -o "$TOOLS_DIR/claude"
  chmod +x "$TOOLS_DIR/claude"
  got=$(shasum -a 256 "$TOOLS_DIR/claude" | cut -d' ' -f1)
  if [ "$got" != "$CLAUDE_HASH" ]; then
    echo "Checksum mismatch: expected $CLAUDE_HASH, got $got" >&2
    exit 1
  fi
fi

echo "Tools built and ready in $TOOLS_DIR:"
ls -lh "$TOOLS_DIR"
