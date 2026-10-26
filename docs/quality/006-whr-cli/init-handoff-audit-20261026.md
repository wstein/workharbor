# Init source audit and remaining handoff evidence

Audited main: `612e38f4e30b676d20cc62405ea4703ee8e63a44` (2026-10-26).
This is an evidence index and gap list, not T030 or T034 acceptance. No task or
progress checkboxes change. The owner-approved next step is usable init, local
stage and promote; no control-plane work is added.

## Delivered entry points

`apps/whr/internal/commands/entries.go` registers `workspace.init`, the `init`
short form and the `ws` group alias. `build.go` registers typed flags through
`addInitFlags`; `init_options.go` preserves absence versus explicit values.
`init.go` discovers the source, selects the wizard for a terminal invocation,
and applies the same scaffold proposal used by flags. The existing manifest,
per-path overwrite consent and selected devcontainer are handled in scaffold.

The audited source has init/wizard/devcontainer-reuse implementations. This
audit identifies remaining acceptance evidence, rather than inventing missing
commands or changing settled selection/default behavior. Stage CLI is on main
through #271. Promote CLI remains in #272 until owner merge. Its branch now includes the
throwaway cleanup from #274; host acceptance requires that combined #272
implementation to be accepted and merged.

## T030 literal test index

The T030 entry through the start of T031, including its amendment notes,
contains 33 distinct literal test identifiers at this base. 32 have Go test
functions; the historical unavailable-devcontainer name is superseded by T033.
This mechanical count is not a claim that all SC-018 clauses or historical RED
phases are proved. Earlier records counted different task revisions.

| Literal identifier | Source definition or reconciliation |
| --- | --- |
| `TestInitProposalWizardFlagParity` | `apps/whr/internal/commands/init_options_test.go` |
| `TestInitCreatesExpectedManifest` | `apps/whr/internal/scaffold/proposal_test.go` |
| `TestInitOutputValidates` | `apps/whr/internal/scaffold/proposal_test.go` |
| `TestInitPreservesExistingSettings` | `apps/whr/internal/scaffold/options_test.go` |
| `TestInitRerunIsNoOp` | `apps/whr/internal/scaffold/preview_test.go` |
| `TestInitNeverOverwritesWithoutOverwriteFlag` | `apps/whr/internal/scaffold/init_acceptance_consent_test.go` |
| `TestInitOverwriteRequiresYesAndPath` | `apps/whr/internal/scaffold/init_acceptance_consent_test.go` |
| `TestInitFileChangedSincePreview` | `apps/whr/internal/scaffold/apply_test.go` |
| `TestInitSymlinkTarget` | `apps/whr/internal/scaffold/init_acceptance_paths_test.go` |
| `TestInitExistingDirectoriesReused` | `apps/whr/internal/scaffold/init_acceptance_paths_test.go` |
| `TestInitDryRunWritesAndRunsNothing` | `apps/whr/internal/scaffold/discover_test.go` |
| `TestInitFailureInjectionRollback` | `apps/whr/internal/scaffold/apply_test.go` |
| `TestInitRollbackIncompleteExits73` | `apps/whr/internal/scaffold/apply_test.go` |
| `TestInitMissingInputNeedsHuman` | `apps/whr/internal/scaffold/proposal_test.go` |
| `TestInitProjectURLRules` | `apps/whr/internal/scaffold/options_test.go` |
| `TestInitNoGitNoNetwork` | `apps/whr/internal/commands/init_network_linux_test.go` |
| `TestInitTemplateRefused` | `apps/whr/internal/scaffold/proposal_test.go` |
| `TestInitModes` | `apps/whr/internal/commands/init_options_test.go` |
| `TestInitJSONMatchesSchema` | `apps/whr/internal/scaffold/preview_test.go` |
| `TestInitExitCodes` | `apps/whr/internal/commands/init_acceptance_results_test.go` |
| `TestInitProjectDeclaredNotPinnedWarning` | `apps/whr/internal/scaffold/init_acceptance_results_test.go` |
| `TestInitExistingManifestPreservedWithoutRequestedEdit` | `apps/whr/internal/scaffold/init_acceptance_consent_test.go` |
| `TestInitManifestEditsNeedOverwrite` | `apps/whr/internal/scaffold/init_acceptance_consent_test.go` |
| `TestInitVersion2NotCreationReadyWarning` | `apps/whr/internal/scaffold/init_acceptance_results_test.go` |
| `TestInitNonInteractiveWithoutYes` | `apps/whr/internal/scaffold/init_acceptance_consent_test.go` |
| `TestInitDevcontainerFlagNotInRelease` | Historical unavailable assertion; superseded by accepted T033. See `TestInitDevcontainerSelectionValidated`. |
| `TestInitIdenticalFileSkipExisting` | `apps/whr/internal/scaffold/init_acceptance_paths_test.go` |
| `TestInitGitkeepUnderProjectsPath` | `apps/whr/internal/scaffold/gitkeep_test.go` |
| `TestInitBundlesLeftUnchanged` | `apps/whr/internal/scaffold/init_acceptance_paths_test.go` |
| `TestInitOverwriteTempFilePrefixDetected` | `apps/whr/internal/scaffold/init_acceptance_paths_test.go` |
| `TestInitExitCodeExistingFileVsReplaced` | `apps/whr/internal/commands/init_acceptance_results_test.go` |
| `TestInitExistingDevcontainerPathPreserved` | `apps/whr/internal/scaffold/discover_test.go` |
| `TestInitExistingShellSettingsPreserved` | `apps/whr/internal/scaffold/discover_test.go` |

