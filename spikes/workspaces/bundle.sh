#!/bin/sh
# Spike #89, criterion 1: export commits as a git bundle streamed out of a running environment,
# verify it and fetch it into a supervisor-owned bare repository, with nothing planted in the
# workspace's .git running on the host. Usage: bundle.sh
set -u
N=whr-spike89b; W=
cleanup() { container delete --force $N >/dev/null 2>&1; [ -n "$W" ] && rm -rf "$W"; }
cleanup
W=$(cd "$(mktemp -d)" && pwd -P)
trap cleanup EXIT
IMG=golang:1.27.1-trixie   # has git; any image with git will do
say() { printf '%s\n' "$*"; }
ms() { python3 -c 'import time;print(int(time.time()*1000))'; }

# The workspace: a folder with the agent's own clone, seeded on the host from a "forge" repository.
git init -q --bare "$W/forge.git"
git clone -q "$W/forge.git" "$W/seed" 2>/dev/null
( cd "$W/seed" && git -c user.name=h -c user.email=h@h checkout -q -b main && echo base > README && git add . && git -c user.name=h -c user.email=h@h commit -q -m base && git push -q origin main )
mkdir -p "$W/ws" "$W/host-markers"
git clone -q "$W/forge.git" "$W/ws/repo"

container run -d --name $N -v "$W/ws:/ws" -w /ws -e HOME=/tmp/h -e GIT_CONFIG_GLOBAL=/tmp/gitconfig "$IMG" sleep 3600 >/dev/null 2>&1
G='git -c user.name=agent -c user.email=agent@whr'
container exec $N sh -c "git config --global user.name agent; git config --global user.email agent@whr; git config --global --add safe.directory '*'; mkdir -p /ws/wt; $G -C /ws/repo worktree add -q -b agent/docs /ws/wt/docs main && $G -C /ws/repo worktree add -q -b agent/runtime /ws/wt/runtime main && git -C /ws/repo worktree list | wc -l"

# What a compromised agent could write into its clone's .git. The markers live in a directory that only
# exists on the host, so a marker proves the command ran on the host (in the guest the path does not exist).
plant() {
  container exec $N sh -c "cd /ws/repo/.git && git config core.fsmonitor 'sh -c \"touch $W/host-markers/FSMONITOR || true\"' && printf '#!/bin/sh\ntouch $W/host-markers/HOOK_POST_CHECKOUT || true\n' > hooks/post-checkout && chmod +x hooks/post-checkout && printf '#!/bin/sh\ntouch $W/host-markers/HOOK_REF_TX || true\n' > hooks/reference-transaction && chmod +x hooks/reference-transaction && git config filter.x.clean 'touch $W/host-markers/FILTER 2>/dev/null; cat' && echo '*.txt filter=x' > info/attributes"
}

make_commits() { # worktree count files
  container exec $N sh -c "cd $1 && i=0; while [ \$i -lt $2 ]; do j=0; while [ \$j -lt $3 ]; do echo \"line \$i \$j\" >> f\$j.txt; j=\$((j+1)); done; git add -A; git commit -q -m \"c\$i\"; i=\$((i+1)); done"
}
say "== small range: 3 commits"
make_commits /ws/wt/docs 3 2 >/dev/null 2>&1
plant
export_bundle() { # worktree range outfile
  t0=$(ms); container exec $N git -C "$1" bundle create - "$2" > "$3" 2>/dev/null; rc=$?; t1=$(ms)
  say "bundle create exit $rc, $(wc -c < "$3" | tr -d ' ') bytes, $((t1 - t0)) ms"
}
export_bundle /ws/wt/docs main..agent/docs "$W/small.bundle"

# The supervisor's bare repository, isolated configuration: no system/global config, hooks off.
export GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null
H="git -c core.hooksPath=/dev/null -c core.fsmonitor=false"
git init -q --bare "$W/import.git"
$H -C "$W/import.git" fetch -q "$W/forge.git" main:refs/heads/main 2>&1 | head -2
t0=$(ms); $H -C "$W/import.git" bundle verify "$W/small.bundle" 2>&1 | tail -1; t1=$(ms)
$H -C "$W/import.git" fetch -q "$W/small.bundle" agent/docs:refs/heads/agent/docs 2>&1 | head -2; t2=$(ms)
say "verify $((t1 - t0)) ms, fetch $((t2 - t1)) ms; imported tip: $($H -C "$W/import.git" log --oneline -1 agent/docs)"

say "== large range: 400 commits x 20 files plus a 60 MB blob"
container exec $N sh -c 'cd /ws/wt/runtime && head -c 62914560 /dev/urandom > blob.bin && git add blob.bin && git commit -q -m blob' >/dev/null
make_commits /ws/wt/runtime 400 20 >/dev/null 2>&1
export_bundle /ws/wt/runtime main..agent/runtime "$W/large.bundle"
t0=$(ms); $H -C "$W/import.git" bundle verify "$W/large.bundle" 2>&1 | tail -1; t1=$(ms)
$H -C "$W/import.git" fetch -q "$W/large.bundle" agent/runtime:refs/heads/agent/runtime 2>&1 | head -2; t2=$(ms)
say "verify $((t1 - t0)) ms, fetch $((t2 - t1)) ms; imported commits: $($H -C "$W/import.git" rev-list --count main..agent/runtime)"

say "== a truncated and a corrupt large bundle are refused"
# a fresh repository: objects the first import already brought in would hide a bad pack
git init -q --bare "$W/import2.git"
$H -C "$W/import2.git" fetch -q "$W/forge.git" main:refs/heads/main
LS=$(wc -c < "$W/large.bundle" | tr -d ' ')
head -c $((LS / 2)) "$W/large.bundle" > "$W/trunc.bundle"
$H -C "$W/import2.git" bundle verify "$W/trunc.bundle" >/dev/null 2>&1; say "truncated to half: verify exit $?"
$H -C "$W/import2.git" fetch -q "$W/trunc.bundle" agent/runtime:refs/heads/t >/dev/null 2>&1; say "truncated to half: fetch exit $?"
cp "$W/large.bundle" "$W/corrupt.bundle"; head -c 4096 /dev/urandom | dd of="$W/corrupt.bundle" bs=1 seek=$((LS / 2)) conv=notrunc 2>/dev/null
$H -C "$W/import2.git" fetch -q "$W/corrupt.bundle" agent/runtime:refs/heads/c >/dev/null 2>&1; say "corrupted in the middle: fetch exit $?"
say "refs after the refused imports: $($H -C "$W/import2.git" for-each-ref --format='%(refname)' | tr '\n' ' ')"

say "== nothing planted in the workspace ran on the host during verify and fetch"
ls -A "$W/host-markers" | sed 's/^/MARKER on host: /'; [ -z "$(ls -A "$W/host-markers")" ] && say "no marker: nothing ran on the host"
say "== control: host git IN the workspace clone does run the planted config (this is why the host never does that)"
( cd "$W/ws/repo" && GIT_CONFIG_NOSYSTEM=1 git status >/dev/null 2>&1; GIT_CONFIG_NOSYSTEM=1 git checkout -q main 2>/dev/null; GIT_CONFIG_NOSYSTEM=1 git add -A 2>/dev/null )
ls -A "$W/host-markers" | sed 's/^/MARKER after host git in the workspace: /'
say "== the environment kept running: $(container list | awk -v n=$N '$1==n{print $5}')"
