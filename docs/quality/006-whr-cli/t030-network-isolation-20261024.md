# T030 init with physical network isolation

The dedicated literal `TestInitNoGitNoNetwork` lives in the new Linux-only
`apps/whr/internal/commands/init_network_linux_test.go`. Current corrected
lineage is based on `95ce4621fdde505654eb8fb8a468ec128b65d0a1` (F1 below).
The initial measurement used base `f2d96805383410f401826877b634d2c03780e977`,
verified by fetch, and original test commit
`202fa64d7af4ba1785281340859715d101819557`.
Measured on 2026-10-24 with Go `go1.27.2 linux/arm64`. No product file,
dependency, command flag or production environment hook is added.

## Executed boundary

The parent first starts its own Go test executable through `unshare -rn --`,
creating user and network namespaces. If native policy restricts that path, it
tries the established passwordless `sudo -n unshare --net -- setpriv` strategy,
setting the invoking UID/GID and clearing supplementary groups before
`env -i` restores the exact closed child environment. This is a test-compiler executable, not a
standalone CLI build or installed artifact. Its closed environment contains
only test coordination values, private HOME/XDG config/state/cache/TMP paths,
a private PATH, TERM and LANG. No host profile, credential or Docker socket
configuration is inherited.

Before attempting any network negative control or invoking commands, the child
checks all of the following:

- `/proc/self/ns/net` differs from the parent's captured namespace identity.
- `net.Interfaces` reports exactly one interface: down loopback.
- There are no IPv4 routes. Any IPv6 route entries are reject-only loopback
  entries, with no routable IPv6 entry.

The final logged green run measured parent `net:[4026532498]`, child
`net:[4026532624]`. Only after those checks, a 250 ms TCP negative control to
literal documentation address `203.0.113.1:443` returned ENETUNREACH. It performs
no DNS lookup and cannot send an external packet from that verified namespace.
The parent never performs that connection attempt. Namespace/interface/route
conditions are checked again after the command flow.

The child then executes actual production command handlers for dry-run init,
unattended init, validate of the generated files, and an identical no-op init.
All return 0 with schema-conforming single JSON envelopes. Dry-run leaves the
workspace snapshot unchanged; validate is read-only; no-op reports no applied
actions and leaves the written tree unchanged. Init reports git_checks not_run
and runtime_enforcement not_in_this_release.

## Git, startup and private-state evidence

The private PATH supplies recording executables for git, docker, devcontainer,
ssh, curl, wget, fish, bash and sh. Each records its name and exits harmlessly
with 97. A separate execution of the owned Git trap proves it records `git` and
exits 97. An explicit `/bin/sh` invocation sources only the owned private
.profile canary, proving startup execution records `startup`. The exact
`git\nstartup\n` trace is checked and cleared before the command flow.

Owned profile/bash/zsh/fish startup files contain recording canaries. Every
subsequent init/validate invocation finds no trace file. Recursive snapshots of
the private HOME/XDG/TMP tree, including file content hashes, modes, sizes and
modification times, remain identical throughout. The source commands therefore
pass this measured PATH/startup canary and private-state boundary. This is not
syscall tracing of arbitrary absolute executable paths or proof that every
possible file read is absent.

Network calls are physically isolated and blocked from external reachability;
network attempts by product code are not instrumented or counted. This test
does not claim zero attempted network calls. The deliberately attempted
negative-control dial is test evidence, not product activity.

## Bounds, unavailable hosts and regression probes

The child gets a 15 s operation context and 20 s Go test timeout; the parent has
a 25 s context per candidate, its own process group, parent-death signal, group
cancellation and cleanup, and a 1 s wait delay. At most two candidates run. Captured stdout/stderr and total child output
are bounded at 32 KiB. When neither candidate can run, namespace/tool unavailability is
`FAILED, NOT SKIPPED` if CI is nonempty or WHR_REQUIRE_NETNS=1; only optional
local runs report `SKIPPED, NOT PASSED`, with native reasons. Child assertion failures, timeout,
output overflow or missing completion evidence fail rather than skip. This
Linux run executed successfully, without a skip; no macOS run is claimed.

Two compiled temporary overlays each produced assertion failures (exit 1):

1. Add `exec.CommandContext(ctx, "git", "--version").Run()` to initHandler and
   the required os/exec import: the namespace child detects the Git trace during
   the actual command flow.
