#!/bin/sh
# Issue #80: how npm copes with a volume mounted at node_modules over a bind-mounted checkout.
# Cases: npm install, npm ci over an existing tree, rm -rf node_modules && npm install, npm ci with the dir empty.
set -u
N=whr-spike80n; V=$N-vol; W=
cleanup() { container delete --force $N >/dev/null 2>&1; container volume delete $V >/dev/null 2>&1; [ -n "$W" ] && rm -rf "$W"; }
cleanup
W=$(cd "$(mktemp -d)" && pwd -P)
trap cleanup EXIT
mkdir -p "$W/work"
cat >"$W/work/package.json" <<'JSON'
{"name":"spike","version":"1.0.0","dependencies":{"left-pad":"1.3.0","is-odd":"3.0.1"}}
JSON
container volume create -s 2G $V >/dev/null
container run -d --name $N -v "$W/work:/work" -v "$V:/work/node_modules" -w /work -e HOME=/tmp -e npm_config_cache=/tmp/npm node:22-slim sleep 3600 >/dev/null 2>&1 || { echo "cannot start node:22-slim"; exit 1; }
g() { container exec $N sh -c "$1" 2>&1 | tail -${2:-3}; }
echo "== A. npm install (first, volume empty apart from lost+found)"; g 'npm install --no-audit --no-fund; echo "exit $?"; ls node_modules | tr "\n" " "'
echo "== B. npm ci over an existing tree"; g 'npm ci --no-audit --no-fund; echo "exit $?"; ls node_modules | tr "\n" " "' 6
echo "== C. rm -rf node_modules && npm install"; g 'rm -rf node_modules; echo "rm exit $?"; npm install --no-audit --no-fund; echo "exit $?"; ls node_modules | tr "\n" " "' 6
echo "== D. npm ci with the directory emptied by hand"; g 'find node_modules -mindepth 1 -delete; npm ci --no-audit --no-fund; echo "exit $?"; ls node_modules | tr "\n" " "' 6
echo "== E. host view"; ls -A "$W/work/node_modules" | wc -l | sed 's/^/entries in host node_modules: /'
