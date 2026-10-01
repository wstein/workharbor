#!/bin/sh
# Spike #69: what can a guest reach on the host (design D29, §7.2)?
#
# Starts host listeners bound to loopback, the LAN address, the container
# network's gateway address and every interface, then curls each of them from
# a guest on the default network (as the egress proxy sidecar is) and from a
# guest on an --internal network. Prints one row per guest, target and port.
#
# Needs: Apple `container` (the system running), python3, curl in the image.
# Changes nothing on the host: it listens on ports 18080-18083 and removes the
# network it creates. Not measured here: a Tailscale address (not installed on
# the test Mac) and any firewall rule (pf needs sudo and a second device).
set -eu

IMAGE=${IMAGE:-fedora}
LAN=${LAN:-$(ipconfig getifaddr en0)}
ROUTER=${ROUTER:-$(route -n get default | awk '/gateway/ {print $2}')}
PIDS=""
cleanup() {
  for p in $PIDS; do kill "$p" 2>/dev/null || true; done
  container stop whr-spike-anchor >/dev/null 2>&1 || true
  container delete whr-spike-anchor >/dev/null 2>&1 || true
  container network delete whr-spike-int >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

# The host has an address on the container network only while a container
# runs, so an anchor container keeps it up.
container run -d --name whr-spike-anchor "$IMAGE" sleep 600 >/dev/null
sleep 3
# The host's address on the container network (default 192.168.64.0/24).
GW=${GW:-$(ifconfig | awk '/inet 192\.168\.64\./ {print $2; exit}')}
[ -n "$GW" ] || { echo "no host address on the container network" >&2; exit 1; }

listen() { # port bind-address
  python3 -m http.server "$1" --bind "$2" >/dev/null 2>&1 &
  PIDS="$PIDS $!"
}
listen 18080 127.0.0.1
listen 18081 "$LAN"
listen 18082 0.0.0.0
listen 18083 "$GW"
sleep 1

echo "host: lan=$LAN gateway=$GW router=$ROUTER macos=$(sw_vers -productVersion) firewall=$(/usr/libexec/ApplicationFirewall/socketfilterfw --getglobalstate 2>&1 | head -1)"
echo "listeners: 18080=127.0.0.1 18081=$LAN 18082=0.0.0.0 18083=$GW"
# Does the host see its own listeners? (A control: a failure here is the test, not the guest.)
for spec in "127.0.0.1:18080" "$LAN:18081" "127.0.0.1:18082" "$GW:18083"; do
  code=$(curl -s -m 3 -o /dev/null -w '%{http_code}' "http://$spec/" || true)
  echo "control host->$spec $code"
done

probe='echo "  guest address: $(grep -A1 "|--" /proc/net/fib_trie | grep -B1 "/32 host LOCAL" | grep -o "[0-9]*\\.[0-9]*\\.[0-9]*\\.[0-9]*" | grep -v "^127" | sort -u | tr "\\n" " " | sed "s/ *$//")"; c=$(curl -s -m 4 -o /dev/null -w "%{http_code}" https://1.1.1.1/ || true); echo "  internet (https://1.1.1.1) -> ${c:-000}"; for t in "$LAN" "$GW"; do for p in 18080 18081 18082 18083; do c=$(curl -s -m 4 -o /dev/null -w "%{http_code}" "http://$t:$p/" || true); echo "  $t:$p -> ${c:-000}"; done; done; c=$(curl -s -m 4 -o /dev/null -w "%{http_code}" "http://$ROUTER/" || true); echo "  router $ROUTER:80 (another LAN device) -> ${c:-000}"'

container network create --internal whr-spike-int >/dev/null
for net in default whr-spike-int; do
  echo "guest on network $net:"
  container run --rm --network "$net" -e LAN="$LAN" -e GW="$GW" -e ROUTER="$ROUTER" "$IMAGE" sh -c "$probe" 2>&1 | grep -E '^  ' || echo "  (no output)"
done
