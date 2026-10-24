# T030 init with physical network isolation

The dedicated literal `TestInitNoGitNoNetwork` lives in the new Linux-only
`apps/whr/internal/commands/init_network_linux_test.go`. Base:
`f2d96805383410f401826877b634d2c03780e977`, verified by fetch before authoring;
test commit: `202fa64d7af4ba1785281340859715d101819557`.
Measured on 2026-10-24 with Go `go1.27.2 linux/arm64`. No product file,
dependency, command flag or production environment hook is added.

## Executed boundary

The parent starts its own Go test executable through `unshare -rn --`, creating
user and network namespaces. This is a test-compiler executable, not a
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
a 25 s context, its own process group, parent-death signal, group cancellation
and cleanup, and a 1 s wait delay. Captured stdout/stderr and total child output
are bounded at 32 KiB. Namespace/tool unavailability is an explicit
`SKIPPED, NOT PASSED` with its native reason. Child assertion failures, timeout,
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

## Checks and remaining acceptance

From apps/whr, with `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off`:

```sh
go test -count=1 -v ./internal/commands -run '^TestInitNoGitNoNetwork$'
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
