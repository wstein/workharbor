#!/bin/sh
# Spike #89: the console mounts the workspaces root read-only and one workspace read-write over it.
set -u
N=whr-spike89m; W=
cleanup() { container delete --force $N >/dev/null 2>&1; [ -n "$W" ] && rm -rf "$W"; }
cleanup; W=$(cd "$(mktemp -d)" && pwd -P); trap cleanup EXIT
mkdir -p "$W/root/a" "$W/root/b"; echo a > "$W/root/a/f"; echo b > "$W/root/b/f"
container run -d --name $N -v "$W/root:/workspaces:ro" -v "$W/root/a:/workspaces/a" whr-console-ubuntu sleep 600 >/dev/null 2>&1
E() { container exec $N sh -c "$1" 2>&1; }
echo "mounts:"; E 'grep " /workspaces" /proc/mounts'
echo "write to b (read-only): $(E 'echo x >> /workspaces/b/f; echo exit $?' | tr '\n' ' ')"
echo "create in root (read-only): $(E 'touch /workspaces/new; echo exit $?' | tr '\n' ' ')"
echo "write to a (read-write): $(E 'echo x >> /workspaces/a/f; echo exit $?' | tr '\n' ' ')"
echo "host a/f: $(tr '\n' ' ' < "$W/root/a/f")  host b/f: $(tr '\n' ' ' < "$W/root/b/f")"
