# 0013-whr-exit-codes: A fresh `whr` exit-code table

Date: 2026-10-14
Status: Proposed; owner decision H15:b recorded 2026-10-13

## Context

Feature 006 requires one published, frozen exit-code table (FR-018) and
pass-through of remote statuses 0-255 for interactive commands (FR-019).
[ADR 0010](0010-go-whr-cli-layout.md) defers the numbers to this ADR. Owner
decision H15:b: start fresh. The legacy `whr` table (0-10) and the development
script tables of features 003 and 004 are reference only and are not inherited.
Related: [ADR 0006](0006-optional-ai-execution.md),
[ADR 0009](0009-in-container-ai-agents.md),
[ADR 0011](0011-readonly-spec-dashboard.md),
[ADR 0012](0012-persistent-workspace-console.md). The `whr` feature spec is
[006](../../specs/006-whr-cli/spec.md).

## Decision

### Table

CLI-owned codes live in 64-79 (the BSD sysexits neighbourhood, clear of the
ubiquitous 1 and 2, shell-reserved 126-127 and signal statuses 128+n). 75-79 stay
reserved and unassigned; any change to this table needs a new ADR that supersedes it.

| Code | Name | Category and use |
| --- | --- | --- |
| 0 | ok | Success |
| 64 | usage | Bad arguments, flags or unsupported `--json` |
| 65 | not_found | Workspace, host, agent, operation, session or session interface absent; a missing interface uses 65, not 69 |
| 66 | unauthenticated | No, expired or revoked credential; remediation hint issued |
| 67 | forbidden | Authenticated but not authorized, including foreign workspaces |
| 68 | conflict | Conflict or wrong state, including idempotency-key mismatch |
| 69 | incompatible_contract | Incompatible session/contract version; both versions named (FR-029, US5-4) |
| 70 | needs_human | Required human decision refused in unattended or answer-file mode, or not given (FR-013) |
| 71 | timeout | Deadline exceeded; operation neither cancelled nor reported complete; operation id printed |
| 72 | interrupted | User interrupt, or transport lost during an interactive command |
| 73 | partial_failure | Multi-step command stopped midway; completed and remaining steps listed |
| 74 | internal | Unclassified failure; never used where a category above applies |

### Relation to remote status passthrough

`shell`, `console`, `ssh` and `agent attach` exit with the remote status 0-255
unchanged when the remote side exits (FR-019); on transport loss they exit 72.
Remote statuses are not remapped or clamped, because passthrough must stay faithful
for scripts. Overlap with 64-79 is therefore possible and is accepted. Instead of a
second channel that scripts would have to learn, the CLI keeps itself
distinguishable by two existing signals:

1. Every CLI-owned failure also writes a `whr:` line to standard error. This is
   advisory in human mode: remote stderr is relayed, so a remote program
   (including `whr` inside the workspace) can print a similar line.
2. With `--json` (not available for `ssh`), the exit event carries `kind` of `cli`
   or `remote`, plus the code.

For `ssh`, a status in 64-79 is ambiguous by design, including a remote 72 versus
transport-loss 72. Scripts needing certainty use `shell` or `console` with `--json`.

Choosing 64-79 keeps the common remote statuses (0, 1, 2, 126, 127, 130, 255)
collision-free. Scripts that must tell the two apart in the rare overlap read the
JSON `kind` (not for `ssh`). Non-interactive commands only ever use the table above.

### Freeze test plan

The table is frozen by the language-neutral contract
[`contracts/cli/exit-codes.json`](../../contracts/cli/exit-codes.json). Planned Go
tests in `apps/whr/internal/exitcode/table_test.go` (offline, no network, no
credentials, per [ADR 0007](0007-explicit-ci-acceptance.md); they must be listed
in the CI acceptance manifest when implemented):

- `TestExitTableMatchesContract`: the Go table equals the contract file entry by
  entry (code, name, category); any difference fails.
- `TestExitTableCodesUniqueAndInRange`: codes are unique, 0 or within 64-74, and
  reserved codes 75-79 are unassigned.
- `TestEveryExitPathMapsToCode`: each failure category, including missing and
  incompatible session interface and unattended refusals, maps to its code (SC-012).
- `TestRemoteStatusPassthrough0_1_255`: `shell`, `console`, `ssh` and
  `agent attach` return remote statuses 0, 1 and 255 unchanged, and 72 on
  transport loss.
- `TestCLIFailureAnnouncedOnStderrAndKindCLI`: CLI failures print `whr:` and mark
  `kind: cli`; remote statuses mark `kind: remote` and print no `whr:` line.

A change to the contract file or these tests needs a new ADR that supersedes this
table, not a silent edit.

## Owner confirmation requested

The 64-79 range, the 74 internal catch-all and the accepted passthrough overlap.

## Consequences

Release documentation publishes this table (FR-018). Nothing is implemented and no
check has run for this ADR; the tests above are a plan only.
