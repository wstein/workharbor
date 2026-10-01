#!/usr/bin/env bash
# Orchestrates all measurements for spike #82: Agent Subscription Sign-in.
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "=== Building tools and ensuring binaries ==="
"$HERE/build-tools.sh"

echo "=== Step 1: Testing Container Setup and Allowlist Proxy ==="
"$HERE/test-setup.sh" | tee "$HERE/results/01-setup.txt"

echo "=== Step 2: Testing Claude Code 2.1.285 Sign-in Flows & Storage ==="
"$HERE/test-claude-signin.sh" | tee "$HERE/results/02-claude-signin.txt"

echo "=== Step 3: Testing Codex CLI 0.159.2 Sign-in Flows & Storage ==="
"$HERE/test-codex-signin.sh" | tee "$HERE/results/03-codex-signin.txt"

echo "=== Step 4: Testing Concurrency and Multi-Environment Isolation ==="
"$HERE/test-concurrency.sh" | tee "$HERE/results/04-concurrency.txt"

echo "=== All spike measurements completed successfully ==="
