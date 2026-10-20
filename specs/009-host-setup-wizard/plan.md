# Implementation Plan: Host setup wizard and doctor repair

Branch: `docs/spec009-host-setup-wizard` (PR target `main`) | Date: 2026-10-20
Spec: [spec.md](spec.md)
Status: proposed for independent review and owner approval; no product code exists for this feature.

## Approach

Library-first and offline. Go packages under `apps/whr/internal/hostsetup/` (ADR 0010; module `apps/whr`), standard library only:
`catalogue/`, `detect/` and `plan/` (detection, typed steps, hash, apply), `pkgmgr/` (port, Homebrew adapter, fakes for package manager, sudo and
network), `dotfiles/`, `gitmove/`, `profile/` (host profile, protocol, run lock), `fix/` (`doctor --fix` on the same engine), `wizard/` (Stage 2).
One engine serves `setup` and `doctor --fix`: detect, plan, apply; they differ only in how confirmations are obtained (prompt or flags). Real
`git` runs only on fixtures in a temporary HOME. No real `brew`, `sudo`, network or Docker in any test (FR-023).

## Dependencies on other features

| Item | Use | Rule |
| --- | --- | --- |
| 006/T025 | exit table, envelope | prerequisite of command-facing tasks |
| 006/T027 | local doctor check runner | `fix/` extends it through its port; its catalogue is edited only by gated T011 |
| 006/T031, T032 | terminal harness, wizard framework | Stage 2 prerequisites |
| 006/T015 | Linux enrollment | untouched; only routing is tested (T008) |
| 008/T005 | git location classes, host-only keys | prerequisite of T005; if absent, stop and ask |
| BundleDock | none | the host catalogue has no BundleDock carrier or consumer; no task edits BundleDock |

## Stages

Stage 1 (T001 to T009): offline library, contracts and the amendment proposal text; no command registered, no frozen contract changed.
Stage 2 (T010 to T014): wizard prompt flow; owner-gated command wiring, doctor catalogue and output schema (T011); owner-gated docs-only
amendment PRs (T012 to T014) on which T011 depends. T015: acceptance. Real runs on the owner's Mac are owner gates, not tasks. Each PR is at
most about 1,500 changed lines; split a task into coherent halves when it would exceed that.

## Requirements to evidence

| Requirements | Task (prerequisites) | Acceptance tests (planned, red first) | Evidence location |
| --- | --- | --- | --- |
| FR-010, FR-022, SC-012 | T001 (006/T025) | `TestCatalogueLoadsAndValidates`, `TestCatalogueRejectsInvalid` | `apps/whr/internal/hostsetup/catalogue/` |
| FR-002, FR-004, FR-006, FR-007, SC-001, SC-006 | T002 (T001) | `TestDetectStates`, `TestDetectIsReadOnlyAndNeverRunsBrew`, `TestDetectCLTGitShimNotExecuted`, `TestPlanHashAndClasses` (includes no Docker VM step) | `.../detect`, `.../plan` |
| FR-003, FR-009, FR-011, FR-012, FR-017, FR-025, SC-004, SC-005 | T003 (T001, T002) | `TestBrewArgvAndEnvFixed`, `TestRootOrSudoStartRefused`, `TestPrivilegedStepShownConfirmedNeverUnattended`, `TestNoPasswordCanary`, `TestRuntimeOnlyWhenSelectedNeverStarted`, `TestNoLoginShellStartupFileTapOrTelemetry` | `.../pkgmgr` |
| FR-008, FR-013, SC-003 | T004 (T001) | `TestDotfileDiffBackupAtomic`, `TestExistingDifferentNeedsConfirmation`, `TestSymlinkTargetRefused`, `TestChangedSincePreviewRefused` | `.../dotfiles` |
| FR-014, SC-007 | T005 (T004, T006, 008/T005) | `TestMoveLegacyToXDG`, `TestMoveSplit`, `TestMoveRefusals`, `TestMoveRestoresOnFailure` | `.../gitmove` |
| FR-005, FR-018, FR-019, FR-021, SC-002, SC-008, SC-011 | T006 (T002, T003, T004) | `TestOptOutPersistsAndHonored`, `TestRerunIsNoOp`, `TestProtocolContentModesAppendOnly`, `TestRunLockExclusive`, `TestDryRunWritesNothing` | `.../profile`, `.../plan` |
| FR-015, FR-016, SC-010 | T007 (T006, 006/T027) | `TestFixSameEngineAsWizard`, `TestFixWithoutYesExit70`, `TestPlainDoctorReadOnly` | `.../fix` |
| FR-001, FR-017, FR-020, SC-009, SC-013 | T008 (T006, 006/T025) | `TestExitMappingMatchesADR0013`, `TestPrecedence73`, `TestResultRedaction`, `TestLinuxSetupRoutesToEnrollment` | `.../plan`, `.../route_test.go` |
| FR-004, FR-005, SC-002 | T010 (T005, T006, 006/T031, 006/T032) | `TestWizardYesCustomizeNo`, `TestWizardNoWritesNothing`, `TestWizardOptOutPerItem` | `.../wizard` |
| FR-024, SC-014 | T009, T015 | per-PR `git diff --stat` on frozen paths empty | PR description |
| Real-host behavior (brew, cask, OrbStack, git on macOS) | owner authorization (OG-1, OG-2, OG-9) | out of this increment | owner-run record |

