#!/usr/bin/env bash
# Builds linux-arm64 whr-proxy, whr-shim, and fetches claude 2.1.285 & codex 0.159.2.
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TOOLS_DIR="${TOOLS_DIR:-$HERE/bin}"
mkdir -p "$TOOLS_DIR"

echo "=== 1. Building linux-arm64 whr-proxy ==="
(cd "$HERE/../../cmd/whr-proxy" && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o "$TOOLS_DIR/whr-proxy" .)

echo "=== 2. Building linux-arm64 whr-shim ==="
(cd "$HERE/../../cmd/whr-shim" && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o "$TOOLS_DIR/whr-shim" .)

echo "=== 3. Ensuring Claude Code 2.1.285 linux-arm64 ==="
CLAUDE_VER="2.1.285"
CLAUDE_SHA256="24fac77749bed3d91365d6b6915aa4b824e14318ecb6bc17adbc192f01c9173d"
SCRATCH_CLAUDE="/Users/werner/.gemini/antigravity-ide/brain/d685c7b5-6838-4ba3-bbae-99df113b1223/scratch/tools/claude"

if [ -f "$SCRATCH_CLAUDE" ] && [ "$(shasum -a 256 "$SCRATCH_CLAUDE" | cut -d' ' -f1)" = "$CLAUDE_SHA256" ]; then
  cp "$SCRATCH_CLAUDE" "$TOOLS_DIR/claude"
  chmod +x "$TOOLS_DIR/claude"
else
  curl -fsSL "https://downloads.claude.ai/claude-code-releases/$CLAUDE_VER/linux-arm64/claude" -o "$TOOLS_DIR/claude"
  got=$(shasum -a 256 "$TOOLS_DIR/claude" | cut -d' ' -f1)
  if [ "$got" != "$CLAUDE_SHA256" ]; then
    echo "Checksum mismatch for claude: expected $CLAUDE_SHA256, got $got" >&2
    exit 1
  fi
  chmod +x "$TOOLS_DIR/claude"
fi

echo "=== 4. Ensuring Codex CLI 0.159.2 linux-arm64 musl ==="
TMP_CODEX="/tmp/codex-bin/codex"
if [ -f "$TMP_CODEX" ] && file "$TMP_CODEX" | grep -q "ARM aarch64"; then
  cp "$TMP_CODEX" "$TOOLS_DIR/codex"
  chmod +x "$TOOLS_DIR/codex"
else
  TMP_DIR="$(mktemp -d /tmp/codex-fetch.XXXXXX)"
  gh release download rust-v0.159.2 -R openai/codex -p 'codex-aarch64-unknown-linux-musl.tar.gz' -D "$TMP_DIR"
  tar -xzf "$TMP_DIR/codex-aarch64-unknown-linux-musl.tar.gz" -C "$TMP_DIR"
  mv "$TMP_DIR/codex-aarch64-unknown-linux-musl" "$TOOLS_DIR/codex"
  chmod +x "$TOOLS_DIR/codex"
  rm -rf "$TMP_DIR"
fi

echo "=== Tools ready in $TOOLS_DIR ==="
ls -lh "$TOOLS_DIR"
