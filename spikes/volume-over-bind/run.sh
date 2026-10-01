#!/bin/sh
# Spike #80: does a named volume mounted at a subdirectory of a bind mount work on Apple Container?
# Checks: (1) the guest sees the volume, (2) the host sees only an empty mount-point directory,
# (3) contents survive stop+start, (4) contents survive delete and a rebuild with the same volume,
# (5) the volume is writable while the bind mount stays writable, (6) a tracked file at the mount point.
set -u
W=$(mktemp -d)
N=whr-spike80
V=$N-vol
cleanup() { container delete --force $N >/dev/null 2>&1; container volume delete $V >/dev/null 2>&1; rm -rf "$W"; }
trap cleanup EXIT
cleanup >/dev/null 2>&1; W=$(mktemp -d)
say() { printf '%s\n' "$*"; }
mkdir -p "$W/work" && echo tracked > "$W/work/README"
container volume create -s 1G $V >/dev/null
mk() { container run -d --name $N -v "$W/work:/work" -v "$V:/work/node_modules" fedora sleep 3600 >/dev/null; }
mk
say "1 guest mounts:"; container exec $N sh -c 'grep " /work" /proc/mounts'
container exec $N sh -c 'echo hello > /work/node_modules/dep.txt; echo guest-edit >> /work/README; touch /work/new-file'
say "2 guest sees volume file: $(container exec $N cat /work/node_modules/dep.txt)"
say "2 host sees mount point: $(ls -ld "$W/work/node_modules" 2>&1 | cut -c1-10) entries=$(ls -A "$W/work/node_modules" 2>/dev/null | wc -l | tr -d ' ')"
say "2 host sees bind writes: README=$(tr '\n' ' ' < "$W/work/README") new-file=$([ -e "$W/work/new-file" ] && echo yes || echo no)"
container stop $N >/dev/null; container start $N >/dev/null
say "3 after stop+start: $(container exec $N cat /work/node_modules/dep.txt 2>&1)"
container stop $N >/dev/null; container delete $N >/dev/null
rm -rf "$W/work/node_modules"   # the host removes the mount point (the supervisor's checkout was re-created)
mk
say "4 after delete+rebuild, mount point recreated by runtime: $(ls -ld "$W/work/node_modules" 2>&1 | cut -c1-10)"
say "4 after delete+rebuild: $(container exec $N cat /work/node_modules/dep.txt 2>&1)"
container stop $N >/dev/null; container delete $N >/dev/null
mkdir -p "$W/work/node_modules" && echo tracked-file > "$W/work/node_modules/tracked.txt"
mk
say "6 host has a file at the mount point; guest sees: $(container exec $N ls /work/node_modules 2>&1 | tr '\n' ' ')"
say "6 host file still there: $(ls "$W/work/node_modules")"
container exec $N sh -c 'cd /work && git --version >/dev/null 2>&1; echo -n "7 unmounting not needed; rename of the mount point: "; mv /work/node_modules /work/nm2 2>&1 | head -1; echo'
container exec $N sh -c 'rm -rf /work/node_modules; echo "8 rm -rf of the mount point: exit $?"; ls -A /work/node_modules | tr "\n" " "; echo; mkdir -p /work/node_modules/a/b && echo "8 recreate inside: ok"; rmdir /work/node_modules 2>&1 | sed "s/^/8 rmdir: /"'
