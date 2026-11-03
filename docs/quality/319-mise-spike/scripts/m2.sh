#!/bin/bash
T=$CLAUDE_JOB_DIR/tmp; S=$T/scripts/trustcase.sh; L=$T/lab; H=$L/ws/repos/hostile; O=$L/ws/repos/own
export LINES_SHOWN=2
for c in "env" "exec -- true" "install" "run pwn" "tasks"; do $S "ignored(hostile)+paranoid: mise $c" $H MISE_PARANOID=1 MISE_IGNORED_CONFIG_PATHS=$H -- $c; done
echo ==== ignored only
for c in "env" "exec -- true" "install" "run pwn" "tasks"; do $S "ignored(hostile): mise $c" $H MISE_IGNORED_CONFIG_PATHS=$H MISE_YES=1 -- $c; done
echo ==== trusted_config_paths=own, paranoid, hostile also presen
for c in "env" "exec -- true" "install" "run pwn"; do $S "paranoid+trusted(own) in OWN: mise $c" $O MISE_PARANOID=1 MISE_TRUSTED_CONFIG_PATHS=$O -- $c; done
for c in "env" "exec -- true" "install"; do $S "paranoid+trusted(own) in HOSTILE: mise $c" $H MISE_PARANOID=1 MISE_TRUSTED_CONFIG_PATHS=$O -- $c; done
echo ==== trusted_config_paths=own, NOT paranoid
for c in "env" "ls" "tasks"; do $S "trusted(own) in OWN: mise $c" $O MISE_TRUSTED_CONFIG_PATHS=$O -- $c; done
for c in "env" "ls"; do $S "trusted(own) in HOSTILE, no paranoid: mise $c" $H MISE_TRUSTED_CONFIG_PATHS=$O -- $c; done
