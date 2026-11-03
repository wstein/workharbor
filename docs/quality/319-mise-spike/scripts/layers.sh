#!/bin/bash
# Section 1 of the ADR: five layers with origins. Needs lab-setup.sh first.
. "$CLAUDE_JOB_DIR/tmp/env.sh"; T=$CLAUDE_JOB_DIR/tmp
cd "$T/lab/layers/whr/ws/repos/proj"; export MISE_YES=1
echo '$ mise cfg'; mise cfg 2>&1 | sed "s#$T/#\$T/#"
echo '$ mise ls --current --json (tool, requested, source)'
mise ls --current --json | python3 -I -c "
import json,sys
for k,v in sorted(json.load(sys.stdin).items()):
  for e in v: print(k, e['requested_version'], e['source']['path'])" | sed "s#$T/#\$T/#"
echo '$ mise set'; mise set | sed "s#$T/#\$T/#"
