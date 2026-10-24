# T030 physical ENOSPC evidence

This test-only unit measures real kernel ENOSPC in a bounded private filesystem,
separate from the existing injected-error matrices and the remaining
[T030 core acceptance](t030-init-core-20261024.md). Base:
`f2d96805383410f401826877b634d2c03780e977`, measured 2026-10-24 with
Go `go1.27.2 linux/arm64`, umask 022. Production is unchanged.

## Isolation and actual failures

[TestInitPhysicalENOSPC](../../../apps/whr/internal/scaffold/enospc_linux_test.go)
starts an owned test-executable helper through `unshare -rnm`. Before mounting,
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

Children have private HOME/TMPDIR/PATH, bounded output, a 15-second deadline,
owned process-group timeout cleanup and a two-second wait bound. Unsupported
namespace or mount permissions produce an explicit SKIP reason; skipped cases
are not physical-ENOSPC acceptance. This Linux-only test made no network
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
