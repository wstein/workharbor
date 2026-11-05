#!/bin/bash
# Builds the lab used by the other scripts. Run once: T=<empty scratch dir> bash lab-setup.sh
# Needs: gh (logged in), docker (OrbStack), curl-free download via gh, python3.
# The other scripts use $CLAUDE_JOB_DIR/tmp as $T; set CLAUDE_JOB_DIR so that $CLAUDE_JOB_DIR/tmp == $T.
set -eu
: "${T:?set T to an empty scratch directory}"
V=v2026.10.25
mkdir -p "$T"/{mise-dl,bin,home,scripts,lab/canary,lin/{bin,data,proj}}
cd "$T/mise-dl"
gh release download $V --repo jdx/mise -p "mise-$V-macos-arm64" -p "mise-$V-linux-arm64" -p SHASUMS256.txt
grep -E "(macos|linux)-arm64$" SHASUMS256.txt | shasum -a 256 -c -
cp "mise-$V-macos-arm64" "$T/bin/mise"; cp "mise-$V-linux-arm64" "$T/lin/bin/mise"; chmod +x "$T/bin/mise" "$T/lin/bin/mise"
cat > "$T/env.sh" <<X
export PATH=$T/bin:/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin:/usr/local/bin
export HOME=$T/home XDG_CONFIG_HOME=$T/home/.config XDG_DATA_HOME=$T/home/.local/share XDG_CACHE_HOME=$T/home/.cache XDG_STATE_HOME=$T/home/.local/state
export MISE_DATA_DIR=$T/mise-data MISE_CACHE_DIR=$T/mise-cache MISE_STATE_DIR=$T/mise-state MISE_CONFIG_DIR=$T/home/.config/mise
X
# Container image: any Debian 13 arm64 image with bash and ca-certificates (the spike used the Anchorage devcontainer image).
echo "IMG=${IMG:-debian:trixie}" > "$T/lin/img.env"
L=$T/lab
# hostile and own repositories (see ../fixtures/hostile-mise.toml; paths differ per case, here the host variant)
mkdir -p "$L/ws/repos/hostile" "$L/ws/repos/own"
for r in hostile own; do
  sed "s#\$T#$T#g; s#env-template-hostile#env-template-$r#; s#hook-enter-hostile#hook-enter-$r#; s#hook-postinstall-hostile#hook-postinstall-$r#; s#task-hostile#task-$r#" \
    "$(dirname "$0")/../fixtures/hostile-mise.toml" > "$L/ws/repos/$r/mise.toml"
  git -C "$L/ws/repos/$r" init -q
done
# five-layer tree
W=$L/layers/whr/ws; mkdir -p "$W/repos/proj" "$T/home/.config/mise"
printf '[tools]\njq = "1.6"\nfd = "10.0.0"\n[env]\nLAYER_ORIGIN = "user-global"\nUSER_ONLY = "u"\n' > "$T/home/.config/mise/config.toml"
printf '[tools]\njq = "1.7.1"\nripgrep = "14.0.0"\n[env]\nLAYER_ORIGIN = "whr-global"\nWHR_ONLY = "w"\n' > "$L/layers/whr/mise.toml"
printf '[tools]\njq = "1.8.0"\nbat = "0.25.0"\n[env]\nLAYER_ORIGIN = "workspace-root"\nWS_ONLY = "r"\n' > "$W/mise.toml"
printf '[tools]\njq = "1.8.1"\nfzf = "0.60.0"\n[env]\nLAYER_ORIGIN = "project"\nPROJ_ONLY = "p"\n' > "$W/repos/proj/mise.toml"
printf '[tools]\njq = "1.8.2"\nzoxide = "0.9.8"\n[env]\nLAYER_ORIGIN = "local"\nLOCAL_ONLY = "l"\n' > "$W/repos/proj/mise.local.toml"
git -C "$W/repos/proj" init -q
# lock project
mkdir -p "$L/lock"; printf '[tools]\njq = "1.8.2"\nripgrep = "15.1.0"\n' > "$L/lock/mise.toml"; git -C "$L/lock" init -q
echo "lab ready in $T (the user-global layer file is removed again by layers.sh after use)"
