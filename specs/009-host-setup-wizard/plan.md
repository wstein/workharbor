# Implementation Plan: Host setup wizard and doctor repair

Branch: `docs/spec009-host-setup-wizard` (PR target `main`) | Date: 2026-10-20
Spec: [spec.md](spec.md)
Status: source work authorized by H37; DP-1 to DP-5 and no-op protocol bookkeeping confirmed by H38 (2026-10-20). The H35 loader/proposal slice has a separate scoped CLEAR. Full-feature acceptance, task completion, remaining AP application, production-catalogue integration and actual adapter/host acceptance remain separate. H40's selected minimum-version snapshot belongs to its catalogue increment; this canonical slice does not select floors or authorize standalone builds.

## Approach

Library-first and offline. Go packages under `apps/whr/internal/hostsetup/` (ADR 0010; module `apps/whr`): catalogue, detection and plan logic use the standard library only. The native Unix boundary uses the module's existing `golang.org/x/sys/unix` dependency for descriptor-relative, nonfollowing filesystem operations (no added module dependency).
`catalogue/`, `detect/` and `plan/` (detection, typed steps, hash, apply), `pkgmgr/` (port, Homebrew adapter, fakes for package manager, sudo and
network), `dotfiles/`, `gitmove/`, `profile/` (host profile, protocol, run lock), `fix/` (`doctor --fix` on the same engine), `wizard/` (Stage 2).
One engine serves `setup` and `doctor --fix`: detect, plan, apply; they differ only in how confirmations are obtained (prompt or flags). Real
`git` runs only on fixtures in a temporary HOME. Git move previews show structural keys/redacted paths and actions, never content/value diffs. No real `brew`, `sudo`, network or Docker in any test (FR-023).

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
| OD-3 | Dotfiles (starship.toml, a fish drop-in) need diff preview and backup; the git configuration move needs structural key/path-only preview and backup, never a value/content diff (H38). Nothing is silently overwritten; `--dry-run` previews; no merging into a user's file. | FR-013, FR-014; T004, T005 |
| OD-4 | On Linux `whr setup` keeps the Feature 006 enrollment: one name, platform-specific fixed step lists chosen by platform (AP-1 to AP-3). | FR-001; T008, T009, T011 |
| OD-5 | R-40 is superseded: setup may install a container runtime when the user selects it. The archived requirement is not edited; `docs/container-runtime.md` and Feature 006 FR-060 wording change by AP-7 and AP-4. | FR-003; T012 |

### Confirmed design decisions (H38, 2026-10-20)

| ID | Confirmed existing default | Reason |
| --- | --- | --- |
| DP-1 | Host profile `~/.config/whr/host-profile.json`; state directory `~/.local/state/whr/` with `protocols/`, `backups/<run-id>/` and `run.lock` | `doctor --fix` must know opt-outs |
| DP-2 | Step deadline 900 s, probe 5 s, output tail 64 KiB, SIGINT grace 30 s, warning above 500 protocols | testable bounds |
| DP-3 | Flag names and closed answer sets of [wizard-flow.md](contracts/wizard-flow.md) (`--yes`, `--confirm <id>`, `--only`, `--skip`) | consistent with Feature 006 FR-045 |
| DP-4 | Feature009 carrier confirmed: wizard step plus doctor check `git.global_config`; no separate `host git-config move` command | follows D-4; Feature008 AP-8 text applied in PR #164 |
| DP-5 | Feature008 follow-up text applied in PR #164 (not edited here): [plan.md](../008-workspace-sandbox/plan.md) resolved OD-5 and dependency rows, [spec.md](../008-workspace-sandbox/spec.md) Terms, Owner decisions and FR-026, [tasks.md](../008-workspace-sandbox/tasks.md) T010, [analysis.md](../008-workspace-sandbox/analysis.md) A-3, [checklists/requirements.md](../008-workspace-sandbox/checklists/requirements.md) OD-5 item, the `legacy_home_file` remedy of [git-host-only-keys.json](../008-workspace-sandbox/contracts/git-host-only-keys.json) | text applied by PR #164; remaining T014 acceptance stays separate |

### Amendment application status (external changes, not edited here)

