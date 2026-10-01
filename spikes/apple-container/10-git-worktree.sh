#!/usr/bin/env bash
# Item 9: repositories live on the host and are mounted into environments.
# Compares an absolute worktree, a relative worktree and a per-task clone that
# borrows the host cache's objects (alternates), for: whether git works in the
# guest, how fast it is on a bind mount, and what a hostile guest can do.
#
# The planted "attacks" only touch marker files under $SCRATCH. Nothing here
# runs outside the scratch directory.
set -uo pipefail
. "$(dirname "$0")/lib.sh"
SCRATCH=${SCRATCH:?set SCRATCH to a scratch directory}
GITIMG=${GITIMG:-docker.io/alpine/git:latest}
trap cleanup EXIT
rm -rf "$SCRATCH/g" && mkdir -p "$SCRATCH/g" && cd "$SCRATCH/g"
G="$SCRATCH/g"
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
gh() { git -c user.name=spike -c user.email=spike@example.invalid "$@"; }

hdr "setup: a synthetic repository with 5000 small files, a bare host cache, three layouts"
mkdir src && cd src && git init -q -b main . && python3 - <<'PY'
import os
for d in range(50):
    os.makedirs(f"d{d}", exist_ok=True)
    for f in range(100):
        open(f"d{d}/f{f}.txt","w").write(f"file {d} {f}\n"*5)
PY
gh add -A && gh commit -q -m init && cd "$G"
git clone -q --bare src cache.git
say "cache objects: $(du -sh cache.git/objects | cut -f1), tracked files: $(git -C src ls-files | wc -l)"
mkdir wt
gh -C cache.git worktree add -q -b agent/t1 "$G/wt/t1" main           # absolute pointer
gh -C cache.git worktree add -q --relative-paths -b agent/t2 "$G/wt/t2" main   # relative pointer
git clone -q --shared cache.git "$G/clone3" && git -C clone3 checkout -q -b agent/t3   # own repo, alternates to the cache
say "t1 .git file : $(cat wt/t1/.git)"
say "t2 .git file : $(cat wt/t2/.git)"
say "t3 alternates: $(cat clone3/.git/objects/info/alternates)"

hdr "guest image with git: $GITIMG"
container image ls | grep -q "alpine/git" && say "present" || { container image pull "$GITIMG" >/dev/null 2>&1 && say "pulled (remove it afterwards)" && echo "$GITIMG" > "$SCRATCH/pulled-git-image.txt"; }
N=${PREFIX}-g1
start() { # start <name> <mount args...>
  local n=$1; shift
  container rm -f $n >/dev/null 2>&1
  container run -d --name $n --init --entrypoint sleep "$@" $GITIMG 3600 >/dev/null 2>&1
}
gx() { container exec $N sh -c "$*" 2>&1; }

hdr "V0: mount only the worktree (absolute pointer) at /work"
start $N --mount type=bind,source="$G/wt/t1",target=/work
say "$(gx 'cd /work && git status 2>&1 | head -2')"

hdr "V0r: relative worktree: mount the worktree and the cache keeping their relative layout"
start $N --mount type=bind,source="$G/wt/t2",target=/m/wt/t2 --mount type=bind,source="$G/cache.git",target=/m/cache.git
say "$(gx 'cd /m/wt/t2 && git status 2>&1 | head -3; git branch --show-current')"

hdr "V1: absolute worktree with the cache mounted at its host path (read-write)"
start $N --mount type=bind,source="$G/wt/t1",target="$G/wt/t1" --mount type=bind,source="$G/cache.git",target="$G/cache.git"
say "git works: $(gx "cd $G/wt/t1 && git status --short | wc -l | sed 's/^/changed files: /'; git branch --show-current")"
say "guest sees ALL branches of the shared repo: $(gx "git -C $G/cache.git branch --list | tr -s ' \n' ' '")"
say "guest sees other tasks' worktree metadata:   $(gx "ls $G/cache.git/worktrees | tr '\n' ' '")"

hdr "S4: from the shared repo a guest can destroy another task's branch"
gx "git -C $G/cache.git branch -D agent/t2" | head -1
say "host: branch agent/t2 still exists? $(git -C cache.git branch --list agent/t2 | wc -l | sed 's/^0$/NO, deleted by the guest/;s/^1$/yes/')"
gh -C cache.git branch -q agent/t2 main 2>/dev/null

hdr "S1: a hostile guest plants a pre-commit hook in the shared repo; the host then runs git"
rm -f "$G/PWNED_hook"
gx "printf '#!/bin/sh\ntouch $G/PWNED_hook\n' > $G/cache.git/hooks/pre-commit; chmod +x $G/cache.git/hooks/pre-commit"
echo "x" >> wt/t1/d0/f0.txt; gh -C wt/t1 add -A
gh -C wt/t1 commit -q -m "host commit" 2>&1 | head -1
say "plain host git ran the guest's hook: $([ -e "$G/PWNED_hook" ] && echo YES, code executed on the host || echo no)"
rm -f "$G/PWNED_hook"; echo "y" >> wt/t1/d0/f1.txt; gh -C wt/t1 add -A
gh -c core.hooksPath=/dev/null -C wt/t1 commit -q -m "host commit, hardened" 2>&1 | head -1
say "host git with -c core.hooksPath=/dev/null:   $([ -e "$G/PWNED_hook" ] && echo YES, still executed || echo no, hook ignored)"
rm -f "$G/cache.git/hooks/pre-commit"

