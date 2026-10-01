#!/bin/sh
# Spike #89, criteria 3 and 5:
#  3. what one agent can do to another's worktree and branches (one trust domain),
#  5. whether GIT_CONFIG_COUNT/KEY/VALUE with core.hooksPath=/dev/null and core.fsmonitor=false stops a
#     planted hook and fsmonitor when the human runs git in a console, and what it does not stop.
set -u
N=whr-spike89c; W=
cleanup() { container delete --force $N >/dev/null 2>&1; [ -n "$W" ] && rm -rf "$W"; }
cleanup
W=$(cd "$(mktemp -d)" && pwd -P)
trap cleanup EXIT
IMG=golang:1.27.1-trixie
say() { printf '%s\n' "$*"; }
git init -q --bare "$W/forge.git"
git clone -q "$W/forge.git" "$W/seed" 2>/dev/null
( cd "$W/seed" && git -c user.name=h -c user.email=h@h checkout -q -b main && echo base > README.txt && git add . && git -c user.name=h -c user.email=h@h commit -q -m base && git push -q origin main )
mkdir -p "$W/ws"; git clone -q "$W/forge.git" "$W/ws/repo"
container run -d --name $N -v "$W/ws:/ws" -w /ws -e HOME=/tmp/h "$IMG" sleep 3600 >/dev/null 2>&1
E() { container exec $N sh -c "$1" 2>&1; }
E "mkdir -p /tmp/h; git config --global user.name a; git config --global user.email a@a; git config --global --add safe.directory '*'; mkdir -p /ws/wt; git -C /ws/repo worktree add -q -b agent/docs /ws/wt/docs main; git -C /ws/repo worktree add -q -b agent/runtime /ws/wt/runtime main" >/dev/null

say "== 3. agent 'docs' acting on agent 'runtime' (same environment, same user)"
E "cd /ws/wt/runtime && echo runtime-work > r.txt && git add r.txt && git commit -q -m 'runtime commit' && git log --oneline -1"
E "echo tampered > /ws/wt/runtime/r.txt; echo 'edit the other worktree: exit '\$?"
E "cd /ws/wt/docs && git commit -q --allow-empty -m 'docs writes to the other branch'; git -C /ws/wt/docs update-ref refs/heads/agent/runtime HEAD; echo 'move the other agent branch: exit '\$?; git -C /ws/repo log --oneline -1 agent/runtime"
E "git -C /ws/repo branch -D agent/runtime 2>&1 | head -1; echo 'delete the other branch (checked out elsewhere): exit '\$?"
E "git -C /ws/wt/docs config core.hooksPath /ws/wt/docs/.hooks; echo 'config is shared by all worktrees: '; git -C /ws/wt/runtime config core.hooksPath"
E "ls /ws/repo/.git/worktrees"
E "git -C /ws/repo config --unset core.hooksPath; git -C /ws/repo config core.hooksPath; echo cleaned up for part 5" >/dev/null

say "== 5. console git with GIT_CONFIG_COUNT (run as the human would, in the workspace clone)"
# Plant: a hook, fsmonitor, a clean filter, a diff textconv, an alias and a pager. Markers go to /ws/markers.
E 'mkdir -p /ws/markers; cd /ws/repo/.git && printf "#!/bin/sh\ntouch /ws/markers/HOOK\n" > hooks/post-checkout && chmod +x hooks/post-checkout && printf "#!/bin/sh\ntouch /ws/markers/PRECOMMIT\n" > hooks/pre-commit && chmod +x hooks/pre-commit
git config core.fsmonitor "sh -c \"touch /ws/markers/FSMONITOR; true\""
git config filter.x.clean "sh -c \"touch /ws/markers/FILTER; cat\""
git config diff.y.textconv "sh -c \"touch /ws/markers/TEXTCONV; cat \\\$1\" --"
git config core.pager "sh -c \"touch /ws/markers/PAGER; cat\""
git config alias.st "!touch /ws/markers/ALIAS; git status"
printf "*.txt filter=x diff=y\n" > info/attributes'
run_human() { # label, env prefix
  E "rm -f /ws/markers/*; cd /ws/repo; $2 git checkout -q main 2>/dev/null; $2 git status >/dev/null 2>&1; echo change >> README.txt; $2 git add README.txt >/dev/null 2>&1; $2 git commit -q -m c >/dev/null 2>&1; $2 git -c core.pager='sh -c \"touch /ws/markers/PAGER; cat\"' diff HEAD~1 >/dev/null 2>&1; $2 git --paginate log -1 >/dev/null 2>&1; $2 git st >/dev/null 2>&1; $2 git reset -q --hard HEAD~1 2>/dev/null; true" >/dev/null
  say "$1: markers = $(E 'ls /ws/markers | tr "\n" " "')"
}
run_human "plain git                              " ""
run_human "GIT_CONFIG_COUNT hooksPath+fsmonitor   " "GIT_CONFIG_COUNT=2 GIT_CONFIG_KEY_0=core.hooksPath GIT_CONFIG_VALUE_0=/dev/null GIT_CONFIG_KEY_1=core.fsmonitor GIT_CONFIG_VALUE_1=false"
run_human "GIT_CONFIG_COUNT + filter/pager/textconv unset" "GIT_CONFIG_COUNT=6 GIT_CONFIG_KEY_0=core.hooksPath GIT_CONFIG_VALUE_0=/dev/null GIT_CONFIG_KEY_1=core.fsmonitor GIT_CONFIG_VALUE_1=false GIT_CONFIG_KEY_2=filter.x.clean GIT_CONFIG_VALUE_2=cat GIT_CONFIG_KEY_3=diff.y.textconv GIT_CONFIG_VALUE_3= GIT_CONFIG_KEY_4=core.pager GIT_CONFIG_VALUE_4=cat GIT_CONFIG_KEY_5=alias.st GIT_CONFIG_VALUE_5=status"
