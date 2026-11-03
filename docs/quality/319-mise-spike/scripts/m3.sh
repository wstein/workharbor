#!/bin/bash
. "$CLAUDE_JOB_DIR/tmp/env.sh"
T=$CLAUDE_JOB_DIR/tmp; S=$T/scripts/trustcase.sh; L=$T/lab; O=$L/ws/repos/own
export LINES_SHOWN=2
echo "=== A: own repo trusted by PATH (trusted_config_paths); sandbox later writes mise.local.toml"
cat > $O/mise.local.toml <<X
[env]
LOCAL_INJECT = "{{ exec(command='touch $L/canary/local-injected') }}"
X
$S "path-trust, no paranoid, mise env" $O MISE_TRUSTED_CONFIG_PATHS=$O -- env
$S "path-trust + paranoid, mise env" $O MISE_PARANOID=1 MISE_TRUSTED_CONFIG_PATHS=$O -- env
rm $O/mise.local.toml
echo "=== B: hash trust in paranoid mode"
export MISE_STATE_DIR=$T/state-hash; rm -rf $MISE_STATE_DIR; mkdir -p $MISE_STATE_DIR; cd $O
export MISE_PARANOID=1
mise trust $O/mise.toml 2>&1 | head -2; echo "after trust:"; mise trust --show 2>&1
rm -rf $L/canary; mkdir $L/canary; mise env 2>&1 </dev/null | head -1; echo "canary: $(ls $L/canary)"
echo "# edit" >> $O/mise.toml
echo "after edit (appended comment):"; mise trust --show 2>&1; mise env 2>&1 </dev/null| head -2
sed -i '' '$d' $O/mise.toml
echo "after revert:"; mise trust --show 2>&1
echo "stateDir content:"; find $MISE_STATE_DIR -type f | head; ls -la $MISE_STATE_DIR/trusted-configs 2>/dev/null | head
unset MISE_PARANOID
echo "=== C: repo [settings] trusted_config_paths=['/'] in trusted own repo does not trust hostile"
cp $O/mise.toml $T/own.bak; printf '[settings]\ntrusted_config_paths=["/"]\n' >> $O/mise.toml
$S "settings in own, run in HOSTILE" $L/ws/repos/hostile MISE_TRUSTED_CONFIG_PATHS=$O -- env
cp $T/own.bak $O/mise.toml
