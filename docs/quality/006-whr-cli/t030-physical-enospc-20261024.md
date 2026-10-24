# T030 physical ENOSPC evidence

This test-only unit measures real kernel ENOSPC in a bounded private filesystem,
separate from the existing injected-error matrices and the remaining
[T030 core acceptance](t030-init-core-20261024.md). Base:
`f2d96805383410f401826877b634d2c03780e977`, measured 2026-10-24 with
Go `go1.27.2 linux/arm64`, umask 022. Production is unchanged.

## Isolation and actual failures

[TestInitPhysicalENOSPC](../../../apps/whr/internal/scaffold/enospc_linux_test.go)
prefers an owned test-executable helper through `unshare -rnm`, with the
original-owner mapping fallback below when native setup is refused. Before mounting,
the helper verifies its user, network and mount namespace identities differ
from the parent's, the owned mountpoint is a real empty directory, and mount
propagation is not shared. It mounts only that directory with a 1 MiB tmpfs.
Before filling, both the mount and the open filler descriptor must identify the
new device, tmpfs type and capacity at most 1 MiB. Filling is explicitly bounded.
No host filesystem is filled or mounted, and the underlying directory remains
empty with its original identity after the child exits.

At the selected existing writer callback, a sibling filler on that private
filesystem is written until the kernel returns ENOSPC. A separate positive
control write must also return ENOSPC. The callback returns nil: the subsequent
product write must encounter the full filesystem itself. For complete rollback
cases, the filler is closed and removed at the first existing rollback callback
so restoring old bytes has space. The partial case deliberately retains it
until after the product outcome and inventory have been checked.

| Physical case / selected callback | Required and observed outcome |
| --- | --- |
| Create manifest: `write:workharbor.json` | Exit 74 with the actual disk-full write error; complete original tree restored. |
| Overwrite temporary: `write-temp:.whr-init-tmp-00000001` | Exit 74 with disk-full error; original manifest retained and temporary removed. |
| After manifest replacement: `write:AGENTS.md`, free filler on rollback | Exit 74 with disk-full error; original manifest bytes and 0640 mode restored. |
| Same late write, keep filesystem full through rollback | Exit 73; exactly `.whr-init-tmp-journal` and `workharbor.json` reported remaining. The marker exists, the manifest remains changed, and no other generated paths remain. |

Every complete case compares the entire original path inventory, bytes and
modes and retains pre-existing directory/devcontainer/project-file identities.
The partial case checks the exact reported recovery set against the actual
inventory and preserves all other original content. Private fixture teardown
does not count as product rollback or recovery of that partial state.

Children have an exact HOME/TMPDIR/PATH/TERM environment cleared by `env -i`,
bounded output, a 15-second deadline,
owned process-group timeout cleanup and a two-second wait bound. Unsupported
namespace or mount permissions produce an explicit SKIP reason only outside
required execution. Nonempty `CI`, `WHR_REQUIRE_NETNS=1` or
`WHR_REQUIRE_PHYSICAL_ENOSPC=1` instead requires `FAILED, NOT SKIPPED` after
all native candidates fail. Repository application acceptance also rejects any
test skip; a local package exit 0 with skips is not CI acceptance. This Linux-only test made no network
connection and introduced no product hook, dependency or configuration flag.

## Executed checks

Commands ran from `apps/whr` with
`GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off`. The first new-test executions were
the deliberate mutation RED controls; tracked production source stayed intact.

| Check | Actual result |
| --- | --- |
| Overlay `remaining := t.rollback()` with `remaining := []string{}` in `apply.go`; select `^TestInitPhysicalENOSPC$/^overwrite-restore$` | Expected RED, exit 1: required rollback callback/restoration is missing after an actual disk-full product write. |
| Overlay both `_, err = f.Write(data)` assignments with `_, _ = f.Write(data)`; select `^TestInitPhysicalENOSPC$/^create-write$` | Expected RED, exit 1: ignored kernel write failure yields conflict instead of the required disk-full exit 74. |
| `go test -count=1 -run '^TestInitPhysicalENOSPC(Process)?$' -json ./internal/scaffold` | Exit 0: all four physical cases, parent and helper pass (six outcomes); zero failures/skips. |

To reproduce a mutation, copy
[apply.go](../../../apps/whr/internal/scaffold/apply.go) to a temporary file,
make the stated replacement, supply a Go overlay JSON `Replace` entry from the
absolute source path to that copy, and add `-overlay=<overlay.json>` to the test
command with its selection above. These semantic failures establish fresh
regression evidence, not historical RED for the original implementation.

Package vet, format, repository source, spelling and whitespace checks passed;
the source checker reports bootstrap checks, not product-runtime acceptance.

This is four representative physical write points, not physical disk exhaustion
after every writer step or proof for every filesystem/provider. Whole T030 and
SC-018 acceptance, published-head CI and independent review remain separate.
No task/progress tick, full suite, standalone build, race run, installation,
runtime activation, host-disk exhaustion or host-namespace mutation was performed.

