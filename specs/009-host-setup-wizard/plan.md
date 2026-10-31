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

Endpoint selection (owner decisions D-12 to D-18, FR-029 to FR-032) adds one more library part, a package (working name `endpoint/`) that `doctor`, `localdocker` and `hostsetup` can all import without a cycle (final path chosen in T020): the selection function (option, config value, `DOCKER_HOST`, standard socket), the read-only investigation with the Docker context metadata reader, and the drift reports. It is built first as a library (Stage 1) with fake sockets and fake context files, and `localdocker` is switched by Feature 010 T032 and `doctor` only in the owner-gated contract PR (T028 (a)), because `doctor.CandidateOrder()` is pinned to `contracts/cli/doctor-checks.json` by `TestDoctorChecksMatchContract`. The wizard question is Stage 2 (T026). The catalog gains `colima` and `docker` (T017, OG-7); the host profile gains the record `container_endpoint` and the rule that never-selected entries leave no record (T019).

## Dependencies on other features

| Item | Use | Rule |
| --- | --- | --- |
| 006/T025 | exit table, envelope | prerequisite of command-facing tasks |
| 006/T027 | local doctor check runner | `fix/` extends it through its port; its catalogue is edited only by gated T011 |
| 006/T031, T032 | terminal harness, wizard framework | Stage 2 prerequisites |
| 006/T015 | Linux enrollment | untouched; only routing is tested (T008) |
| 008/T005 | git location classes, host-only keys | prerequisite of T005; if absent, stop and ask |
| 006/T027 (reopened) | `doctor/socket.go` socket rules | T020 builds the selection function beside it; the `doctor` switch and `doctor-checks.json` change are one gated PR (T028 (a)); `localdocker` is switched by Feature 010 T032 |
| 006 FR-060, FR-066, `contracts/cli/*` | `container.socket` text, flag lists | text and contract changes only through T027, T028 (OG-6, OG-4, OG-5); none in Stage 1 |
| 010 (merged P1 to P4) | `localdocker/endpoint.go` is a consumer of the socket rules | T032 follows 009 T020 and merges together with the docs parts T027 (a) and T029 (a); it precedes T028 (a); 010 T033 follows T028 (b); nothing in 009 depends on 010 code |
| 011 | consumes FR-029, FR-033, FR-034, `container_endpoint`, the 18-entry catalog | 011 starts after T020, T017, T019 are accepted (see Cross-feature ordering) |
| 012 | consumes the selected endpoint (Terms) | after 010 and 011 |
| BundleDock | none | the host catalogue has no BundleDock carrier or consumer; no task edits BundleDock |

## Cross-feature ordering (010, 009, 011, 012)

Recommended order: Feature 010, then Feature 009, then Feature 011, then Feature 012. The reasons below were checked against the specs and the merged tree; where the evidence differs from a plain "010 first", the deviation is stated.

| Edge | Direction | Evidence | Kind |
| --- | --- | --- | --- |
| 010 packages P1 to P4 and Feature 009 | none: 010 needs nothing from 009 | 010 plan P1 depends only on 006/T025 and 006/T027; the only 009 mention in 010 is that the host catalog lists `herdr` (010 spec, facts) and an owner-installed plugin | no dependency; 010 can finish first |
| 009 library and 010 endpoint code | 010 follow-up needs 009 | 010 spec FR-002 and Dependencies: the endpoint selection of Feature 009 FR-029 replaces the FR-060 socket discovery; 009 FR-029 names `ls --local` and `shell --local` as consumers; merged P1 pins the old order in `apps/whr/internal/localdocker/endpoint_test.go` (`TestLocalEndpointUsesDoctorCandidateOrder`) and `apps/whr/internal/doctor/checks_test.go` (`CandidateOrder` against the contract) | hard, for the follow-up only |
| 009 and 011 | 011 needs 009 | 011 FR-039 and FR-041 use the selected endpoint (009 FR-029) and the record `container_endpoint`; FR-040 and SC-035 count the catalog as 18 with 009 FR-026 and FR-033 and rely on 009 FR-034 (`unselected`); 011 appends a code to 009 `refusal-codes.json`, adds a readiness summary to `whr setup` and an exception to 009 FR-016 | hard |
| 010 and 011 | none | 011 spec has no reference to Feature 010 | no dependency; 011 may run in parallel with 010 |
| 011 and 012 | 012 needs 011 | 012 Related and FR-004: capability `interactive_shell`, 011 FR-016 and the code `refused.interactive_unavailable`; 012 FR-003 keeps `interactive_shell=none` until its conformance passes | soft: 012 FR-004 says that if 011 has not landed, 012 appends the code itself |
| 010 and 012 | 012 needs 010 | 012 step 5 and FR-008 cite 010 FR-002, FR-004, FR-009 for runtime access and the exec plan | hard on the merged 010 behavior |
| 009 and 012 | 012 needs 009 | 012 says "discovered `unix://` endpoint"; the 009 Terms define that as the selected endpoint (FR-029) | hard, through the selection function |

