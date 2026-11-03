#!/bin/bash
T=$CLAUDE_JOB_DIR/tmp; . $T/lin/img.env; C=$T/lin/ct; S=$T/lin/hashstate; rm -rf $S $C/canary; mkdir -p $S $C/canary
cat > $C/ws/repos/own/mise.toml <<X
[env]
FROM_TEMPLATE = "{{ exec(command='touch /canary/env-template-own') }}x"
[tools]
jq = "1.8.2"
X
run() { # run <state-mode> <ws-mode> <cmd>
  docker run --rm -v $T/lin/bin/mise:/usr/local/bin/mise:ro -v $T/lin/data:/mise/data:ro -v $C/ws:/ws:$2 -v $C/canary:/canary -v $S:/mise/state:$1 --tmpfs /mise/cache -w /ws/repos/own -e MISE_DATA_DIR=/mise/data -e MISE_CACHE_DIR=/mise/cache -e MISE_STATE_DIR=/mise/state -e HOME=/tmp/h -e MISE_PARANOID=1 -e MISE_TRUSTED_CONFIG_PATHS=/ws/mise.toml $IMG bash -c "$3" 2>&1 | head -${N:-3}
}
echo "[1] outside sandbox (state rw): mise trust own/mise.toml"; run rw rw "mise trust /ws/repos/own/mise.toml"
echo "[2] sandbox (state ro, ws rw): unmodified"; run ro rw "mise env | head -1; ls /canary"
rm -rf $C/canary/*
echo "[3] sandbox edits mise.toml (adds a task), then mise env"; run ro rw "echo '[tasks.x]
run=\"true\"' >> mise.toml; mise env | head -1; echo canary=\$(ls /canary)"
echo "[4] sandbox reverts to the reviewed bytes -> trusted again"; run ro rw "sed -i '/tasks.x/,\$d' mise.toml; mise env | head -1"
echo "[5] sandbox adds mise.local.toml with template"; run ro rw "printf '[env]\nL=\"{{exec(command=\"touch /canary/local\")}}\"\n' > mise.local.toml; mise env | head -2; echo canary=\$(ls /canary); rm mise.local.toml"
echo "[6] sandbox forges a trust record (state ro)"; run ro rw "echo x > /mise/state/trusted-configs/forged 2>&1"
echo "[7] hash record content"; ls $S/trusted-configs; cat $S/trusted-configs/*.hash; echo; shasum -a 256 $C/ws/repos/own/mise.toml