## CI mapping refusal and bounded fallback correction

The original head `fea08594309c695427d6fa4ea089fe586670e356` passed locally,
but [GitHub bootstrap failed](https://github.com/wstein/workharbor/actions/runs/40133079737/job/120989589858)
on all four cases: unprivileged `unshare` could not write `/proc/self/uid_map`
(Operation not permitted). This is a real native-runner restriction, not a
rerunnable flake. [Review F1/F2](https://github.com/wstein/workharbor/pull/250#pullrequestreview-5640097663)
identified both the absent fallback and the incomplete refusal/skip description.
The own topic was rebased from `f2d96805` onto
`54c4b271745fe78800a523607dd00a9124d22f01`; range-diff showed both original
commits equal before this correction. No foreign or accepted commit was rewritten.

The privileged chain accepts only canonical `/usr/bin` tools with root-owned,
files and ancestors that are not group/other writable; an owner-writable PATH wrapper is refused.
The fallback uses fixed absolute native-tool paths:
`sudo -n -- unshare --user --mount --net --map-users=0:<owner-uid>:1 --map-groups=0:<owner-gid>:1 --setgroups=allow --setuid=0 --setgid=0 -- setpriv --clear-groups -- env -i <four-field environment> <owned test executable>`.
Local util-linux 2.41.5 help and `unshare(1)` confirm the
`inner:outer:count` mapping syntax and group-mapping behavior. A non-root original host owner is mandatory. The helper
requires exact single-entry UID/GID maps from inner 0 to the original outer
owner, inner real/effective UID/GID 0, and no supplementary groups on the
fallback. The Go writer never runs as root in the initial namespace or as host
UID 0: its host UID/GID are the original owner's. Capabilities exist only in
the new user namespace, the same boundary already used by `unshare -rnm`.
All three changed-namespace, private-propagation, owned-mountpoint, new-device,
tmpfs type/capacity and bounded-fill guards remain mandatory.

Both `unshare failed` and `write failed /proc/self/uid_map` native refusals
permit trying the mapped candidate. A helper-start marker makes child
assertions hard failures. Output overflow, context deadlines and pipe wait
deadlines are typed hard failures checked before any native-looking prefix;
they cannot trigger fallback or be hidden as unavailable capability.

| New focused evidence | Actual result |
| --- | --- |
| Fallback argv/exact environment and required-refusal tests before correction | Semantic RED: argv lacked the mapped candidate; ordinary EPERM was skipped despite required proof, and uid_map EPERM lacked the required explicit failure result. |
| Host-UID-zero launcher negative control before its guard | Semantic RED: the candidate builder still emitted a launcher for host UID 0; the new guard now refuses that case before launching any Go helper. |
| Native-prefix-first classifier compiler mutation | Expected RED: five output/deadline boundary subcases and the helper-assertion guard fail (seven failed test/subtest outcomes). |
| `go test -count=1 -run '^TestInitPhysicalENOSPC' -json ./internal/scaffold` after correction | Exit 0: 21 test/subtest outcomes, zero failures/skips. All four normal physical cases ran via the direct launcher; `TestInitPhysicalENOSPCMappedFallback` separately ran a real overwrite/restoration case via the mapped sudo launcher. |

The restricted-native regression executes the owned test binary with controlled
native refusal scripts and `WHR_REQUIRE_NETNS=1`; both refusal forms must exit 1 with
`FAILED, NOT SKIPPED`, never a skipped or passing proof. The argv test pins
original-owner maps, supplementary-group clearing and `env -i`. The helper
validates those identity/environment properties during the actual mapped run.
Each successful physical case logs `direct` or `mapped` and the native-refusal
count, so the published CI log can identify which path ran. Corrected-head
GitHub CI has not run in this authoring record; no green hosted run is claimed.

### Canonical privileged arguments

Independent review of `5d9e0594` found that checking a resolved path's ownership
while retaining the original lookup string still allowed an owner-writable
symlink to enter sudo's argv. No privileged alias execution was used to probe it.
The new `TestInitPhysicalENOSPCMappedArgvSurvivesMutableAliases` first reproduced
four semantic failures by building the candidate through temporary aliases,
replacing them with owned scripts, and inspecting argv only.

The resolver now returns each verified canonical path. The privileged candidate
is constructed exclusively from those canonical sudo/unshare/setpriv/env paths
and their verified root-owned, non-group/other-writable ancestors; owner-writable aliases
are absent from that chain. The ordinary owner launcher is built separately.
The alias-replacement regression and canonical system-path positive controls now
pass, without executing an alias. This proof covers mutable lookup aliases;
it does not claim protection against a privileged administrator replacing
trusted system binaries. Package vet, format, source, spelling and whitespace
checks also passed for the correction.
