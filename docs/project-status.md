# Project status and frozen scope

Owner decision 2026-11-03, tracked in issue #330 ("feat(whr): local walking skeleton init -> create -> shell -> stop -> rm").
Current focus is the local walking skeleton in `apps/whr`. Everything below is frozen: kept in the repository, not
deleted, and not extended until the owner reopens it. Reopening: owner decides.

## Frozen

| Area | Scope |
| --- | --- |
| Apps | `apps/control_plane`, `apps/api_client`, `apps/host_agent` |
| Specs | [002](../specs/002-symphony-doc-smoke/spec.md), [003](../specs/003-single-task-entry/spec.md), [004](../specs/004-speckit-symphony-automation/spec.md), [005](../specs/005-provider-aware-orchestration/spec.md), [007](../specs/007-bundledock-shell-policy/spec.md), [009](../specs/009-host-setup-wizard/spec.md), [010](../specs/010-host-and-workspace-panes/spec.md), [011](../specs/011-vm-provider/spec.md), [012](../specs/012-container-interactive-shell/spec.md) |
| Spec 008 | Frozen except the profiles the walking skeleton needs |
| Commands | `stage`, `promote`, `capture` (part of [spec 006](../specs/006-whr-cli/spec.md)) |

## Decisions that apply to the skeleton

- Heavy CI runs only at the stack tip, triggered by the label `ci-full` (see [AGENTS.md](../AGENTS.md)). The workflow
  does not evaluate the label yet; changing it is an owner task.
- The `AGENTS.md` and `.claude` files written by `init` are opt-in. Implementation belongs to the skeleton work.
- The default workspace user is `vscode` (home `/home/vscode`). Documentation uses `vscode`. Code fallbacks to
  `captain` remain until the skeleton work changes them.
- `fish` is provided through mise.