## Owner decisions

Resolved 2026-10-20 (D-1 to D-8, [spec](spec.md#owner-decisions)). **OD-1 to OD-5 are confirmed 2026-10-20** (OD-2 amended).

| ID | Decision (confirmed 2026-10-20) | Affects |
| --- | --- | --- |
| OD-1 | Installation source: Homebrew only, plus the OrbStack cask. Every item listed up front (name, tap, minimum version), every item individually deselectable. Homebrew itself is a precondition, never installed by `whr`. | FR-009, FR-010; T003 |
| OD-2 (amended) | The host catalogue is a small curated list owned by WorkHarbor (13 initial entries listed in spec FR-010; entries added deliberately), each with a minimum version only. It is not a mirror of the BundleDock toolbox: no host install of toolbox tools, no version equality, no toolbox sync. `whr` embeds a snapshot per release. | FR-010, FR-022; T001 |
| OD-3 | Dotfiles (starship.toml, a fish drop-in, git configuration) are written only after diff preview and backup, never silently overwritten, `--dry-run` previews; no merging into a user's file. | FR-013, FR-014; T004, T005 |
| OD-4 | On Linux `whr setup` keeps the Feature 006 enrollment: one name, platform-specific fixed step lists chosen by platform (AP-1 to AP-3). | FR-001; T008, T009, T011 |
| OD-5 | R-40 is superseded: setup may install a container runtime when the user selects it. The archived requirement is not edited; `docs/container-runtime.md` and Feature 006 FR-060 wording change by AP-7 and AP-4. | FR-003; T012 |

### Author design proposals (confirm with the review)

| ID | Proposal | Reason |
| --- | --- | --- |
| DP-1 | Host profile `~/.config/whr/host-profile.json`; state directory `~/.local/state/whr/` with `protocols/`, `backups/<run-id>/` and `run.lock` | `doctor --fix` must know opt-outs |
| DP-2 | Step deadline 900 s, probe 5 s, output tail 64 KiB, SIGINT grace 30 s, warning at 500 protocols | testable bounds |
| DP-3 | Flag names and closed answer sets of [wizard-flow.md](contracts/wizard-flow.md) (`--yes`, `--confirm <id>`, `--only`, `--skip`) | consistent with Feature 006 FR-045 |
| DP-4 | Feature 008 OD-5 resolved here: wizard step plus doctor check `git.global_config`; no separate `host git-config move` command | follows D-4; needs a 008 amendment (AP-8) |
| DP-5 | Follow-up to merged Feature 008 (not edited here): [plan.md](../008-workspace-sandbox/plan.md) "Open: OD-5" and dependency rows, [spec.md](../008-workspace-sandbox/spec.md) Terms, Owner decisions and FR-026, [tasks.md](../008-workspace-sandbox/tasks.md) T010, [analysis.md](../008-workspace-sandbox/analysis.md) A-3, [checklists/requirements.md](../008-workspace-sandbox/checklists/requirements.md) OD-5 item, the `legacy_home_file` remedy of [git-host-only-keys.json](../008-workspace-sandbox/contracts/git-host-only-keys.json) | applied by T014 (OG-8) |

### Amendment proposals (not applied by this feature)

T009 writes them as one docs-only text; each is an owner decision applied by T012 to T014, on which T011 depends, so Feature 006 never contradicts the
shipped behavior.

| ID | Text affected | Proposed change |
| --- | --- | --- |
| AP-1 | Feature 006 FR-009 (and US3 `setup` stories) | Add: "On macOS `whr setup` is the host setup wizard of Feature 009: it consumes no enrollment code, installs no host agent service and has its own dry run. On Linux it is unchanged." |
| AP-2 | FR-026 | macOS is a host platform for `setup` (wizard) and `doctor --fix`, unverified until owner-run evidence; `service` and `offboard` still refuse macOS. |
| AP-3 | T015, T027 (`TestMutatingHostCommandsRefuseUnsupportedPlatform`) | T015 stays Linux-only; add a note that the macOS route is Feature 009; the refusal test treats macOS `setup` as supported. |
| AP-4 | FR-060 ("Doctor is read-only on every platform", "never installs or configures a container runtime"), FR-026, US8 scenario 7 ("created no file", "no network connection other than the local socket"), the doctor no-network success criterion (spec.md line 1388) and Assumptions, `doctor-checks.json` rules, T027 `TestDoctorReadOnlyNoNetwork`, `commands.json` `host.doctor` writes | Plain `doctor` is read-only for the host (no install, no write, no sudo, no protocol) and may use the network; `--fix` is the mutating real run. |
| AP-5 | FR-061 ("`setup --dry-run` is not included") | Unchanged for Linux; the macOS wizard has its own dry run. |
| AP-6 | `commands.json`, `doctor-checks.json`, `output.schema.json`, ADR 0014 section 3 and Consequences | `host.setup` note and `blocked_on`; `host.doctor` writes `none` or `host` with `--fix`; checks `host.profile`, `host.tool.<id>`, `host.dotfile.<id>`, `git.global_config`; result definitions; a place for the stable reason code (`result.reason`, or Feature 008's `error.refusal`). No new exit code. |
| AP-7 | `docs/container-runtime.md` | Replace "`whr` never installs ... a container runtime" by the macOS wizard statement; keep the Linux statement. |
| AP-8 | Feature 008 FR-026, OD-5, T010 and the DP-5 places | Record DP-4 and DP-5. |
| AP-9 | Feature 006 FR-063 (around spec.md line 1195) and ADR 0014 section 4 (lines 77 to 88) | Add the exits of macOS `setup` and `doctor --fix` (64, 68, 70, 71, 72, 73 per FR-020). No new code; ADR 0013 unchanged. |

## Owner gates

OG-1 any real run on the owner's Mac; OG-2 any host mutation outside a temporary HOME in tests; OG-3 artifact, image or documentation builds
and releases; OG-4 changes to `contracts/cli/*`, ADR 0013 or ADR 0014; OG-5 doctor catalogue, `setup` or `container-runtime.md` changes;
OG-6 any change to Feature 006 text; OG-7 adding or changing a host catalogue entry; OG-8 any change to Feature 008 text; OG-9 the first real run that uses
`sudo` or a privileged catalogue step. An approval covers the one operation named.
