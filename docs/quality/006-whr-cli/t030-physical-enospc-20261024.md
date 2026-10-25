# T030 physical ENOSPC evidence

This test-only unit measures real kernel ENOSPC in a bounded private filesystem,
separate from the existing injected-error matrices and the remaining
[T030 core acceptance](t030-init-core-20261024.md). Current base: `d18ccc87926084ca3f51d08774f7a627606b255e`; the initial
measurement base was `f2d96805383410f401826877b634d2c03780e977`. Measured
2026-10-24 with
Go `go1.27.2 linux/arm64`, umask 022. Production is unchanged.

## Isolation and actual failures

[TestInitPhysicalENOSPC](../../../apps/whr/internal/scaffold/enospc_linux_test.go)
prefers an owned test-executable helper through `unshare -rnm`, with the
portable premounted fallback below when native setup is refused. The direct
helper requires changed user/network/mount namespaces, an exact single-entry
mapping of inner UID/GID 0 to the original owner, a real empty mountpoint and
private propagation. It mounts only that directory with a 1 MiB tmpfs. The
fallback mounts before dropping privilege and runs Go as the original non-root
owner; its distinct namespace and filesystem guards are detailed below.
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

## Initial executed checks (historical)

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

An initial correction used an exact original-owner user-namespace mapping.
Local util-linux 2.41.5 ran that mapped fallback successfully: the historical
focused run had 21 test/subtest passes and no skips. The topic subsequently
rebased through `86008fab4b0131187c3d3b244fef94a48063b48b` and
`79d2f4eb336514c27b93d220edf7f5c6efe49b0d` with equal own patches. Those are
historical bases; the current base is named above.

