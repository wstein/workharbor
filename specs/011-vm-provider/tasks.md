# Feature 011 work items

Status: proposed with [plan.md](plan.md); not approved or started. A task starts only after the spec, plan and this file are
approved at one exact head (WORKFLOW.md) and its prerequisites have implementation and test evidence. IDs `T001` to `T041` are
Feature 011's own; other features' items are written `006/T0nn`, `008/T0nn`, `009/T0nn`. Each task is one pull request of at
most about 1,500 changed lines (split a task into coherent halves when it would exceed that), targeting `main`. Write the named
failing tests first, show them failing, then implement. All tests are offline: no VM, hypervisor, network, registry, real
Docker, real `brew` or `sudo`, credential or host mutation (FR-026, SC-011); a fake driver, fake runners and a temporary HOME
stand in. Paths are in module `apps/whr` unless a path starts with `specs/`, `scripts/`, `docs/`, `contracts/` or `.github/`.
Test names are planned names. Exit codes: only the ADR 0013 table; every refusal of this feature is 68. No task names a VM
backend outside a driver location (FR-022): the first driver is `<first-driver>`.

Markers: `[P]` parallelizable with the other `[P]` tasks of the same stage once its prerequisites are met (distinct files);
`[USn]` the user story of the spec; `OWNER GATE` needs the named gate before it starts (plan, Owner gates).

## Stage 0: amendment proposals and owner-gated text

- [ ] T001 Amendment proposals, docs only, in `specs/011-vm-provider/amendments.md`: exact before and after text for every item
  of the spec section "Changes to existing documents" (Feature 008 A1 text, contract and refusal codes; Feature 006 FR-060 and
  scenario 7; `doctor-checks.json` rules and the six check ids; Feature 009 spec, `wizard-flow.md`, data model, schema,
  `refusal-codes.json`, the "13" places listed there; `container-runtime.md`), each as its own owner decision. It edits no
  frozen contract, ADR or other feature's text. Acceptance: `python3 -I -B scripts/check_repo.py`, `git diff --check`;
  independent review CLEAR. Traces the "Changes to existing documents" list. Prerequisites: none.
- [ ] T002 OWNER GATE (OG-8), docs only: apply amendment A1 to Feature 008 as approved in T001: `spec.md` FR-012, FR-034 and the
  FR-035 mapping, `data-model.md` (eighteen fields; `name_and_record` ownership), `contracts/runtime-provider.md` (new fields,
  container values of FR-003, the per-field refusing code, the re-verification sentence, conformance cases 1, 4, 6 and the new
  cases), `contracts/refusal-codes.json` (the eight codes of the spec appended relative to the 008 baseline, or seven if Feature 012 landed first and already added `refused.interactive_unavailable`; `refused.foreign_object` text widened exactly as FR-008).
  Acceptance: `check_repo.py`, `git diff --check`; review CLEAR; owner approval of the exact head. Traces FR-001 to FR-005,
  FR-008, FR-012, FR-030; decisions 3 and 6. Coordination: the Feature 012 plan and tasks (on main, merged as PR 297; its T022 (a)) add `interactive_shell` and `refused.interactive_unavailable` to the Feature 008 contract if this task has not landed; whichever lands first adds them, the other verifies they match FR-004 and the refusal table and adds nothing. Prerequisite: T001.
- [ ] T003 OWNER GATE (OG-5, OG-6), docs only: apply the approved amendments to Feature 006 (FR-060 check ids, budget lists,
  scenario 7, the verdict consequence of decision 22) and `docs/container-runtime.md` (VM backend prerequisite section; the
  Windows statement unchanged). No `contracts/cli/` edit here. Acceptance as T002. Traces FR-018, FR-029, FR-032, FR-039,
  FR-041; decision 22. Prerequisite: T001.
- [ ] T004 OWNER GATE (OG-10), docs only: apply the approved text amendments to Feature 009 (`spec.md`, `contracts/wizard-flow.md`,
  `data-model.md`, `contracts/refusal-codes.json` text for `host_setup.host_requirement_unmet` and the widened
  `host_setup.unknown_item`, the protocol contract text, and the "13" places of the spec list). The schema file and loader are
  changed in T029, not here. Acceptance as T002. Traces FR-031, FR-033, FR-034, FR-037, FR-038, FR-040, FR-041. Prerequisite: T001.

## Stage 1: cross-platform CI evidence (User Story 1; independent of any provider)

- [ ] T005 [US1] Skip-record check in `scripts/check_skips.py` and `scripts/test_check_skips.py`, wired into the acceptance
  manifest `docs/development/ci-acceptance.json` and `scripts/check_ci.py` so that a complete record is the only exemption from
  the existing no-skip rules (OQ-3). Failing tests first: `test_skip_without_platform_fails`, `test_skip_without_reason_fails`,
  `test_skip_without_tracking_reference_fails`, `test_complete_skip_record_passes`,
  `test_classification_must_be_known_value` (`product_defect`, `test_portability_defect`, `host_environment`, with evidence),
  `test_unrecorded_skip_still_fails_acceptance`, plus Go: a `t.Skip` call needs the record on the preceding line. Traces FR-028;
  SC-012; US1 scenario 2. Prerequisites: none (format per OQ-3). Edits `scripts/check_ci.py`, so not parallel with T006.
- [ ] T006 [US1] No-Windows check in `scripts/check_ci.py` (workflow `runs-on`, `GOOS=windows`, matrix entries) with fixtures
  in `scripts/test_check_ci.py`. Failing tests first: `test_windows_runner_label_fails`, `test_windows_goos_fails`,
  `test_unsupported_comment_in_workflow_allowed`.
  The check scans workflows and scripts only; documents are covered by review (OQ-10), and the planned tests above are limited to those files. Traces FR-029, FR-030; SC-024; OD-6. Prerequisites: T005 (same file, `scripts/check_ci.py`).