hdr "S2: a hostile guest sets core.fsmonitor in the shared config; the host runs git status"
rm -f "$G/PWNED_fsmon"
gx "git -C $G/cache.git config core.fsmonitor 'touch $G/PWNED_fsmon; echo'"
gh -C wt/t1 status >/dev/null 2>&1
say "plain host git status ran the guest's command: $([ -e "$G/PWNED_fsmon" ] && echo YES, code executed on the host || echo no)"
rm -f "$G/PWNED_fsmon"
gh -c core.fsmonitor=false -C wt/t1 status >/dev/null 2>&1
say "host git with -c core.fsmonitor=false:        $([ -e "$G/PWNED_fsmon" ] && echo YES, still executed || echo no, ignored)"
git -C cache.git config --unset core.fsmonitor

hdr "V3: per-task clone with alternates; the cache objects are mounted read-only at their host path"
start $N --mount type=bind,source="$G/clone3",target=/work --mount type=bind,source="$G/cache.git/objects",target="$G/cache.git/objects",readonly
say "git works: $(gx 'cd /work && git status --short | wc -l | sed "s/^/changed files: /"; git branch --show-current')"
OBJ_BEFORE=$(find cache.git/objects -type f | wc -l)
gx "cd /work && echo new >> d1/f1.txt && git -c user.name=g -c user.email=g@x add -A && git -c user.name=g -c user.email=g@x commit -q -m 'commit in the guest' && git log --oneline | head -2"
say "guest commit stored in the task's own repo; host cache objects before/after: $OBJ_BEFORE / $(find cache.git/objects -type f | wc -l)"
say "guest cannot write the cache objects: $(gx "touch $G/cache.git/objects/x 2>&1 | head -1")"
say "other tasks' worktrees and branches are not visible: $(gx "ls $G/wt 2>&1 | head -1")"
rm -f "$G/PWNED_hook"; gx "printf '#!/bin/sh\ntouch $G/PWNED_hook\n' > /work/.git/hooks/pre-commit; chmod +x /work/.git/hooks/pre-commit"
echo z >> clone3/d2/f2.txt; gh -C clone3 add -A
say "host commit, plain git, in the task clone with a planted hook:    $(gh -C clone3 commit -q -m h 2>&1 | head -1; [ -e "$G/PWNED_hook" ] && echo YES, code executed on the host || echo no)"
rm -f "$G/PWNED_hook"; echo zz >> clone3/d2/f3.txt; gh -C clone3 add -A
say "host commit, with -c core.hooksPath=/dev/null:                    $(gh -c core.hooksPath=/dev/null -C clone3 commit -q -m h2 2>&1 | head -1; [ -e "$G/PWNED_hook" ] && echo YES, executed || echo no, ignored)"

hdr "performance: 5000 files. Bind-mounted checkout versus a clone inside a volume"
VOL=${PREFIX}-gv; container volume create $VOL >/dev/null 2>&1
perf() { # perf <label> <container> <dir>
  local label=$1 n=$2 d=$3 t0 t1 t2 t3 t4
  container exec $n sh -c "cd $d && for i in \$(seq 1 300); do echo more >> d\$((i % 50))/f\$((i % 100)).txt; done" >/dev/null 2>&1
  t0=$(now_ms); container exec $n sh -c "cd $d && git status --short | wc -l" >/dev/null 2>&1
  t1=$(now_ms); container exec $n sh -c "cd $d && git -c user.name=g -c user.email=g@x add -A" >/dev/null 2>&1
  t2=$(now_ms); container exec $n sh -c "cd $d && git -c user.name=g -c user.email=g@x commit -q -m perf" >/dev/null 2>&1
  t3=$(now_ms); container exec $n sh -c "cd $d && git status --short | wc -l" >/dev/null 2>&1
  t4=$(now_ms)
  say "$label: status after 300 edits $((t1-t0)) ms | add $((t2-t1)) ms | commit $((t3-t2)) ms | clean status $((t4-t3)) ms"
}
PN=${PREFIX}-g2
container rm -f $N $PN >/dev/null 2>&1
container run -d --name $PN --init --entrypoint sleep \
  --mount type=bind,source="$G/clone3",target=/bindclone \
  --mount type=bind,source="$G/cache.git/objects",target="$G/cache.git/objects",readonly \
  --mount type=bind,source="$G/cache.git",target=/cache,readonly \
  -v $VOL:/vol $GITIMG 3600 >/dev/null 2>&1
t0=$(now_ms); container exec $PN sh -c "git clone -q --shared /cache /vol/clone 2>&1 | tail -1; git -C /vol/clone checkout -q -b perf" >/dev/null 2>&1; t1=$(now_ms)
say "clone into the volume (alternates to the read-only cache): $((t1-t0)) ms"
perf "bind-mounted clone (host dir)  " $PN /bindclone
perf "clone inside a volume          " $PN /vol/clone

hdr "cleanup note"
say "host-side hardening used above: git -c core.hooksPath=/dev/null -c core.fsmonitor=false"
