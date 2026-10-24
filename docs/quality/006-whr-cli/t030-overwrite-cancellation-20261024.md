# T030 overwrite cancellation matrix

This test-only unit covers the missing overwrite context-cancellation matrix
from the [T030 core record](t030-init-core-20261024.md). Base:
`f2d96805383410f401826877b634d2c03780e977`, measured 2026-10-24 on
Go `go1.27.2 linux/arm64`, umask 022. Existing production passes this evidence;
no product defect, behavioral change or complete SC-018 acceptance is claimed.

[TestInitOverwriteCancellationEveryStep](../../../apps/whr/internal/scaffold/overwrite_cancel_test.go)
first observes all 28 success-path callbacks of a consented manifest overwrite.
It pins the event count and required journal-write, temporary-write/sync,
rename, post-overwrite and pre-validation events. Each callback then gets its
own fresh temporary fixture and context cancellation. The callback returns nil,
so the writer must observe the cancelled context even at a fault callback.

Every case requires exit 72 with no remaining recovery paths. A complete
before/after tree comparison checks identical path inventory, file bytes and
modes, including the original manifest's distinct 0640 mode. All pre-existing
directory identities are retained; devcontainer and project-data files also
retain their identities and modification times. No journal, temporary, generated
file or newly created directory may remain. Overwrite restoration may replace
the manifest inode; that identity is deliberately not required to stay the same.

## Executed RED and GREEN evidence

Commands ran from `apps/whr` with
`GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off`. The first new-test execution was
the ignored-context mutation RED; both mutations used compiler overlays of
[apply.go](../../../apps/whr/internal/scaffold/apply.go), with tracked
production source unchanged throughout.

| Check | Actual result |
| --- | --- |
| Insert `ctx = context.Background()` at entry to `Proposal.apply`; run the new test | Exit 1: all 28 subcases fail semantically because cancellation returns success instead of 72. |
| Same ignored-context overlay; run existing `TestInitOverwriteFailureInjectionEveryStep` | Exit 0: the old ENOSPC matrix still passes, demonstrating that it does not cover this cancellation behavior. |
| Replace `remaining := t.rollback()` with `remaining := []string{}`; run the new test | Exit 1: 20 subcases fail at restoration/inventory assertions, rather than compilation or setup. |
| `go test -count=1 -run '^(TestInitOverwriteCancellationEveryStep\|TestInitOverwriteFailureInjectionEveryStep\|TestInitRollbackRestoresOverwriteModeAndBytes)$' -json ./internal/scaffold` | Exit 0: 59 test/subtest outcomes pass, comprising three top-level tests and 28 cancellation plus 28 ENOSPC subcases; zero failures/skips. |

For either mutation, copy `apply.go` to a temporary file, make only the stated
replacement, and supply an overlay JSON `Replace` entry from the absolute
original path to the copy. Run
`go test -count=1 -overlay=<overlay.json> -run '^TestInitOverwriteCancellationEveryStep$' -json ./internal/scaffold`;
for the control, replace the test selection with
`^TestInitOverwriteFailureInjectionEveryStep$`. The semantic failures establish
fresh regression evidence, not historical RED for the original implementation.

Package `go vet`, test-file `gofmt -l`, the repository source checker,
`typos` on both new files and `git diff --check` passed. The source checker
reports bootstrap checks only, with product runtime not evaluated.

## Scope and remaining gates

The test uses the existing unexported apply callback; there is no production
test hook, flag or environment variable. It adds context cancellation at every
observed success-path event of this overwrite fixture, including fault callbacks
and checkpoints. It does not inject cancellation during rollback or generate
actual operating-system signals. The existing ENOSPC matrix remains separate.

Every-step actual SIGINT/SIGTERM, physical filesystem exhaustion, process-wide
network denial and other applicable T030 evidence remain separate units.
Historical implementation RED remains unproved; fresh regression evidence is
reported independently. No task/progress checkbox or CI inventory changes here.
Full `make check`, published-head CI and independent review remain coordinating
handoff gates. No full suite, standalone build, installation, race run, runtime
activation, physical disk exhaustion or network operation was performed.