2. Remove `-rn` from this test's unshare invocation: the owned child detects the
   unchanged namespace identity and fails before reaching the dial. The failed
   run contains no negative-control connection evidence; no host-namespace dial
   is made by this probe.

These are regression probes, not newly discovered product defects. Overlay
sources, JSON replacement maps and complete initial/final green and RED logs
are local under `/tmp/whr-t030-network-evidence-3yz1fdp5/`. To reproduce, use
Go's `-overlay=<replacement-map.json>` with the described temporary source
replacement and the focused command below. Do not retain a product mutation.

## PR 249 F1: required CI proof and restricted-userns fallback

The initial PR head `3dbc159b17d181d7856601011bfefc6ddef17768` skipped on a
restricted-userns runner even when CI was set. The independent
[review](https://github.com/wstein/workharbor/pull/249#pullrequestreview-5640068987)
identified this as F1. The existing workspacecfg netnsCandidates/skipForbidden
strategy is now mirrored in this test file without a product dependency or
change to unrelated tests. Absolute tool paths are used; sudo is noninteractive.
A native refusal is respected, not bypassed. Child assertions never trigger a
fallback or skip. Existing before-dial isolation checks, 32 KiB output bounds
and unconditional final process-group kill remain in place.

Before the fix, `TestInitNetworkNamespaceRestrictionFailsRequired` executed
this owned test binary with a restricted-unshare stub and separately CI=true
and WHR_REQUIRE_NETNS=1. Both produced the old silent-success/skip behavior and
failed the new assertion (compiled semantic RED). The fixed test requires a
nonzero child exit with FAILED, NOT SKIPPED. The stub PATH contains no sudo,
so this specifically covers exhaustion of available candidates.
`TestInitNetworkNamespaceFallbackContract` also failed against the prior
direct-only candidate builder before implementation. It now checks exact
sudo/unshare/setpriv/env -i argument order, original UID/GID, cleared groups,
exact closed environment, unchanged input environment slice, and absence of
fallback candidates when any required tool is missing.

The fix was authored after a pure rebase onto
`95ce4621fdde505654eb8fb8a468ec128b65d0a1`. Both original commits compared equal
with git range-diff at intermediate head
`bd5873c43430b6a6321ee43f44fbe47b3db748fa`; the test correction is
`da9dec8da603f4fb2bbfe79f409f75a4963dcedd`.

On this Linux container, the final required run with CI=true and
WHR_REQUIRE_NETNS=1 passed all three focused IDs and explicitly reported the
**unprivileged user namespace (-rn)** path. A separate temporary test overlay
forced the existing sudo candidate: native sudo/unshare --net returned
Operation not permitted, so that probe correctly failed with FAILED, NOT
SKIPPED before any namespace child or negative-control dial started. Runtime
fallback success is therefore **not established locally**. The exact-head
GitHub CI execution/path remains pending; green CI must include actual
namespace execution or fail the test. No GitHub path is inferred from the local
result or the argv contract test.

Four compiled regression overlays against the correction produced assertion
failures: removing the required-proof guard; removing env -i; removing -rn
(failed namespace identity before dialing); and invoking Git during init.
Complete RED, green, native-fallback-refusal, vet and source logs are under
`/tmp/whr-t030-network-f1-akda3yhp/`. This supersedes the initial unconditional
local/CI skip policy only. The PATH/startup-canary and uncounted-network-attempt
limits above remain unchanged (review F2); final group cleanup remains intact
(review F3).

## Checks and remaining acceptance

From apps/whr, with `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off`:

```sh
CI=true WHR_REQUIRE_NETNS=1 go test -count=1 -v ./internal/commands -run '^TestInit(NoGitNoNetwork|NetworkNamespaceRestrictionFailsRequired|NetworkNamespaceFallbackContract)$'
go vet ./internal/commands
```

Both passed. `python3 scripts/check_repo.py`, targeted typos, gofmt and
`git diff --check` passed. Full raw logs were saved before returning bounded
status summaries. The earlier source-only acceptance record remains true for
its own historical run; this separate evidence adds the namespace measurement.

No full suite, race, standalone build, install, full make, host runtime/browser
acceptance, task/progress ticks or publication is included. This proves the
bounded source-execution init/validate flow works with external network access
physically unavailable; it does not complete T034's built-binary and platform
handoff. Independent exact-head review and broader pristine gates remain
separate.
