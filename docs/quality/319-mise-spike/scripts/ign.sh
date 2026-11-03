#!/bin/bash
T=$CLAUDE_JOB_DIR/tmp; X=$T/scripts/ct.sh
for ign in /ws/repos/hostile /ws/repos/hostile/ /ws/repos/hostile/mise.toml /ws/repos /ws/repos/hostile:/nonexistent; do
 for mode in "none" "yes" "paranoid" "paranoid+yes+trust(/ws/mise.toml)" "trust(/ws/mise.toml)"; do
  a=(); case $mode in none) ;; yes) a=(-e MISE_YES=1);; paranoid) a=(-e MISE_PARANOID=1);; "paranoid+yes+trust(/ws/mise.toml)") a=(-e MISE_PARANOID=1 -e MISE_YES=1 -e MISE_TRUSTED_CONFIG_PATHS=/ws/mise.toml);; "trust(/ws/mise.toml)") a=(-e MISE_TRUSTED_CONFIG_PATHS=/ws/mise.toml);; esac
  out=$(N=20 $X "x" /ws/repos/hostile -e MISE_IGNORED_CONFIG_PATHS=$ign "${a[@]}" -- "mise cfg" 2>&1)
  r="hostile-IGNORED"; echo "$out" | grep -q "error parsing\|repos/hostile/mise.toml  bat" && r="HOSTILE-SEEN"; echo "$out" | grep -q "canary=\[env" && r="$r+EXECUTED"
  printf '%-34s %-36s %s\n' "$ign" "$mode" "$r"
 done
done
