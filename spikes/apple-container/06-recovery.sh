#!/usr/bin/env bash
# Item 6: recovery. Only my own whspike-* container is crashed. `container system
# stop/start` is NOT run here: it needs the owner's approval (see the issue).
set -uo pipefail
. "$(dirname "$0")/lib.sh"
SCRATCH=${SCRATCH:?set SCRATCH}
NET=${PREFIX}-net; VOL=${PREFIX}-home; A=${PREFIX}-ag1
PIP=$(cat "$SCRATCH/proxy.ip")

hdr "how the services are launched"
launchctl list 2>/dev/null | grep -iE 'container|apple.container' | head -10
ls ~/Library/LaunchAgents 2>/dev/null | grep -iE 'container|socktainer' || echo "no container LaunchAgent plist in ~/Library/LaunchAgents"
ls /Library/LaunchDaemons 2>/dev/null | grep -iE 'container|socktainer' || echo "no container LaunchDaemon plist in /Library/LaunchDaemons"
ps -axo pid,ppid,etime,command | grep -E '[c]ontainer-(apiserver|runtime|network|core)' | cut -c1-150 | head -12

hdr "restart policy flags"
container run --help 2>&1 | grep -iE 'restart|--init|always|on-failure' | head
say "(only --init matches: there is no restart policy)"

hdr "crash test: kill the host-side runtime process of $A only"
container start $A >/dev/null 2>&1
cexec $A 'echo "agent-state-before-crash" > /root/.claude/crash-marker; sync'
RPID=$(ps -axo pid,command | grep '[c]ontainer-runtime-linux' | grep -- "--uuid $A\|$A" | awk '{print $1}' | head -1)
say "runtime pid for $A: ${RPID:-not found}"
ps -axo pid,command | grep '[c]ontainer-runtime-linux' | cut -c1-200 | head -4
if [ -n "${RPID:-}" ]; then
  kill -9 "$RPID"; sleep 2
  container ls -a | grep -E "ID|$A"
  say "exec after crash: $(cexec $A 'echo still-alive' 2>&1 | head -1)"
  container stop $A >/dev/null 2>&1; sleep 1
  container start $A >/dev/null 2>&1; sleep 1
  container ls -a | grep -E "$A"
  say "state after restart: $(cexec $A 'cat /root/.claude/crash-marker /root/.claude/whspike-marker | tr "\n" " "' 2>&1)"
fi

hdr "kill the container's init process from inside (simulated agent/OS crash)"
container ls -a | grep "$A" | awk '{print "state before:", $5}'
cexec $A 'kill -9 1' 2>&1 | head -1; sleep 2
container ls -a | grep "$A" | awk '{print "state after in-guest PID 1 kill:", $5}'
container start $A >/dev/null 2>&1; sleep 1
say "restart works: $(cexec $A 'cat /root/.claude/crash-marker' 2>&1 | head -1)"