The [next hosted run failed](https://github.com/wstein/workharbor/actions/runs/40136570703/job/121003086084):
Ubuntu 24's util-linux 2.39 invokes `newuidmap`, which refuses the runner's
single-owner mapping without a permitted subordinate-ID range. The local 2.41
success did not establish 2.39 portability. [The follow-up review](https://github.com/wstein/workharbor/pull/250#pullrequestreview-5640361847)
therefore replaces that fallback; the old mapped command is no longer used.

## Portable private-mount fallback

The fallback is a fixed literal prelude, invoked as
`sudo -n -- <canonical-env> -i <four-field-environment> <canonical-unshare> --mount --net --propagation private -- <canonical-sh> -eu -c <literal-prelude> <arg0> <quoted-positional-arguments>`.
It does not request a user namespace or invoke mapping helpers. The root stage
receives only the exact four-field HOME/TMPDIR/PATH/TERM launch environment,
verified canonical system tools, numeric original-owner UID/GID, owned fixture
paths, parent namespace identities and the fixed helper arguments. No caller
shell initialization, proxy or secret environment is inherited by the launcher.
The literal is never read from an owner-writable script; fixture paths remain
positional arguments and are never interpolated into root shell source.

Before mounting, the prelude rejects UID 0, malformed IDs, a non-absolute or
unexpected mountpoint name, symlinks, nonempty directories, a changed user
namespace, or unchanged network/mount namespaces. Canonical `mount` uses
`--no-mtab --internal-only` and a tmpfs limited to 1 MiB with mode 0700,
original-owner UID/GID, nosuid/nodev/noexec. It then execs canonical `setpriv`
with the original UID/GID and `--clear-groups`, followed by canonical `env -i`
and the owned test executable. Go and product Apply run only after the drop;
no owner script or Go program runs as initial-namespace root.

Only the `premounted` helper path permits the unchanged user namespace. It
requires changed network/mount namespaces, original non-root real/effective
UID/GID and zero supplementary groups. Before any fixture write/fill, it checks
an empty real directory, private propagation, owner/mode, tmpfs type, capacity
at most 1 MiB and a device different from the parent's original directory.
It cannot mount as the dropped owner; namespace exit removes the private mount.
The direct path retains all three changed namespaces and exact UID/GID maps.

Both ordinary `unshare failed` and `write failed /proc/self/uid_map` refusals
may try the portable candidate. `newuidmap:`/`newgidmap:` diagnostics remain
hard failures, not unavailable-capability classifications. The helper-start
marker prevents assertions becoming native refusals. Typed overflow, context
and pipe-wait deadline failures take precedence over native-looking prefixes.
Required execution still fails explicitly when all launchers are unavailable.

| New focused evidence | Actual result |
| --- | --- |
| Portable-candidate regression against the previous mapping implementation | Semantic RED, exit 1: fallback still requested `--user` and mapping helpers. |
| Owner-only prelude protocol probes | Pass: exact privilege-drop/group-clearing/env-i order and literal positional arguments, including spaces and shell metacharacters; seven invalid-input probes fail before reaching an inert mount recorder. These probes perform no privileged mount or identity change. |
| Widen canonical prefix from `/usr/bin/` to `/usr/` using a Go compiler overlay | Expected RED, exit 1: the root-owned, nonwritable `/usr/sbin/nologin` negative control is incorrectly accepted. Neither that tool nor its temporary alias is executed. |
| Replace the exact direct map predicate with `true` using a compiler overlay | Expected RED, exit 1: another owner's mapping is accepted. Negative assertions also cover wrong inner ID, range size and extra entries. |
| Replace the prelude's `--reuid=$fixture_uid` with `--reuid=0` using a compiler overlay | Expected RED, exit 1: the owner-only recorder observes UID 0 instead of the original owner; no privileged execution occurs. |
| `go test -count=1 -run '^TestInitPhysicalENOSPC(Fallback\|DirectMap\|MappingHelper\|Prelude\|Privileged\|Required\|Native\|Child)' -json ./internal/scaffold` | 27 test/subtest passes, zero failures/skips. Includes closed argv/environment, restricted-native required failures, hard bounds and mutable-alias controls. |
| `go test -count=1 -run '^TestInitPhysicalENOSPC(PremountedFallback)?$' -v ./internal/scaffold` | All four actual physical cases pass via `direct` with zero native refusals (five passing outcomes including parent). Forced premounted fallback explicitly SKIPs locally: sudo/unshare returns EPERM before the prelude. Package exit 0 with this skip is not CI acceptance or staged-runtime proof. |

At that checkpoint, the new staged fallback had not executed successfully on this local host, whose
initial user namespace lacks the required mount/network capability. Authentic
hosted execution was pending publication and a green required CI run. The later
hosted proof below supersedes that pending result. Each successful case logs the actual launcher and
refusal count. Package vet, gofmt, source, spelling and whitespace checks passed;
full-suite/build/race checks were not run for this bounded correction.

### Canonical privileged arguments

Independent review of `5d9e0594` found that checking a resolved path's ownership
while retaining the original lookup string still allowed an owner-writable
symlink to enter sudo's argv. No privileged alias execution was used to probe it.
The original alias regression first reproduced
four semantic failures by building the candidate through temporary aliases,
replacing them with owned scripts, and inspecting argv only.

The resolver now returns each verified canonical path. The privileged candidate
is constructed exclusively from canonical sudo/unshare/setpriv/env/sh/mount/readlink paths
and their verified root-owned, non-group/other-writable ancestors; owner-writable aliases
are absent from that chain. The ordinary owner launcher is built separately.
`TestInitPhysicalENOSPCPrivilegedArgvSurvivesMutableAliases` now covers all seven
canonical paths after replacing every owner-writable alias. Canonical positive
controls pass without executing any alias. This proof covers mutable lookup aliases;
it does not claim protection against a privileged administrator replacing
trusted system binaries. Package vet, format, source, spelling and whitespace
checks also passed for the correction.

The canonical-alias regression uses the same capability policy when an optional
system-tool positive control is missing or untrusted: explicit local skip,
but hard failure under CI or required-proof flags. Its affected regression and
the required-policy test passed after this portability correction; no privileged
alias execution or complete-suite rerun was used.

## Hosted fallback proof and runtime guard follow-up

[Run 40141145793](https://github.com/wstein/workharbor/actions/runs/40141145793/job/121013356109)
at `6f478bdca648b5c99cb792bd3013ed1cf1b3c7d9` passed all four physical
cases through the `premounted` launcher after one native refusal each. The
dedicated `TestInitPhysicalENOSPCPremountedFallback` also passed through that
launcher with zero native refusals. The bootstrap event stream reported 3043
passing Go outcomes, without failure or skip events, as read back by the
[independent review](https://github.com/wstein/workharbor/pull/250#pullrequestreview-5640572957).
This proves the hosted portable fallback; the local forced-fallback refusal
above remains historical evidence for that container.

On 2026-10-25, review F6 gained owner-only negative subprocess tests against the
actual `TestInitPhysicalENOSPCProcess` entry point. Its existing premounted
identity guard now runs before namespace inspection. Identity and supplementary
group failures have separate diagnostics, retaining both checks while allowing
an ID mismatch to be distinguished from inherited owner groups. The fixed root
prelude, canonical argv and namespace/mount/fill protections are unchanged.

The negatives pass a wrong nonzero expected UID, a wrong expected GID, or an
extra nonsecret environment field. They require the corresponding guard's
specific diagnostic and an untouched empty mountpoint. Each test subprocess is
the ordinary owner, with an exact four-field environment except for that one
negative field, bounded output, a five-second deadline and group cleanup.
All namespace IDs are unchanged, so even bypassing a guard cannot reach a
mount or fill. A compiled overlay disabling all four real/effective UID/GID
comparisons, while retaining parsing and supplementary-group checks, fails
both identity negatives. Disabling the actual closed-environment assertion
fails the environment negative. Neither probe uses root, sudo, unshare, a
mount or product execution.

These tests pin the runtime guard blocks. An ordinary owner cannot independently
vary real and effective identities, so this is not separate mutation coverage
for each individual UID/EUID/GID/EGID comparison. Hosted runtime proof above
belongs to the accepted prior head; fresh exact-head checks and review remain
required for the follow-up.
