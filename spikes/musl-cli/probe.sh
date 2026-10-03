#!/bin/sh
# Spike musl-cli (#152): do the vendors publish linux-arm64 musl builds? Host-free, no credentials.
set -eu
CL=https://downloads.claude.ai/claude-code-releases
V=$(curl -fsS $CL/stable)
echo "claude stable=$V"
curl -fsS $CL/$V/manifest.json | python3 -c "import json,sys;p=json.load(sys.stdin)['platforms'];print('claude platforms:',sorted(p));print(p['linux-arm64-musl'])"
curl -fsS -r 0-4095 -o /tmp/claude.head $CL/$V/linux-arm64-musl/claude
file /tmp/claude.head
AB=https://antigravity-cli-auto-updater-974169037036.us-central1.run.app/manifests
for p in linux_arm64 linux_arm64_musl linux_amd64_musl; do echo "agy $p http=$(curl -s -o /dev/null -w '%{http_code}' $AB/$p.json)"; done
curl -s $AB/linux_arm64_musl.json
CX=$(curl -sI https://github.com/openai/codex/releases/latest | tr -d '\r' | awk -F/ 'tolower($1) ~ /^location/ {print $NF}')
echo "codex latest=$CX"
curl -fsSL -o /tmp/codex.tgz https://github.com/openai/codex/releases/download/$CX/codex-aarch64-unknown-linux-musl.tar.gz
tar -C /tmp -xzf /tmp/codex.tgz; file /tmp/codex-aarch64-unknown-linux-musl
container system status 2>&1 | head -2 || true
