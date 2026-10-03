#!/usr/bin/env bash
# Builds whr-shim and downloads Claude Code linux-arm64 into bin/ (checksum from the signed manifest).
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TOOLS_DIR="${TOOLS_DIR:-$HERE/bin}"
mkdir -p "$TOOLS_DIR"
(cd "$HERE/../../cmd/whr-shim" && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o "$TOOLS_DIR/whr-shim" .)
CLAUDE_VER="${CLAUDE_VER:-2.1.288}"
want=$(curl -fsSL "https://downloads.claude.ai/claude-code-releases/$CLAUDE_VER/manifest.json" |
  python3 -c 'import json,sys; print(json.load(sys.stdin)["platforms"]["linux-arm64"]["checksum"])')
if [ ! -f "$TOOLS_DIR/claude" ] || [ "$(shasum -a 256 "$TOOLS_DIR/claude" | cut -d' ' -f1)" != "$want" ]; then
  curl -fsSL "https://downloads.claude.ai/claude-code-releases/$CLAUDE_VER/linux-arm64/claude" -o "$TOOLS_DIR/claude"
  chmod +x "$TOOLS_DIR/claude"
fi
got=$(shasum -a 256 "$TOOLS_DIR/claude" | cut -d' ' -f1)
[ "$got" = "$want" ] || { echo "checksum mismatch: want $want got $got" >&2; exit 1; }
echo "claude $CLAUDE_VER sha256 $got"
(cd "$HERE/probe" && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o "$TOOLS_DIR/probe" .)
