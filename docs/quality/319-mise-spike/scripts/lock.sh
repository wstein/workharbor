#!/bin/bash
# Section 4 of the ADR: mise.lock with --locked. Needs lab-setup.sh first (clean HOME: no user-global layer file).
. "$CLAUDE_JOB_DIR/tmp/env.sh"; T=$CLAUDE_JOB_DIR/tmp; P=$T/lab/lock; cd "$P"; export MISE_YES=1 MISE_DATA_DIR=$T/mise-data-lock
mv "$T/home/.config/mise/config.toml" "$T/layers-user-global.toml" 2>/dev/null
rm -rf "$MISE_DATA_DIR" mise.lock; git checkout -q mise.toml 2>/dev/null
printf '[tools]\njq = "1.8.2"\nripgrep = "15.1.0"\n' > mise.toml
mise lock >/dev/null 2>&1
echo '1 in sync:'; mise install --locked >/dev/null 2>&1; echo "  exit $?"
echo '2 version drift (ripgrep 15.0.0 not in lock):'; sed -i '' 's/15.1.0/15.0.0/' mise.toml; mise install --locked >/dev/null 2>&1; echo "  exit $?"
echo '  --dry-run-code:'; mise install --locked --dry-run-code >/dev/null 2>&1; echo "  exit $?"; sed -i '' 's/15.0.0/15.1.0/' mise.toml
echo '3 tool added without lock entry (fd):'; echo 'fd = "10.4.0"' >> mise.toml; mise install --locked >/dev/null 2>&1; echo "  exit $?"; sed -i '' '/^fd = /d' mise.toml
echo '4 tampered checksum (macos-arm64 ripgrep):'; cp mise.lock "$T/lock.bak"
sed -i '' 's/378e973289176ca0c6054054ee7f631a065874a352bf43f0fa60ef079b6ba715/0000000000000000000000000000000000000000000000000000000000000000/' mise.lock
rm -rf "$MISE_DATA_DIR/installs/ripgrep"; mise install --locked 2>&1 | grep -E "Actual|Expected|mismatch" | head -2; mise install --locked >/dev/null 2>&1; echo "  exit $?"
cp "$T/lock.bak" mise.lock
