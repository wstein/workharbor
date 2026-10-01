#!/usr/bin/env bash
# Item 6 (continued): can a named volume be attached to more than one container at a time?
set -uo pipefail
. "$(dirname "$0")/lib.sh"
trap cleanup EXIT
V=${PREFIX}-sv; A=${PREFIX}-s1; B=${PREFIX}-s2; C=${PREFIX}-s3; D=${PREFIX}-s4; E=${PREFIX}-s5
state() { container ls -a | awk -v c=$1 '$1==c{print $5}'; }
short() { local t; t=$(cat); echo "$t" | grep -oE 'The storage device attachment is invalid' | head -1 || true; [ -z "$(echo "$t" | grep -oE 'The storage device attachment is invalid')" ] && echo "$t" | grep -oE 'Error:[^(]{0,60}' | head -1; }
container volume create $V >/dev/null 2>&1

hdr "A holds the volume read-write"
container run -d --name $A --init -v $V:/data $IMG sleep 600 >/dev/null 2>&1
say "A: $(state $A)"
hdr "second container, same volume read-write"
say "B start: $(container run -d --name $B --init -v $V:/data $IMG sleep 600 2>&1 | short) | state: '$(state $B)' (empty means it was not created)"
hdr "second container, same volume read-only, while A has it read-write"
say "C start: $(container run -d --name $C --init -v $V:/data:ro $IMG sleep 600 2>&1 | short) | state: '$(state $C)'"
hdr "A stops; the volume is free again"
container stop $A >/dev/null 2>&1
container run -d --name $B --init -v $V:/data $IMG sleep 600 >/dev/null 2>&1
say "B after A stopped: $(state $B)"
container stop $B >/dev/null 2>&1

hdr "read-only attach by two running containers (as in the tool store)"
container run -d --name $D --init -v $V:/data:ro $IMG sleep 600 >/dev/null 2>&1
container run -d --name $E --init -v $V:/data:ro $IMG sleep 600 >/dev/null 2>&1
say "D: $(state $D)  E: $(state $E)"
hdr "a read-write attach while those two hold it read-only"
container run -d --name ${PREFIX}-s6 --init -v $V:/data $IMG sleep 600 2>&1 | short | sed 's/^/rw attach: /'