## SC-018 clause mapping and open evidence

| Clause | Existing source evidence | Still required |
| --- | --- | --- |
| Complete result or original tree after writer failure | `scaffold/apply_test.go`; `t030-init-core-20261024.md` | Consolidate every writer step against all three allowed outcomes, without inferring coverage from names. |
| SIGINT/SIGTERM and overwrite cancellation | `t030-overwrite-cancellation-20261024.md`; signal tests in scaffold | Complete the per-step signal/failure matrix and identify any untested step. |
| Full disk | `t030-physical-enospc-20261024.md`; `scaffold/enospc_linux_test.go` | Keep native Linux execution distinct from macOS/other filesystems; no universal proof claimed. |
| Appearing/replaced paths, overwrite symlinks, preserved bytes | `t030-path-acceptance-20261024.md`; `t030-custom-path-refusals-20261024.md`; `scaffold/init_acceptance_paths_test.go` | Carry actual refusal reasons and outside-tree preservation into the final matrix. |
| No Git/network/credentials; dry-run/no-op | `t030-network-isolation-20261024.md`; scaffold discovery/preview tests | Whole-increment built-binary offline evidence remains separate. |
| Wizard/flag byte parity and reused devcontainer preservation | `commands/init_options_test.go`; `wizard/wizard_test.go`; `t033-devcontainer-reuse-20261024.md` | Final exact-head native PTY and equivalent-input file comparison. |
| Partial failure lists remaining paths | `t030-result-acceptance-20261024.md`; scaffold rollback tests | Consolidate each partial failure with the resulting paths and exit 73. |

## Remaining work and ownership

| Item | Who / next unit |
| --- | --- |
| SC-018 consolidated per-step matrix and any missing assertions | Codex: one focused test PR if the matrix exposes a missing behavior; no new safety layer or changed init semantics. |
| SC-016/SC-020 installed-binary validate/doctor/init/validate and offline run | Owner-approved binary execution scope is required; current Go-test re-execution is not a distribution-binary transcript. Codex can prepare and consolidate results. |
| SC-017 device nodes, full hostile corpus and real-host bounds | Owner host evidence; record all filesystem metadata, markers, network observations and resource measurements. |
| SC-019 Linux Docker and macOS+OrbStack doctor | Owner runs both target hosts and records exact versions/statuses; hosted macOS tests are not OrbStack evidence. |
| Real bash/zsh/fish completion | Owner installed-binary transcripts; existing real-shell adapter tests use a Go-test wrapper. |
| Local stage/promote disposal acceptance | Owner follows the [host runbook](owner-init-stage-promote-runbook.md) after #272 is accepted and merged. |

T034 stays open. Feature 010 G-A/G-B remain independent owner gates. Builds,
release tags, publication and installation are separate owner actions.