- [ ] T007 [US1] Measurement, no merge-blocking change: run the Python and Go suites on `macos-15` and `ubuntu-24.04` from a draft
  branch and record `docs/quality/011-vm-provider/platform-baseline.md` (runner architecture, durations, every failing test
  with a classification and evidence, or "none"). Failing tests are fixed in separate PRs or recorded with T005 records; this
  task changes no test. Traces FR-027, FR-028; Context 1; measurement needs. Prerequisite: T005.
- [ ] T008 [US1] OWNER GATE (OG-11): platform matrix in `.github/workflows/check.yml`: Python and Go suites per platform with the
  exact-head verification of the existing macOS jobs, `scripts/ci_changes.py` (`FULL_JOBS`, `gate`) and
  `scripts/test_ci_changes.py`. Failing tests first: `test_gate_requires_every_platform_job`,
  `test_gate_fails_when_one_platform_job_missing_or_failed`, `test_docs_only_tier_still_green_without_platform_jobs`,
  `test_aggregate_prints_tested_head_sha`, `test_runner_images_are_pinned_labels`. Runner labels per plan. Traces FR-027; SC-008;
  OD-1; US1 scenarios 1 and 3. Prerequisites: T005, T006, T007.
- [ ] T009 [US1] Evidence, not merged: a throwaway Draft PR injects a failure that exists on one platform only and shows that
  platform's job and the aggregate red while the others stay green; link recorded in
  `docs/quality/011-vm-provider/platform-baseline.md`. Traces SC-008; US1 independent test. Prerequisite: T008 merged.

## Stage 2: vendor-name gate

