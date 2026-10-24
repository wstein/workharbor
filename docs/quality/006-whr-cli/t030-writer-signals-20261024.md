# T030 real signals at writer steps

This test-only unit exercises actual SIGINT and SIGTERM delivery to the D1
writer. Measured on 2026-10-24 at base
`f2d96805383410f401826877b634d2c03780e977`, Go `go1.27.2 linux/arm64`,
umask 022. Production is unchanged. The first full matrix had one unattributed
failure; four subsequent complete matrices passed. **No all-green aggregate or
resolved stability claim is made.** Independent review/QA must assess that risk.

## Scope and child lifecycle

[TestInitWriterSignalsEveryStep](../../../apps/whr/internal/scaffold/overwrite_signal_test.go)
observes 27 create and 28 overwrite callbacks, then runs both signals at every
event: 110 cases. Each uses an owned child of the Go test executable. The private
test helper calls `Proposal.apply` with `signal.NotifyContext` and the existing
unexported callback, announces the selected event and waits for signal-driven
context cancellation before continuing. The parent sends the real signal only
after that handshake. No product flag, environment hook or test interface is
introduced.

Every child must exit 72 after observing its signal and report no remaining
recovery paths. The parent checks exact original path inventory, file bytes and
modes, including an overwritten manifest's 0640 mode, pre-existing directory
identities, and retained devcontainer/project-file identities and timestamps.
No journal, temporary or generated file/directory may remain. Atomic manifest
restoration may replace its inode.

Child HOME/TMPDIR/PATH are private, with only TERM additionally supplied.
Each output stream is capped at 8 KiB. A five-second child deadline shares a
90-second suite budget; unfinished owned process groups receive SIGKILL and
are reaped with a separate two-second cleanup bound. Cases run sequentially.
The test is built for Linux and macOS; only Linux was measured here.

This is writer-level signal proof through a test helper, separate from the
existing `cmd/whr/TestInitEntrypointSignalsCancelAndCleanUp` main-entrypoint
evidence. It is not a built CLI run, a signal during rollback or proof of every
possible point between filesystem syscalls.

## Executed evidence, including the initial failure

Commands ran from `apps/whr` with
`GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off`. The test file was unchanged
through the mutation checks and all full-matrix runs below (Git blob
`1200bb3e4ba0485a4868d292bf4cd2dbf3d56753`).

| Check | Actual result |
| --- | --- |
| Overlay `apply.go` to insert `ctx = context.Background()` at `Proposal.apply` entry; select overwrite/after:workharbor.json/SIGINT | Expected RED, exit 1: the real signalled child exits 0 rather than 72. |
| Overlay `remaining := t.rollback()` with `remaining := []string{}` at the same selected event | Expected RED, exit 1: original manifest bytes/mode are not restored. |
| First `go test -count=1 -run '^TestInitWriterSignal(Process\|sEveryStep)$' -json ./internal/scaffold` | Exit 1: one of 110 signal cases failed; 109 cases plus the helper passed. Its leaf diagnostic was lost when aggregate tool output was clipped. The 24.66-second suite duration excludes exhaustion of the 90-second budget; the exact event and cause remain unknown. |
| Same command, unchanged rerun | Exit 0: all 110 signal cases, helper and parent passed (112 outcomes), no failures/skips; matrix 17.80 seconds. |
| `go test -count=1 -run '^TestInitWriterSignalsEveryStep$' -json ./internal/scaffold`, three bounded repeats | Each exited 0: 110 cases plus parent (111 outcomes), no failures/skips; 17.46, 17.63 and 17.52 seconds. Full raw output was retained for these repeats; no further matrix runs were performed. |

The deliberate mutations were compiler overlays of
[apply.go](../../../apps/whr/internal/scaffold/apply.go), leaving tracked
production source untouched. Their selection was
`^TestInitWriterSignalsEveryStep$/^overwrite$/^after:workharbor[.]json$/^interrupt$`.
Copy the source to a temporary file, apply the single stated replacement and
provide a Go overlay JSON `Replace` entry to reproduce those checks. These are
fresh regression failures, not historical implementation RED.

Package vet, format, repository source, spelling and whitespace checks passed;
the source checker reports bootstrap checks, not product-runtime acceptance.
No deadline was raised and no product rule changed to hide the initial failure.
The successful ordinary repeats are not separate stress evidence. Review/QA,
published-head CI and the coordinating repository gate remain required before
handoff. Physical ENOSPC, process-wide network denial, other applicable T030
assertions and original historical evidence remain separate. No task/progress
ticks, standalone build, full suite, race run, installation or runtime activation
was performed.