Deviation from the plain order: Feature 010's merged packages P1 to P4 and its owner gates (G-A, G-B) do not wait for 009, but 010's endpoint follow-up (Feature 010 T032 to T034) cannot start before 009 T020 (selection function); its `ls --local`/`shell --local` option (T033) waits for 009 T028 (b). The workable sequence is therefore: 010 P1 to P4 and gates; 009 Stage 1 (T016 to T025); 010 T032 together with T027 (a) and T029 (a) (needs 009 T020); then 009 T028 (a), T028 (b), 010 T033 and T034; 009 Stage 2 (T026, T030 to T032, T031); then T027 (b), (c) and T029 (b) together with or after T031 (they need T028, T031 and 010 T033). Two further observations: 011 does not depend on 010 (it may overlap with it), and 012 depends on 011 only softly. Not all of 011 waits for 009: in the draft Feature 011 tasks (PR #298, head 8285869) the amendment proposals, skip-record, no-Windows, platform-matrix and vendor-name items (T001 to T011) start after approval with no provider and no 009 code; its Stage 3 library uses no 009 code either but starts only when the named Feature 008 tasks are on `main` and its T002 is merged; its Stage 4 needs 009 T001 to T003, T006, T007, T010 and 006/T027, takes the endpoint for `container.image`/`container.leftovers` from the 009 T020 selection function, reads the Colima storage through the 009 T023 library for `disk.container_storage`, reopens 009 T001 for the catalog loader, and its registration step needs 009 T011 approved. Feature 012 has no plan or tasks on `main`; PR #297 (ready for review, not merged) proposes them. Also, the header of the 012 spec still calls the 011 decisions "unmerged draft PR #290"; the 011 text is on `main`, so that wording is stale (not edited here).

## Questions for the owner (not decided by this plan)

Docs-versus-code windows: merged documents must never describe behavior that no command implements yet, so the user-facing text is split. T027 (a) and T029 (a) (only the default order for `ls --local` and `shell --local`) merge together with 010 T032; T027 (b), (c) and T029 (b) (option, config value, investigation, drift, Colima) merge together with or after T031 and 010 T033. Remaining mismatches: (1) from 010 T032 until 009 T028 (a), `doctor` still searches every candidate while `ls`/`shell --local` use the new default order (one gated PR sequence; the docs of (a) do not mention `doctor`); (2) from T028 (a) until T031 and 010 T033, doctor and the contract implement the selection rule while FR-060 text and `container-runtime.md` still describe the old one (documents lag, they do not run ahead; the gated T028 (a) PR and T031 should follow each other directly). Red-first tests are unaffected (documents only), and the graph stays acyclic: no code task depends on T027 or T029.

The position of the wizard endpoint question is not a question: spec 009 says "the question after detection" and `contracts/wizard-flow.md` places it inside step 1, right after detection ends and before the plan table.

1. Where does the selection function live (a neutral package imported by `doctor`, `localdocker` and `hostsetup`)? T020 proposes a package of its own; the final path is an implementation choice that the spec does not settle.
2. `contracts/host-tool-catalogue.schema.json`: do `x-initial-items` and `x-initial-defaults.selected_by_default_false` gain `colima` and `docker`, or is a separate added-items list kept so that "initial" keeps its meaning? (spec offers both; T017 needs one.)
3. Should SC-009 checkpoint A (Feature 010 T018, G-A) be run before or after the endpoint follow-up of Feature 010 T032 to T033? On the owner's OrbStack Mac the old and new default give the same result, but a record made before the follow-up does not cover `--container-endpoint`.
4. Must Feature 010 T031 (whole-feature reconciliation) wait for the new follow-up tasks? This plan leaves T031 unchanged and records the follow-up as an addition.
5. Are the two docs-versus-code windows above acceptable? Recommendation: accept both, because they close with one gated PR sequence each (T028 (a) directly after 010 T032, and T031 with 010 T033 directly after T028 (b)), and the alternative (docs first) would describe unimplemented behavior for longer.

## Stages

Stage 1 (T001 to T009): offline library, contracts and the amendment proposal text; no command registered, no frozen contract changed.
Stage 2 (T010 to T014): wizard prompt flow; owner-gated command wiring, doctor catalogue and output schema (T011); owner-gated docs-only
amendment PRs (T012 to T014) on which T011 depends. T015: acceptance. Real runs on the owner's Mac are owner gates, not tasks.

Endpoint additions (T016 to T032): Stage 1 library first, no command registered: T016 (owner measurements, OG-1), T017 (catalog entries, OG-7), T018 (plan and exec behavior for the two entries), T019 (profile record), T020 (selection function), T021 (investigation and context reader), T022 (drift), T023 (Colima storage location), T024 (`fix/` additions), T025 (exit and flag additions). Stage 2: T026 (wizard question and recording), owner-gated amendments T027 (Feature 006 text, OG-6), T028 (contracts and `doctor` switch, OG-4/OG-5/OG-6), T029 (`container-runtime.md`, OG-5), T030 (Feature 008 wording, OG-8), registration T031, acceptance T032. Each PR is at
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
| FR-026, FR-033, FR-010 (added names), SC-015 (catalog half), SC-022 (catalog half); CC-1 | T017 (T001, T016; OG-7) | `TestInitialCatalogueEntries` (reopened, 15 ids), `TestColimaEntryShape`, `TestDockerEntryShape`, `TestAddedEntriesNeedNoSchemaChange`, `TestNoRequiresRelationAdded` | `.../catalogue` |
| FR-026, FR-027, FR-033 (no automatic install), SC-015 (plan half), SC-016 (boundary half); CC-2, CC-3, CC-4 | T018 (T017, T002, T003) | `TestYesPlansNoColimaOrDockerStep`, `TestColimaSelectedPlansOneBrewInstallAndRedetect`, `TestBrewArgvForAddedEntriesNoIgnoreDependenciesTapOrOtherFormula`, `TestColimaDetectionStates`, `TestNoColimaLifecycleOrContextCall`, `TestSelectingColimaSelectsNoDocker` | `.../plan`, `.../pkgmgr` |
| FR-030 (record), FR-034 (profile half), FR-005; SC-023 (profile half); CC-22 (profile half) | T019 (T006) | `TestProfileContainerEndpointRoundTrip`, `TestProfileEndpointRecordIsOnlyPathValue`, `TestProfileStoresNothingForUnselectedEntry`, `TestDeclineNotPreselectedRecordsNothing`, `TestReservedContainerEndpointItemAccepted` | `.../profile` |
| FR-029, FR-030 (grammar), FR-032 (selection); SC-018, SC-020 (selection half); CC-6 and CC-7 (replaced by CC-12 and CC-19), CC-11 to CC-14, CC-17, CC-23 | T020 (T019, 006/T025, 006/T027) | `TestSelectionOrderOptionConfigDockerHostStandard`, `TestSelectionFirstSpecifiedSourceIsOnlyOne`, `TestSelectionSpecifiedButUnreachableIsNeverReplaced`, `TestSelectionConfigStaysAuthoritative`, `TestSelectionOptionValueGrammar`, `TestSelectionStartsNoProcessReadsNoContextListsNoDirectory` | the endpoint package (T020) |
| FR-031 (read-only part), FR-032 (investigation evidence), EP-4, EP-6; SC-019 (investigation half); CC-15 (library half), CC-20 (investigation half), CC-24 | T021 (T020; T016) | `TestInvestigationStartsOnlyWhenNothingIsThere`, `TestInvestigationDialsOnlyFixedCandidatesInOrder`, `TestInvestigationOmitsSecondColimaSocket`, `TestContextReaderReadsOnlyActiveNameAndEndpoints`, `TestRemoteContextListedNotSupportedNeverOffered`, `TestPlainDoctorInvestigationListsInEvidenceAndNeverAsks` | the endpoint package |
| FR-032 (drift), EP-5, EP-9; SC-021 (reporting half); CC-18 (library half) | T022 (T021, T019) | `TestDriftStoredContextDeleted`, `TestDriftStoredContextPointsElsewhere`, `TestDriftActiveContextEndpointDiffers`, `TestDriftDockerHostDiffersFromStoredEndpoint`, `TestDriftIsWarningOnlyNeverStatusOrSwitch`, `TestDriftUnreachableStoredEndpointIsErrorNotDrift` | the endpoint package |
| FR-028; SC-017; CC-8, CC-9, CC-19, CC-20 (endpoint half) | T023 (T020; T016) | `TestColimaProfileFromEndpointDirectory`, `TestColimaYamlReadsOnlyDiskKeysBounded`, `TestColimaHostVolumeFreeSpaceViaPort`, `TestColimaStorageFallbackToRecordedValueThenUnknown`, `TestNonColimaEndpointReadsNoColimaFile` | the endpoint package |
| FR-016 (amended), FR-032 (re-record), FR-033 (message), FR-034 (skip); SC-021 (re-record half), SC-022 (message half), SC-023 (doctor half); CC-18 (fix half), CC-21, CC-22, CC-24 (plain doctor half) | T024 (T007, T017, T019, T021, T022) | `TestFixEndpointQuestionOnlyOnTerminalAndNeedsProfile`, `TestFixNeverPromptsExceptEndpointQuestion`, `TestFixRerecordEndpointRequiresConfirmContainerEndpoint`, `TestFixYesAloneNeverRerecordsEndpoint`, `TestUnselectedEntrySkippedWithReasonUnselected`, `TestFixNeverInstallsUnselectedEntry`, `TestDockerClientMessageNamesEntryAndInstallsNothing` | `.../fix` |
| FR-017, FR-020, FR-030, FR-031 (exits); SC-009 (extended), SC-019 (exit half), SC-020 (64 half); CC-16 (exit half) | T025 (T008, T020, T021) | `TestReservedContainerEndpointIDAccepted`, `TestContainerEndpointFlagOnLinuxSetupIsUsageError`, `TestUnattendedEndpointSkipIsNotExit70`, `TestEndpointQuestionEndOfInputOrInterruptExits72`, `TestMalformedEndpointOptionExits64`, `TestResultRedactionCoversEndpointRecordAndContexts` | `.../plan`, `.../route_test.go` |
| FR-027 (report), FR-031 (question), FR-034, FR-041 of Feature 011 (wizard side); SC-019 (question half), SC-023 (wizard half); CC-10, CC-15, CC-16, CC-21, CC-22 | T026 (T010, T018, T019, T021, T023, T025) | `TestWizardEndpointQuestionAfterDetectionOnStderrOncePerRun`, `TestWizardEndpointAnswerIsOnePreviewedProfileWrite`, `TestWizardEndpointSkipRecordsNothingAndNextRunAsks`, `TestWizardEndpointOptionAnswersQuestion`, `TestWizardEndpointUnattendedDefaultSkipOneStderrLineSetupContinues`, `TestWizardEndpointEndOfInputExits72`, `TestWizardOffersUnselectedEntriesUncheckedEachRun`, `TestWizardRecordsColimaStorageOnlyWhenYamlExists` | `.../wizard` |
| FR-029, FR-030, FR-032 as contract and doctor switch; SC-016 (second sentence), SC-018, SC-020; CC-5 (doctor half), CC-17 (consumers) | T028 (T020 to T022, T027, 010 T032 for (a); OG-4/5/6) | amended `TestDoctorChecksMatchContract`, `TestCommandTableMatchesContract`, `TestLocalShellContractAmendment`, `TestLocalLsContractAmendment` red against the old contract; `TestContainerSocketPassesWithOnlySelectedColimaEndpoint` (CC-5); consumers in Feature 010 T032, T033 | contracts, `apps/whr/internal/doctor`, `.../commands` (T028); the `localdocker` and `ls`/`shell --local` consumers are Feature 010 T032 and T033 |
| Registration of the endpoint surface; SC-019, SC-021 | T031 (T011, T024, T025, T026, T028) | `TestSetupContainerEndpointFlagAndExitCodes`, `TestSetupEndpointDryRunCreatesNothing`, `TestDoctorEndpointEvidenceAndWarnings`, `TestDoctorFixEndpointQuestionAndConfirm` | `apps/whr/internal/commands` |
| Document amendments (Feature 006 text, `container-runtime.md`, Feature 008 wording) | T027, T029, T030 | `python3 scripts/check_repo.py`, `git diff --check`, review CLEAR | amendment PRs |
| Real-host facts (versions, context layout, Colima files) | T016 (OG-1) | owner-run record | `docs/quality/009-host-setup-wizard/og1-endpoint-measurements.md` |
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

### Endpoint, Colima and Docker client decisions (owner decisions of 2026-10-31; all confirmed in the spec)

These are recorded in spec.md as D-10 to D-18 and EP-1 to EP-9. They are fixed inputs: no task reopens them, and a task difficulty is a question to the owner, not a new default. The table gives the consumers.

| ID | Decision (summary; spec text is authoritative) | Affects |
| --- | --- | --- |
| D-10 | Colima is a catalog entry next to OrbStack, not preselected; Lima only as a Homebrew dependency; `whr` never runs Colima lifecycle commands | FR-026, FR-027; T017, T018 |
| D-11 | The container storage location is read from the runtime's configuration file first, then the wizard's record, else `unknown`; no runtime API | FR-028; T023, T026; Feature 011 FR-041 |
| D-12 | Default: `DOCKER_HOST`, then `/var/run/docker.sock`; an option or a config value beats the default; the configured value stays authoritative; when nothing is there `setup` and `doctor` investigate and ask | FR-029 to FR-031; T020, T021, T026 |
| D-13 | Contexts are read for detection and drift only, never for selection; the profile stores the endpoint (authoritative) and the context name (informational); drift is a warning; remote targets unsupported | FR-030 to FR-032; T019, T021, T022 |
| D-14 | Separate `docker` client entry, no `requires`; a never-selected entry leaves no record, is offered unchecked each run and is `skipped` with a reason | FR-033, FR-034; T017, T019, T024, T026 |
| D-15 | Only `setup` and `doctor --fix` ask, on a terminal; start condition is "nothing is there"; second Colima socket not listed; Linux gets message text only; an unreachable stored endpoint is an error | FR-016, FR-031, FR-032; T020, T021, T024 |
| D-16 | `skip` records nothing and the question repeats; unattended applies `skip` without exit 70; order option, config value, `DOCKER_HOST`, standard socket; `DOCKER_HOST` drift warns | FR-029, FR-031, FR-032; T020, T022, T025, T026 |
| D-17 | Plain `doctor` runs the investigation read-only, lists options in the `container.socket` evidence, never asks | FR-031, FR-032; T021, T024 |
| D-18 | Names (`--container-endpoint`, `container_endpoint` with `socket` and `context`, reserved id `container-endpoint`), ordinary commands, terminal mechanics, drift reporting, candidate list, Docker client entry details, skip reason `unselected` | EP-2, EP-3, EP-4 (mechanics), EP-5 to EP-8; T017, T019, T020, T021, T024, T025 |

Mapping of the confirmed details: EP-1 to T020; EP-2 to T019, T020, T025; EP-3 to T020 and, for the callers, to T028 (a) (doctor) and Feature 010 T032, T033; EP-4 to T021, T025, T026; EP-5 to T022; EP-6 to T021; EP-7 to T017; EP-8 to T024; EP-9 to T022. No new exit code, refusal code, step kind or check status is introduced (EP-3, EP-5); `host_setup.unknown_item` only changes its text to name the reserved id.

OG-1 results M-1 to M-6 (spec) are the only measured facts. Still unmeasured, and therefore fixtures with a stop-and-report rule rather than defaults: the `colima` and `docker` version arguments and minima, the Docker context file layout, the Colima files before the first start, `COLIMA_HOME`, non-default profile sockets, a stopped Colima, and the `docker` formula behavior. They are collected in T016.

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

### Endpoint amendment set (external changes, not edited here)

The spec section "Endpoint selection (D-12, D-13) and catalog additions (D-14)" lists the changes to existing documents. This feature plan only schedules them; none is applied by this plan. Spec text of Features 009, 010 and 011 already carries the decisions.

| Text affected | Task | Gate |
| --- | --- | --- |
| Feature 006 FR-066 `ls --local` sentence (T027 (a), with 010 T032); FR-060 `container.socket`, the 5 s budget sentence (T027 (b)); command surface sentence (T027 (c)) | T027 | OG-6 |
| `contracts/cli/doctor-checks.json` (`socket_candidates`, `container.socket`, `disk.container_storage`), `doctor/socket.go` and the doctor tests | T028 (a) | OG-4, OG-5 |
| `apps/whr/internal/localdocker/endpoint.go` and its tests (`TestLocalEndpointUsesDoctorCandidateOrder` is replaced) | Feature 010 T032 (only task that edits them; needs 009 T020, lands before T028 (a)) | none |
| `contracts/cli/commands.json`, `entries.go`, freeze tests (flag lists of `host.setup`, `host.doctor`, `workspace.ls`, `workspace.shell`) | T028 (b) | OG-4, OG-5 |
| `docs/container-runtime.md`, `docs/design/local-devcontainer-lifecycle.md` line 62 check | T029 ((a) with 010 T032, (b) with or after T031) | OG-5 |
| Feature 008 wording (Docker VM example gains Colima) | T030 | OG-8 |
| This feature's own `contracts/wizard-flow.md`, `contracts/refusal-codes.json` text, `data-model.md`, `quickstart.md` | this spec-only PR | none (not frozen) |
| `contracts/host-tool-catalogue.schema.json` (`x-initial-items`, `x-initial-defaults`), `catalogue.json`, `production_test.go` | T017 | OG-7 |
| Feature 010 `research.md`, `contracts/local-cli.md`, `data-model.md`, `docs/quality/010-host-and-workspace-panes/p1.md` | Feature 010 T034 (a note pointing to the amendment is added by this PR) | none |
| Feature 011 wording; Feature 012 wording ("discovered" means selected endpoint) | Feature 011 and 012 own tasks; 012 has no plan or tasks on `main` (PR #297, ready for review, adds them) | their own gates |
| `checklists/requirements.md`, `analysis.md` | T032 | none |

## Owner gates

OG-1 any real run on the owner's Mac; OG-2 any host mutation outside a temporary HOME in tests; OG-3 artifact, image or documentation builds
and releases; OG-4 changes to `contracts/cli/*`, ADR 0013 or ADR 0014; OG-5 doctor catalogue, `setup` or `container-runtime.md` changes;
OG-6 any change to Feature 006 text; OG-7 adding or changing a host catalogue entry; OG-8 any change to Feature 008 text; OG-9 the first real run that uses
`sudo` or a privileged catalogue step. An approval covers the one operation named.

Endpoint additions: T016 is OG-1 (owner measurements); T017 is OG-7 (new catalog entries); T027 is OG-6; T028 is OG-4, OG-5 and OG-6; T029 is OG-5; T030 is OG-8; T031 registers commands and is the named SC-014 exception together with T011 to T014 and T027 to T030. None of T018 to T026 may use a real run (OG-1, OG-2).


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