- [ ] T010 Vendor-name gate in `scripts/check_repo.py` with `tests/test_vendor_name_gate.py` and fixtures under
  `tests/fixtures/vendor_gate/`. Reviewed constant list (the initial list is the spec's informative note), whole-word and
  case-insensitive match; gated set exactly as FR-022; allowed places; marker comments recognized only when alone on a line;
  uppercase MUST, SHOULD, MAY inside a marked region fail. Failing tests first: `test_listed_name_in_gated_file_fails`,
  `test_same_name_in_driver_package_passes`, `test_same_name_in_marked_region_passes`, `test_start_and_restart_do_not_match`,
  `test_marker_not_alone_on_line_is_not_recognized`, `test_uppercase_requirement_word_in_marked_region_fails`,
  `test_catalog_asset_and_runtime_built_text_allowed`, `test_current_tree_passes` (spec, Feature 008 contract files and
  `apps/whr` command text are clean today). Docker is not on the list. Traces FR-022; SC-009; decisions 4 and 5; OD-3.
  Prerequisites: none.
- [ ] T011 Driver support-matrix check in `scripts/check_repo.py` with fixtures: every directory under
  `apps/whr/internal/runtime/drivers/<name>/` needs `docs/drivers/<name>.md` with separate host and guest sections, limits and a
  license note naming the license as the user's concern. Failing tests first: `test_driver_without_matrix_fails`,
  `test_matrix_must_list_host_and_guest_separately`, `test_matrix_must_carry_license_note`, `test_no_driver_no_matrix_passes`.
  Traces FR-024, FR-036; SC-020, SC-031 (documentation half). Prerequisite: T010.

## Stage 3: library, offline (fake driver; no command registered)

Stage 3 needs Feature 008 on `main`: 008/T001 to T003, T008, T009 for T012 to T018, T023 and T024; 008/T011 to T014 where
named. If absent, stop and ask; do not reimplement. No task here edits `contracts/cli/` or an ADR (per-PR empty diff).

- [ ] T012 [US3] A1 capability types and refusal codes in `internal/runtime/capabilities.go`, `refusal.go`: `guest_os`
  (`linux`, `darwin`, `windows` representable), `guest_arch` (including `unknown`), `base_image`, `concurrency_limit`,
  `exec_argv`, `ownership_mechanism`, `interactive_shell`; eighteen fields; eight codes in `contracts` freeze test. Failing
  tests first: `TestCapabilityFieldsCompleteAndTyped` (eighteen), `TestRefusalCodesMatchContract` (the eight spec codes present relative to the baseline, seven new if Feature 012 added one first; none
  renamed), `TestRefusalCodesUniqueAndStable`, `TestWindowsGuestRepresentableNeverDeclared`,
  `TestGuestArchUnknownNeverSatisfiesRequirement`. Traces FR-001, FR-004, FR-030; SC-021; decisions 3, 6. Prerequisites: T002,
  008/T001.
- [ ] T013 [US3] Regression row set, captured before `Admit` changes: `internal/runtime/admit_regression_test.go` runs
  every row of `TestAdmitTable` against the container values of FR-003. Failing tests first: none can fail on a green baseline;
  the task commits `TestAdmitTableContainerValuesUnchanged` green against the unamended `Admit` and the commit hash is
  recorded, so T014 can prove no row changed. Traces FR-003; SC-002; decision 1 (container `interactive_shell=none`).
  Prerequisite: 008/T002.
- [ ] T014 [US3] A1 admission paths in `internal/runtime/admit.go` (pure, reads a new field only when the request names that
  requirement; `base_image` for every request). Failing tests first: `TestAdmitGuestOSMismatchRefused`,
  `TestAdmitGuestArchMismatchRefused`, `TestAdmitMicroVMDoesNotImplyGuestOS`, `TestAdmitWindowsRefusedUntilDeclared`,
  `TestAdmitBaseImageNamedNeedsReadOnlyCloneSource`, `TestAdmitNoBaseImageNeedsNone`, `TestAdmitToolMountsUnavailableRefused`,
  `TestAdmitInteractiveRefusedUnlessEnforcedPTY`, `TestAdmitUnknownNeverSatisfies`, `TestAdmitRefusalCallsNoDriver`, and
  `TestAdmitTableContainerValuesUnchanged` stays green. The `interactive_shell` admission rows share `admit.go` with Feature 012 T004 (`TestAdmitInteractiveShellTable` and its siblings): whichever lands first adds the rows, the other verifies them and adds nothing, and the assertions must not diverge. Traces FR-001, FR-002, FR-003, FR-005, FR-006 (admission rows), FR-016
  (refusal only), FR-030; SC-001, SC-002, SC-014 (refusal half), SC-021; decision 14 (refusal part). Prerequisites: T012, T013.
- [ ] T015 [P] [US4] Driver port and recording fake driver in `internal/runtime/vm/driver.go` and `internal/runtime/vm/fake/`:
  closed operation set (probe, list, clone, start, address and readiness, exec, copy in, copy out, stop, delete), optional size
  and storage-location fields in `probe` and `list` results, call recorder, failure injection at each operation. Failing tests
  first: `TestDriverPortOperationSetIsClosed` (method set equals the FR-021 list: no build, publish, pull, setting or install
  operation), `TestFakeDriverRecordsEveryCall`, `TestFakeDriverInjectsFailureAtEachOperation`,
  `TestProbeAndListResultsHaveOptionalSizeFields`. Traces FR-021, FR-017, FR-032 (port part); SC-016 (port part). Prerequisite: 008/T001.
- [ ] T016 [US3] Explicit driver selection in `internal/runtime/vm/select.go`. Failing tests first: `TestNoDriverChosenRefusedZeroCalls`,
  `TestChosenDriverMissingOrUnsupportedOnlyProbeCalled`, `TestOnlyChosenDriverRecordsCalls`,
  `TestNoAutoDetectNoFallbackNoSubstitution`, `TestSwapDriverChangesAdmissionOnlyThroughCapabilityValues`. Two fake drivers; the
  configuration value is injected (OQ-1). Traces FR-023; SC-022; decision 6. Prerequisites: T012, T015.
- [ ] T017 [US5] Ownership and local records in `internal/runtime/vm/ownership.go`, `records.go`: name prefix plus id, record with
  the full label set, re-verification before every mutation, export only through the record. Failing tests first:
  `TestOwnershipNeedsNameAndAgreeingRecord`, `TestPrefixCollisionForeignVMRefusedZeroMutations`,
  `TestRecordMissingAnyLabelIsNotOwned` (table over all ten labels of the Feature 008 data model),
  `TestNonEnforcedOwnershipRefusesCreateExecExportCancelRemoveAndListsNothing`, `TestListReturnsOnlyOwned`,
  `TestExportResolvesInstanceOnlyThroughRecord`, `TestReverifyBeforeEachMutation`, `TestDriverStateDiffersFromRecordIsReconciledNotTrusted`.
  Traces FR-008; SC-005; edge cases (foreign prefix, driver state differs). Prerequisites: T015, 008/T003.
- [ ] T018 [US3] Concurrency reservations in `internal/runtime/vm/reserve.go`: exclusive lock on the record store, reservation
  with owner process id, start time and deadline, reclaim of stale reservations. Failing tests first:
  `TestLimitOneTwoSimultaneousCreatesOneRefused`, `TestRefusedCreateMadeZeroDriverCalls`,
  `TestReservationReleasedOnFailedCreateAndOnRemove`, `TestKilledCreateReservationReclaimed`,
  `TestStaleReservationWithMatchingInstanceKeepsCounting`, `TestUnknownLimitNeverRefuses`,
  `TestNonEnforcedIntegerLimitNeverRefuses`, `TestReservationWrittenEvenWithoutLimit`, `TestLimitZeroRefusesEveryCreate`,
  `TestOnlyOwnedInstancesCount`, `TestDriverOrHostRefusalIsFailedCreateNotConcurrency`. Fake clock, fake liveness port,
  deterministic barrier for the simultaneous case. Traces FR-009, FR-010 (edge case); SC-013; decision 2. Prerequisite: T017.
- [ ] T019 [US2] Plan, base image rules and mounts in `internal/runtime/vm/plan.go`, `clone.go`. Failing tests first:
  `TestPlanRecordsBaseImageNameAndDriverIdentifier`, `TestCloneNamesBaseImageOnlyAsReadOnlySource`,
  `TestBaseImageNeverStartedWrittenOrDeleted`, `TestBaseImageUnavailableRefusedBeforeAnyCloneCall` (table: missing, running,
  locked, not a clone source, provider not `read_only_clone_source`, no image named on a provider that is not `none`),
  `TestBaseImageIdentifierUnchangedAfterFailedAndCanceledRuns`, `TestPlanRefusesHomeSecretPathPrimaryGitAndHostSocketMounts`,
  `TestPrivateCopyIsDefault`, `TestSharedMountReadOnlyByDefault`, `TestSecretPathInPrivateCopyRefusedUnlessNamedAndConfirmed`.
  Traces FR-006, FR-014; SC-007 (fake half), SC-015, SC-023. Prerequisites: T014, T017, 008/T011 (copy policy).
- [ ] T020 [P] [US2, US5] Readiness, failure and cleanup in `internal/runtime/vm/lifecycle.go`, `cleanup.go`. Failing tests first:
  `TestReadyOnlyAfterReachabilityWithinDeadline`, `TestStartFailureRemovesOnlyWhatWasCreatedAndListsLeftovers`,
  `TestCancelAndRemoveBoundedIdempotentGracefulThenForce`, `TestExportAllowedFromStoppedAndFailedCloneBeforeRemoval`,
  `TestRemoveAfterKilledRunLeavesNoOwnedInstance`, `TestInjectedFailureAtEachStepLeavesNoOwnedInstanceAndKeepsResults` (clone,
  start, ready, copy in, exec, copy out, stop, delete), `TestDiskFullDuringClone`, `TestHostSleepOrBackendRestartDoesNotCorruptRecord`,
  `TestResultsInResultDirectoryNeverDeletedByRemove`. Traces FR-010, FR-011, FR-020; SC-006, SC-017; US5; edge cases. Prerequisites: T018, T019.
- [ ] T021 [P] [US2] Exec, argv and environment in `internal/runtime/vm/exec.go`, `argv.go`. Failing tests first:
  `TestHostileArgvCorpusRoundTripsByteExact` (empty, spaces, quotes, newline, tab, `$`, backtick, backslash, `;`, `&&`, glob,
  leading dash, non-ASCII, 64 KiB; the host POSIX shell is the declared shell model, a second quoting model proves the table
  extends), `TestExecArgvUnavailableRefusesEveryExecUnderEveryProfile`, `TestExecArgvNotEnforcedRefusesEvenWhenNativeOrQuotedVerified`,
  `TestQuotedVerifiedNeedsCorpusProof`, `TestEnvironmentEqualsFeature008FR023Result`, `TestNoHostCredentialVariableInGuest`,
  `TestEgressReportedNoneWithoutPassingProbe`. Traces FR-012, FR-013, FR-015; SC-004, SC-018. Prerequisites: T019, 008/T007 (environment filtering).
- [ ] T022 [US2] Test run through the sandbox lifecycle and export in `internal/runtime/vm/run.go`. Failing tests first:
  `TestRunCallOrderIsCloneStartReadyCopyInExecCopyOutStopDelete` (probe and list excepted),
  `TestFailingTestCommandStillExportsAndRemovesClone`, `TestRunResultDirectoryHasStatusLogsArtifacts`,
  `TestMissingBaseImageRefusedNothingCreated`, `TestRunUsesLifecycleOperationsOnly`. Traces FR-019, FR-020, FR-006; SC-006,
  SC-007 (fake half); US2 scenarios 1 to 3. Prerequisites: T020, T021, 008/T012 (export).
- [ ] T023 [P] [US3] Container provider reports the A1 values in `internal/runtime/docker/` (008/T013): `guest_os=linux`, `guest_arch`
  from the runtime or `unknown`, `base_image=none`, `concurrency_limit=unknown`, `exec_argv=native`, `ownership_mechanism=native_labels`,
  `interactive_shell=none`. Failing tests first: `TestContainerProviderReportsA1ValuesAndStatuses`, `TestContainerProviderUnknownArchOnlyAffectsArchRequests`,
  `TestContainerAdmissionRegressionRowsUnchanged`. Traces FR-003; SC-002; decision 1. Prerequisites: T014, 008/T013.
- [ ] T024 [US4] Conformance cases and the second fake driver: the eight cases of FR-025 in `internal/runtime/conformance/`, run
  for the fake provider, the container provider and the fake VM provider, plus a second fake driver in test code only. Failing
  tests first: `TestConformanceGuestAndArchMismatchZeroDriverCalls`, `TestConformanceConcurrencyLimit`,
  `TestConformanceOwnershipWithPrefixCollision`, `TestConformanceBaseImageIdentifierUnchangedAfterRun`,
  `TestConformanceHostileArgvCorpus`, `TestConformanceCleanupAfterKilledRun`, `TestConformanceUnknownNeverSatisfies`,
  `TestConformanceEnforcedFieldNeedsPassingProbe`, `TestConformanceSuiteSecondFakeDriver`, `TestFakeVMProviderReportsAllEighteenFieldsWithStatusAndScope` (the full VM provider report: guest `darwin` or per driver, `base_image=read_only_clone_source`, limit, `exec_argv`, `ownership_mechanism`, `tool_mounts`, egress `none`), `TestFakeVMProviderReportsInteractiveShellNoneEnforced` (OQ-11),
  `TestSecondDriverChangesNoProductionContractFile`, `TestNoProviderKindOrVendorBranchesInProductionCode`. Traces FR-025, FR-021,
  FR-023; SC-001, SC-003, SC-021. Prerequisites: T016, T018, T020, T021, T022, T023, 008/T009.
- [ ] T025 [US3] Boundaries and offline acceptance. Failing tests first: `TestNoCodePathPublishesUploadsSignsOrPullsImages`,
  `TestProviderPackageHasNoBuildOperation`, `TestConfigHoldsOnlyGuestAccountReference` (no credential field),
  `TestMutatingCallsTargetOnlyOwnedInstancesAndCloneReadsBaseOnlyAsSource`, `TestNoHostSettingOrInstallCalls`,
  `TestNoLicenseReadingOrEnforcementInSource` (source scan), `TestRegistryAddsNoCommandFor011` (command table and
  `contracts/cli/commands.json` unchanged), `TestMakeCheckPassesWithNoBackendInstalled` (recorded in the PR). Traces FR-007,
  FR-017, FR-019, FR-021, FR-026, FR-036; SC-011, SC-016, SC-019, SC-031 (code half). Prerequisite: T024.

## Stage 4: commands and integration (macOS host surface; owner gates)

Stage 4 needs 009 T001 (catalogue), T002 (detection and plan model), T003 (pkgmgr), T006 (profile and apply), T007 (fix engine), T010 (wizard) and 006/T027.
Real brew, real builds and real hosts are never used; fakes only (Feature 009 FR-023). Linux `setup` and `doctor --fix` output
is unchanged in every task.

- [ ] T026 Read-only `vm.backend`, `vm.base_image`, `vm.limit`, `vm.space` in `internal/doctor/vm.go` through the 006/T027
  port. Failing tests first: `TestVMChecksCallOnlyProbeAndList` (no `exec`, `start`; file tree unchanged),
  `TestVMLimitUnknownOrNotEnforcedReportsNoLimitAndNoRefusal`, `TestVMLimitReportsOwnedCountPendingAndStaleReservations`,
  `TestVMChecksSkippedOnLinuxAndWithoutDriver` (`unsupported_platform`, `no_vm_driver`, never `fail`),
  `TestVMBackendMissingChosenDriverFails`, `TestVMSpaceFailsBelowBaseImageSize`, `TestVMSpaceUnknownWithSizeUnknown`,
  `TestVMSpacePassesOtherwise`, `TestVMChecksStayInsideSharedFiveSecondBudget`, `TestDoctorVerdictUnchangedBySkippedChecks`.
  Traces FR-018, FR-032, FR-017; SC-010, SC-026; decisions 8 (A, B), 19(a), 22. Prerequisites: T016, T017, T018, 006/T027, T003.
- [ ] T027 [P] `container.image` and `container.leftovers` in `internal/doctor/container.go`, endpoint through an injected
  port, reuse of `localdocker` list and inspect. Failing tests first: `TestContainerImagePassFailSkipped` (labeled image, missing or
  unusable, no defined image `no_image_defined`), `TestContainerLeftoversListOwnedOnlyWithSizeOrSizeUnknown`,
  `TestContainerLeftoversNeverFailDoctor`, `TestContainerChecksReadOnlyListAndInspectOnly`,
  `TestContainerChecksSkippedOnLinuxAndWithoutEndpoint` (`unsupported_platform`, `no_runtime`). The endpoint comes from the selection function of draft PR 299 (OQ-6). Traces FR-039; SC-034 (checks
  half); decisions 13, 23; OQ-6 for the real endpoint. Prerequisites: 006/T027, 009/T006, T003.
- [ ] T028 [P] `disk.container_storage` macOS lookup in `internal/doctor/disk.go`: runtime configuration file (bounded
  plain read), then the recorded value, else `unknown`; the disk-size setting is information only. Failing tests first:
  `TestStorageLookupMakesNoRuntimeAPICallAndStartsNoProcess`, `TestConfigFileFirstAndSourceNamed`,
  `TestRecordedValueUsedWhenFileAbsentOrUnusable`, `TestNeitherGivesUnknownWithReason`, `TestFreeSpaceIsHostVolumeOfLocation`,
  `TestFailBelowExistingThreshold`, `TestDiskSizeSettingIsInformationOnly`, `TestRecordedValueThatNoLongerExistsIsUnknownNotFail`,
  `TestLookupDrawsOnSharedBudget`, `TestHomeRedactedInEvidence`; Linux `/var/lib/docker` rule unchanged. Traces FR-041; SC-036;
  decisions 21, 23. Consumes the Colima storage-location library of draft PR 299, which may already cover this lookup (OQ-6). Prerequisites: 006/T027, T003.
- [ ] T029 OWNER GATE (OG-7, OG-10): catalog schema and loader for the closed third-party source list: `source.tap` enum grows
  by the two listed sources, the loader rule "tap matches type" amended, optional `note` and `host_requirement`, in
  `specs/009-host-setup-wizard/contracts/host-tool-catalogue.schema.json`, `hostsetup/catalogue/catalogue.go` and tests. Failing
  tests first: `TestCatalogAcceptsCoreAndListedThirdPartyTap`, `TestCatalogRejectsTapOutsideListWildcardUserSourceAndSourceList`,
  `TestPreviewShowsSourceAndPermanentAddStatement`, `TestNoteAndHostRequirementPropertiesValidate`,
  `TestInitialCatalogueEntries` is updated to assert the exact id set (OQ-6), not a literal count. Reopens 009/T001. Traces
  FR-038, FR-040; SC-033 (loader half), SC-035; decisions 17, 18. Prerequisites: T004, 009/T001.
- [ ] T030 OWNER GATE (OG-7): the three catalog entries (VM tool, the OCI-bundle tool for macOS arm64 only, the GitHub overview
  tool with the Git 2.46 `host_requirement`), none preselected, in `hostsetup/catalogue/assets/catalogue.json` and templates;
  `host_setup.host_requirement_unmet` with the existing `blocked` status. Failing tests first:
  `TestNewEntriesCarryAllRequiredFieldsAndAreNotPreselected`, `TestOCIBundleToolOnlyOnMacOSArm64AndPreviewStatesRuntimeNote`,
  `TestOverviewToolBlockedWhenGitAbsentShimOrOlderAndNoGitInstallCall`, `TestOverviewToolOfferedWithGit246`,
  `TestOverviewToolDependsOnCatalogGitInSamePlanAndRedetects`, `TestNeverSelectedEntryLeavesNoProfileRecordAndIsSkippedUnselected`.
  The exact source name of the VM tool is verified here (plan, measurement needs). Traces FR-040; SC-035; decisions 12, 18.
  Prerequisites: T029, 009/T002.
- [ ] T031 Tap-sourced install and upgrade steps in `hostsetup/pkgmgr/` and `hostsetup/plan/`: `brew_install_source`,
  `brew_upgrade_source` (modify class), per-item confirmation, argv table form `<tap>/<name>` for the listed sources only, and
  `doctor --fix` install of a missing selected item except the VM tool. Failing tests first:
  `TestInstallAndUpgradeCallRecordedOnlyAfterSelectionAndItemConfirmation`, `TestYesAloneAndYesWithOnlyRecordNeither`,
  `TestUnattendedWithoutConfirmExits70`, `TestSourceOutsideListRefusedByArgvTable`, `TestNeverReplacesOrRemovesToolFromAnotherSource`,
  `TestDoctorFixInstallsOrUpgradesOnlyWithItemConfirm`, `TestDoctorFixNeverInstallsVMTool`, `TestWizardAsksTapItemsOneByOneDefaultNo`.
  Traces FR-038, FR-031 (offer a), FR-017; SC-033, SC-025 (install half); decisions 12, 13, 19(c), 20. Prerequisites: T029, 009/T003, 009/T006, 009/T007, 009/T010.
- [ ] T032 Image library in `hostsetup/image/`: definition templates (container build file, VM recipe), size preview ("size
  unknown"), offer state machine, build runner ports for the container backend and the VM backend (separate from the driver
  port, OQ-2) with fakes, label and record data. Failing tests first: `TestOfferShowsDefinitionAndSizePreviewBeforeAnyBuild`,
  `TestNoBuildOrDownloadBeforeYes`, `TestNoWritesAndNoBuildOnNo`, `TestYesAloneApprovesNeitherWriteNorBuild`,
  `TestUnattendedWithoutConfirmExits70NoBuildCall`, `TestExistingDefinitionByteIdenticalUnlessDiffConfirmedWithVerifiedBackup`,
  `TestContainerBuildUsesOnlyNormalBuildInvocationLabelOnlyNoVMSettingCall`, `TestBaseLayerFetchAllowedPrebuiltPullAndPublishFail`,
  `TestDefinitionIsNotACatalogDotfile`, `TestImageBuildDeadlineIsInjected`, `TestContainerBuildUsesSelectedEndpoint` (the build invocation carries `--host=<selected endpoint>`, taken from the endpoint port of OQ-6, never the Docker client's active context). Traces FR-034, FR-007, FR-037 (shared mechanics);
  SC-029, SC-016; decisions 10, 11, 16, 19(d), 23. Prerequisites: T004, 009/T006. Needs the `image_build` deadline (OQ-7).
- [ ] T033 Wizard image offer and profile records: the `image_definition_write` and `image_build` steps (modify class), the
  host profile records for the image (home-relative path, name, label), `container_storage` and the `container_endpoint`
  neighbor, accepted confirmation ids `image:container` and `image:vm`. Failing tests first:
  `TestImageOfferIsOwnStepWithDefaultNoAndItemConfirmation`, `TestProfileRecordsImageAndStorageAsPathsOnly`,
  `TestStorageRecordedOnlyWhenConfigFileExistsAndNotByDoctor`, `TestUnknownItemIdsStillExit64ExceptWidenedSet`,
  `TestLinuxSetupOutputUnchanged`. Traces FR-034, FR-041 (wizard half); SC-029, SC-036 (wizard half); decisions 10, 11, 21.
  Prerequisites: T028, T032, 009/T010.
- [ ] T034 VM readiness summary at the end of `setup` (macOS) in `hostsetup/` wizard output: built from the same checks,
  `probe` and `list` only, next steps for every check not `pass`, also for answer `no` and `--dry-run`. Failing tests first:
  `TestReadinessSummaryInEachFakeDriverState` (no driver, backend missing, image missing, all ready),
  `TestNextStepsForEveryNonPassCheckIncludingDeclinedOffer`, `TestSummaryMakesOnlyProbeAndListCallsStartsNoVM`,
  `TestSummaryShownForNoAndDryRun`, `TestNothingInstalledBuiltOrElevatedWithoutSelection`, `TestLinuxSetupOutputUnchanged`. Traces
  FR-031, FR-018; SC-025; decisions 7, 12. Prerequisites: T026, T031, T033, 009/T010.
- [ ] T035 [US5] `doctor --fix` leftover cleanup in `hostsetup/fix/`: step kinds `vm_instance_remove`,
  `vm_reservation_release`, `container_instance_remove`, item ids `vm-instance:`, `vm-reservation:`, `container-instance:`,
  removal only through the provider `Remove` with the ownership check, "results not exported" marking. Failing tests first:
  `TestFixDryRunWritesNothing`, `TestFixPreviewExit70`, `TestYesAloneRemovesNothing`, `TestConfirmedItemRemovedThroughRemoveExactly`,
  `TestForeignOrUnlabeledObjectRefusedAndUntouched`, `TestActiveRunIsNotALeftover`, `TestFixWithoutProfileRefusesProfileAbsent`,
  `TestFixMakesNoInstallCallForVMTool`, `TestLinuxFixStillRefused`, `TestPreviewMarksResultsNotExportedForStoppedAndFailedClone`,
  `TestConfirmingUnexportedItemIsTheResultsDeletionConfirmation`, `TestResultDirectoryFilesByteIdenticalAfterRemoval`,
  `TestPreviewNamesNoExportCommandWhileNoneRegistered`. Traces FR-033, FR-020, FR-011; SC-027, SC-028, SC-034 (cleanup half);
  decisions 13, 19(b). Prerequisites: T020, T027, 008/T014, 009/T007.
- [ ] T036 `doctor --fix` image rebuild in `hostsetup/fix/`: item ids `image:container`, `image:vm`, preview of definition
  file, size and effort, replacement of an unusable image, missing definition reported and never regenerated. Failing tests
  first: `TestRebuildPreviewExit70`, `TestYesAloneBuildsNothing`, `TestConfirmedRebuildBuildsFromDefinitionNoDownloadBefore`,
  `TestMissingDefinitionReportedNotRegenerated`, `TestFailedBuildIsFailedStepPointingToSetup`, `TestSameForContainerAndVM`,
  `TestRebuildNeverInstallsVMTool`, `TestLinuxFixStillRefused`. Traces FR-037, FR-034; SC-032; decision 13. Prerequisites: T032, T035.
- [ ] T037 OWNER GATE (OG-4, OG-5): register the six checks and output in three PRs: (a) `contracts/cli/doctor-checks.json` (ids,
  `skipped` reasons, the amended read-only rule and budget lists) and `contracts/cli/output.schema.json` (`vm_readiness`) with
  `TestDoctorChecksMatchContract`, `TestSetupResultSchemaValid` and existing fixtures valid; (b) doctor wiring of T026 to T028
  with `TestDoctorOutputListsNewChecksWithStatuses`; (c) `setup` and `doctor --fix` wiring of T031 to T036 with
  `TestSetupFlagsAndExitCodes`, `TestDoctorFixFlagsAndExitCodes`. Starts only after T003, T004 are merged and 009/T011 is approved.
  The only exception of Feature 011 to the empty `contracts/cli` diff. Traces FR-018, FR-032, FR-039, FR-031, FR-033, FR-034,
  FR-037. Prerequisites: T026 to T036, T003, T004, 009/T011.
- [ ] T038 [US3] Core command parity in `internal/commands/` tests: one registry entry per core command for both backends, no
  backend-specific command replacing one, same command-level conformance cases for the fake container provider and the fake VM
  provider. Failing tests first: `TestRegistryHasOneEntryPerCoreCommandForBothBackends`,
  `TestNoBackendSpecificCommandReplacesCoreCommand`, `TestCommandConformanceSameForContainerAndVMDifferingOnlyInReport`,
  `TestRefusalNamesItsReason`. Traces FR-035, FR-016 (command half deferred, D-1); SC-030 (partly: the shell-command half is deferred, D-1); decision 9. Prerequisites: T024, 008/T016, 008/T017.

## Stage 5: first driver, documentation and acceptance

- [ ] T039 OWNER GATE (OG-1 for any real run): the `<first-driver>` adapter in `internal/runtime/drivers/<first-driver>/` over a
  process-runner port with recorded backend output, its `docs/drivers/<first-driver>.md` support matrix, and the driver tests.
  Starts only after the owner has done the license and maintenance check of the spec ("Verification before the plan"), which has not been done; the spec places it before the plan. Failing
  tests first: `TestDriverPassesConformanceWithRecordedOutput`, `TestDriverDeclaresOnlyWhatItsProbesProve`,
  `TestProbeAndListParseOptionalSizeAndStorageFields`, `TestDriverRunnerArgvIsFixedNoShell`, `TestDriverNeverInstallsOrChangesHostSettings`,
  `TestSupportMatrixPresentAndCheckedByT011`. Real-VM conformance is owner-run evidence (D-2). Traces FR-021, FR-023, FR-024,
  FR-026; SC-003, SC-016, SC-020; OD-2, OD-5. Prerequisites: T011, T025, plan open question OQ-9.
- [ ] T040 Documentation, docs only: the reproducible base image recipe from the operating system vendor's official download with a
  local image and no `whr` involvement (FR-007), the user's responsibility for licenses in the driver documentation and the
  prerequisite section (FR-036), and the owner-run evidence procedure (host digest of the image files before and after a run).
  Names of the VM backend appear only in `docs/drivers/`. Acceptance: `check_repo.py` including the T010 gate,
  `git diff --check`; independent review CLEAR. Traces FR-007, FR-036, FR-006 (owner-run), FR-026; SC-031. Prerequisites: T010, T039.
- [ ] T041 Acceptance and handoff: coverage table below completed with test names and revisions, checklist items checked only with
  cited evidence, `check_repo.py` and the Go tests recorded, per-PR frozen-path diffs empty except T002 to T004, T008, T029,
  T030, T037. Real-VM, real-build and real-host confirmation listed as pending owner gates (D-2). Independent exact-head review
  CLEAR. Prerequisites: T001 to T040.

## Dependencies and execution

Stages 1 and 2 start after approval with no provider; T001, T005 and T010 are independent first PRs (T006 follows T005, T011 follows T010). Stage 3 starts when
the named Feature 008 tasks are on `main` and T002 is merged. Stage 4 follows Stage 3 and the named Feature 009 tasks. Owner
gates in this list: OG-1 (real runs, none of T001 to T038 may use one), OG-3 (real builds, evidence only), OG-4 and OG-5
(`contracts/cli/*`, doctor catalog, `setup`, `container-runtime.md`), OG-6 (Feature 006 text), OG-7 (catalog entries and schema),
OG-8 (Feature 008 text), OG-10 and OG-11 (proposed, plan). T002 to T004, T008, T029, T030 and T037 are the only
exceptions to the per-PR empty diff on frozen paths.

| Task | Prerequisites |
| --- | --- |
| T001 | none |
| T002, T003, T004 | T001 |
| T005 | none |
| T006 | T005 |
| T007 | T005 |
| T008 | T005, T006, T007 |
| T009 | T008 |
| T010 | none |
| T011 | T010 |
| T012 | T002, 008/T001 |
| T013 | 008/T002 |
| T014 | T012, T013 |
| T015 | 008/T001 |
| T016 | T012, T015 |
| T017 | T015, 008/T003 |
| T018 | T017 |
| T019 | T014, T017, 008/T011 |
| T020 | T018, T019 |
| T021 | T019, 008/T007 |
| T022 | T020, T021, 008/T012 |
| T023 | T014, 008/T013 |
| T024 | T016, T018, T020, T021, T022, T023, 008/T009 |
| T025 | T024 |
| T026 | T016, T017, T018, 006/T027, T003 |
| T027 | 006/T027, 009/T006, T003 |
| T028 | 006/T027, T003 |
| T029 | T004, 009/T001 |
| T030 | T029, 009/T002 |
| T031 | T029, 009/T003, 009/T006, 009/T007, 009/T010 |
| T032 | T004, 009/T006 |
| T033 | T028, T032, 009/T010 |
| T034 | T026, T031, T033, 009/T010 |
| T035 | T020, T027, 008/T014, 009/T007 |
| T036 | T032, T035 |
| T037 | T026 to T036, T003, T004, 009/T011 |
| T038 | T024, 008/T016, 008/T017 |
| T039 | T011, T025 |
| T040 | T010, T039 |
| T041 | T001 to T040 |

## Coverage

Every requirement, success criterion and decision maps to a task or to a deferral with its reason. "(fake)" means the offline
half; the real half is owner-run evidence (D-2 in the plan).

| Requirement | Task(s) |
| --- | --- |
| FR-001 | T012, T014, T024 |
| FR-002 | T014 |
| FR-003 | T013, T014, T023 |
| FR-004 | T012 |
| FR-005 | T014 |
| FR-006 | T014, T019, T022; real digest: deferred D-2, procedure T040 |
| FR-007 | T025, T032, T040 |
| FR-008 | T002, T017 |
| FR-009 | T018 |
| FR-010 | T020 |
| FR-011 | T020, T035 |
| FR-012 | T021 |
| FR-013 | T021 |
| FR-014 | T019 |
| FR-015 | T021, T024 |
| FR-016 | T014 (refusal); `pty` path and command: deferred D-1 (the command belongs to Feature 012 per FR-016 and decision 14) |
| FR-017 | T015, T025, T026, T031, T035 |
| FR-018 | T026, T037 |
| FR-019 | T022, T025 |
| FR-020 | T020, T022, T035 |
| FR-021 | T015, T024, T025 |
| FR-022 | T010 |
| FR-023 | T016, T024 |
| FR-024 | T011, T039 |
| FR-025 | T024 |
| FR-026 | T025 (offline); real conformance: deferred D-2 |
| FR-027 | T007, T008 |
| FR-028 | T005, T007 |
| FR-029 | T006 |
| FR-030 | T012, T014 |
| FR-031 | T031, T034 |
| FR-032 | T026 |
| FR-033 | T035 |
| FR-034 | T032, T033 |
| FR-035 | T038 |
| FR-036 | T011, T025, T040 |
| FR-037 | T036 |
| FR-038 | T029, T031 |
| FR-039 | T027 |
| FR-040 | T030 |
| FR-041 | T028, T033 |

| Success criterion | Task(s) |
| --- | --- |
| SC-001 | T014, T024 |
| SC-002 | T013, T014, T023 |
| SC-003 | T024, T039 |
| SC-004 | T021, T024 (corpus per declared shell model; real guest shells: deferred D-2) |
| SC-005 | T017 |
| SC-006 | T020, T022 |
| SC-007 | T019, T022 (fake); real image digest: deferred D-2 |
| SC-008 | T008, T009 |
| SC-009 | T010 |
| SC-010 | T026 |
| SC-011 | T025 |
| SC-012 | T005 |
| SC-013 | T018 |
| SC-014 | T014 (refusal half); enforced `pty` half: deferred D-1 |
| SC-015 | T019 |
| SC-016 | T015, T025, T026, T032, T035, T039 |
| SC-017 | T020 |
| SC-018 | T021 |
| SC-019 | T022, T025 |
| SC-020 | T011, T039 |
| SC-021 | T012, T014, T024 |
| SC-022 | T016 |
| SC-023 | T019 |
| SC-024 | T006 (automated half); documents: review (OQ-10) |
| SC-025 | T031, T034 |
| SC-026 | T026 |
| SC-027 | T035 |
| SC-028 | T035 |
| SC-029 | T032, T033 |
| SC-030 | T038 partly (waits for 008/T016, T017); shell-command half: deferred D-1 |
| SC-031 | T011, T025, T040 |
| SC-032 | T036 |
| SC-033 | T029, T031 |
| SC-034 | T027, T035 |
| SC-035 | T029, T030 |
| SC-036 | T028, T033 |

| Decision | Task(s) |
| --- | --- |
| OD-1 | T005 to T009 |
| OD-2 | T039, owner-run evidence (D-2) |
| OD-3 | T010, T012 to T025 |
| OD-4, OD-5 | T010, T039, T040 (informative text stays in marked regions and `docs/drivers/`) |
| OD-6 | T006, T012 |
| 1 | T013, T023; shell feature deferred D-1 |
| 2 | T018 |
| 3 | T012 |
| 4, 5 | T010 |
| 6 | T002, T012, T016 |
| 7, 12 | T030, T031, T034 |
| 8 | T026 (A, B), T035 (C); D (`doctor --deep`): deferred D-3 |
| 9 | T038 |
| 10, 11 | T032, T033 |
| 13 | T027, T035, T036 |
| 14 | T014 (refusal), T038; command: deferred D-1 |
| 15 | T025, T040 |
| 16 | T032 |
| 17, 18 | T029, T030 |
| 19 | T026 (a), T035 (b), T031 (c), T032 (d) |
| 20 | T031 |
| 21 | T028, T033 |
| 22 | T003, T026 |
| 23 | T027, T028, T032 (image build endpoint); selection itself: Feature 009, OQ-6 |
| Follow-up outside the feature (BundleDock, Anchorage CI) | deferred D-4 |
