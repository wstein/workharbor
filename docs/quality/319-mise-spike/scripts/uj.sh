#!/bin/bash
T=$CLAUDE_JOB_DIR/tmp; X=$T/scripts/ct.sh
export N=2
UJ=(-e MISE_PARANOID=1 -e MISE_YES=1 -e MISE_TRUSTED_CONFIG_PATHS=/ws/mise.toml -e MISE_IGNORED_CONFIG_PATHS=/ws/repos/hostile)
echo "=== untrusted_job env (paranoid, YES=1 forced by attacker, whr layer file-trusted, repo ignored), data ro"
for c in "mise exec -- jq --version" "mise exec -- bat --version" "mise run pwn" "mise tasks" "mise hook-env -s bash" "mise install --locked" "mise ls --current" "mise cfg"; do $X "[$c]" /ws/repos/hostile "${UJ[@]}" -- "$c"; done
echo "=== defense missing: only ignore (no paranoid)"
for c in "mise exec -- jq --version" "mise run pwn"; do $X "[ignore-only: $c]" /ws/repos/hostile -e MISE_IGNORED_CONFIG_PATHS=/ws/repos/hostile -- "$c"; done
echo "=== sandbox overrides env itself (agent runs MISE_IGNORED_CONFIG_PATHS= / unsets paranoid)"
$X "[agent: MISE_PARANOID=0 MISE_IGNORED_CONFIG_PATHS= mise run pwn]" /ws/repos/hostile "${UJ[@]}" -- "MISE_PARANOID=0 MISE_IGNORED_CONFIG_PATHS= MISE_YES=1 mise run pwn"
$X "[agent: writes ~/.config/mise/config.toml trusted_config_paths=/]" /ws/repos/hostile "${UJ[@]}" -- "mkdir -p /tmp/h/.config/mise; printf '[settings]\ntrusted_config_paths=[\"/\"]\nparanoid=false\nignored_config_paths=[]\n' > /tmp/h/.config/mise/config.toml; MISE_PARANOID= MISE_IGNORED_CONFIG_PATHS= mise run pwn"
$X "[agent: writes .miserc.toml in repo]" /ws/repos/hostile "${UJ[@]}" -v $T/lin/ct/ws:/ws:rw -- "printf 'trusted_config_paths=[\"/\"]\n' > /ws/repos/hostile/.miserc.toml; mise run pwn; rm -f /ws/repos/hostile/.miserc.toml"