The table records the amendment scope and its external application. Feature006
AP-1 to AP-5 and the FR-063 portion of AP-9 were applied in
`1071dd6198854933f124fad4c5ec8d1a02c8fdf9`; AP-7 was applied in
`7b146ef28ea038eb2ad1d58d67f013a7f75cccd5`. Feature008 AP-8/OD-5 text was
applied by owner-approved PR #164 (`9d297da28887ceee35ed6e5669647f07ffb4f220`).
These are text-application evidence, not completion of T012/T014 acceptance or
command registration. AP-6 reason/result placement remains unselected; its CLI
contracts and the T013 ADR application (including AP-9's ADR portion) remain
separate gates. T011 retains its approved/merged prerequisites.

| ID | Text affected | Amendment scope |
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


## Source execution and decision bindings

H37 (2026-10-20) authorizes manual source execution with a private per-work-item
path/argv/evidence record, TDD and independent exact-head review. The strict
single-task v1 parser still rejects this feature's cross-feature prerequisites
and range; it is unchanged. Never fabricate a context/journal or drop external
prerequisites to satisfy that parser. Source dependents may use exact locally
available CLEAR prerequisite heads before owner merge under H37, recording their
SHAs, content identities and relevant passing checks. This does not satisfy an
open real-host acceptance criterion or authorize Feature008 implementation.

| Decision | Status | Consumers / remaining application gates |
| --- | --- | --- |
| DP-1: profile/state paths | Confirmed H38 | T006 profile/protocol/lock; T007 profile-based repairs; no real HOME write authority |
| DP-2: 900s step, 5s probe, 64KiB tail, 30s SIGINT grace, warning above500 protocols | Confirmed H38 | T002 probe budget; T003 process bounds; T006 protocol warning; fake clocks/process ports only |
| DP-3: existing wizard-flow flags and answer sets | Confirmed H38 | T007, T008, T010, T011; current frozen CLI contracts change only through T011 |
| DP-4: setup guided move plus doctor git check | Confirmed H38 | T005, T007, T010; registration T011 after approved/merged T014 |
| DP-5: Feature008 carrier follow-up | Confirmed H38 | AP-8 text applied by PR #164; remaining T014 acceptance and T011 registration gates retained; no Feature008 source implementation here |
| Confirmed no-op protocols | Confirmed H38 | T006/T015 compare only managed installations/config/profile/backups; protocol/lock bookkeeping still occurs |
| AP-6 reason/result placement | UNSELECTED | Hold only affected T008 JSON failure/result schema, T011 registration/schema and related T013 text; independent exit/platform and other source work may proceed with scoped evidence |

Protocol record-shape controls are test-only in
`apps/whr/internal/hostsetup/profile/protocol_schema_test.go`, over the six positive
and explicit negative records in `contracts/protocol.fixtures.json` (relative to
this feature). This is not a protocol writer, sequence verifier, secret audit or
T006 completion. Those remain engine acceptance. F7 exit tests must distinguish
applied+opted_out/applied+declined success from applied+blocked-dependent failure.

The T002 native boundary is explicit: opening a trusted root creates read-only FS/Exec/Clock/Metadata ports and a detector; it does not register a command or run probes automatically. Each directory and executable is opened without following links. Generic version probes execute the opened descriptor (`/dev/fd/3`) on Linux to pin the resolved file across pathname replacement; macOS cannot execute `/dev/fd/N`, so on Darwin they execute the absolute path under the trusted root after an identity check (device and inode of the opened descriptor against `lstat` of the path, immediately before exec; a mismatch is refused). On Darwin, EPERM from signalling the process group of an exited, not yet reaped leader is not a failure; every other signal error, and EPERM for a live child, still fails the probe. The closed OrbStack metadata route instead executes the fixed absolute `/usr/bin/plutil` helper after an identity check, trusting its OS location; only its fixed plist input is descriptor-pinned. That helper boundary does not guarantee execution of an opened descriptor across arbitrary pathname replacement. Version children receive only the fixed PATH and `LC_ALL=C`, closed stdin, bounded stdout and discarded stderr. Cancellation and pipe cleanup terminate the owned process group. Curated version arguments remain a trusted catalogue boundary; these ports are not a sandbox proving arbitrary executables cannot access the network. Linux source tests use owned temporary files and copied test helpers only. Darwin test-compiler checks establish type compatibility, and the macOS CI job executes the owned-fixture native tests; real-host measurements and the fixed `/usr/bin/plutil` helper on a real OrbStack install remain a separate owner-authorized acceptance step.
