# Feature 012 work items

Status: proposed with the plan; not approved or started. A task starts only after the spec, plan and this file are approved at
one exact head (WORKFLOW.md) and its prerequisites have implementation and test evidence. IDs `T001` to `T027` are Feature 012's
own; other features' items are written `006/T0nn`, `008/T0nn`. Each task is one pull request of at most about 1,500 changed lines
(split when larger), targeting `main`. Write the named failing tests first, show them failing, then implement. All tests are
offline with a fake provider, a fake Docker client and daemon, fake clocks and signals, and controlled PTYs; no real Docker,
container, image build, network, credential or host mutation, except in the owner gates T002 and T025. Paths are in module
`apps/whr` under `internal/` unless stated; package names are proposals (ADR 0010). `[P]` marks a task that can run in parallel
with the other tasks that become startable at the same time (different files, no prerequisite relation between them). `[US1]` to `[US5]` are the spec's user stories. Exit codes: only the
ADR 0013 table (FR-015). Test names are planned names. Stage 1 registers no command and changes no frozen contract.

Labels: S12-C1 to S12-C9 are the spec's conformance cases; D1 to D9 its owner decisions; V1 to V5 the five verification items
of the spec in the order listed there (V1 resize, V2 detach disabling, V3 environment route, V4 signals and client status, V5
end-of-exec and reconciliation queries). OG-n gates as in Feature 008/009: OG-1 real Docker, OG-4 `contracts/cli/*`, ADR 0013 or
ADR 0014, OG-6 Feature 006 text, OG-8 Feature 008 text.

## Stage 0: binding, verification, amendment text

- [ ] T001 Approval binding and prerequisite audit in `docs/quality/012-container-interactive-shell/implementation.md`: record the
  reviewed spec/plan/tasks revisions and owner approval links; re-verify each row of the plan's dependency table from the tree
  (008/T001 to T004, T008, T009, T013; 006/T025, T031; 010 `localdocker`/`localshell`; the Feature 009 endpoint selection function (unbuilt; its plan and tasks are merged in PR #299); Feature 011 draft status and A1 application);
  `python3 scripts/select_feature.py 012-container-interactive-shell`. No red test (documentation). Acceptance: `python3 scripts/check_repo.py`,
  `git diff --check`. Prerequisites: none.
- [ ] T002 [P] OWNER GATE (OG-1), real host, no CI substitute: run V1 to V5 on a real Docker host with a terminal and record versions,
  commands and observed results in `docs/quality/012-container-interactive-shell/verification.md` (procedure in
  [quickstart.md](quickstart.md#verification-v1-to-v5)). Outcomes bind later tasks: V2 negative means no `pty`/`enforced` (FR-008);
  V5 negative means no end-of-exec query, so S12-C7 and FR-003 cannot be met and T014 stays open (FR-016, FR-024). Fakes never stand
  in for it. Covers V1 to V5 (FR-008, FR-009, FR-012, FR-013, FR-016, FR-024). Prerequisites: T001.
- [ ] T003 [P] Amendment proposals, docs only, in `specs/012-container-interactive-shell/amendments.md`: the spec's "Changes to
  existing documents" as exact before and after text, one entry per target (011 owner decision 1 and FR-003; 008 FR-028, FR-029, FR-035,
  `runtime-provider.md`, `command-surface.md`, `data-model.md`, `refusal-codes.json`; ADR 0013 and `exit-codes.json`; ADR 0014 and
  `commands.json`; 006 FR-006, FR-019, FR-022a, FR-047). It edits none of them. Each entry is its own owner decision. Acceptance:
  `python3 scripts/check_repo.py`, `git diff --check`; independent review CLEAR. Prerequisites: T001.

## Stage 1: library behind ports (no command, fakes only)

- [ ] T004 [P] [US2] Admission rows in `internal/runtime/` (extends 008/T002): `interactive_shell` of `none`, `pty` with `not_enforced`,
  `pty` with `unknown` refused with `refused.interactive_unavailable`, `pty` with `enforced` allowed (FR-004). This task edits no
  contract file: the code and, if Feature 011 A1 is not applied, the field are added by the gated T022, and the tests read them
  from the contract. The same `interactive_shell` admission rows are planned by Feature 011 T014 (`TestAdmitInteractiveRefusedUnlessEnforcedPTY`, same `admit.go`): whichever lands first adds the rows, the other verifies them and adds nothing, and the assertions must not diverge. Failing tests first: `TestAdmitInteractiveShellTable`, `TestAdmitInteractiveShellZeroProviderCalls`,
  `TestUnknownNeverSatisfiesInteractiveShell`, `TestRefusalCodesMatchContractWithInteractiveUnavailable`,
  `TestNoProviderKindBranchesInShellPath` (source scan, FR-022). Traces FR-004, FR-022; S12-C1; SC-001. Prerequisites: 008/T001,
  008/T002, T022 (a).
- [ ] T005 [P] [US1] Instance record additions and the atomic start in `sandbox/` (extends 008/T003) per
  [data-model.md](data-model.md): `ready` to `running` as an atomic conditional update (D8c), no transition back to `ready`, failure
  `reason` (closed set `transport_lost`, `container_lost`, `end_undetermined`, `interrupted`, `start_failed`, `session_lost`),
  exec process status beside the recorded client status, owner process id and start time on a `running` record; widened trigger
  texts are documentation (T022). Failing tests first: `TestReadyToRunningAtomicExactlyOneWinner` (concurrent starts),
  `TestStartRefusedUnlessReady` (every other state gives `refused.invalid_state`, `finished` gets no second shell, D5),
  `TestNoTransitionBackToReady`, `TestFailureReasonClosedSet`, `TestRunningRecordCarriesOwnerPidAndStart`,
  `TestRecordHoldsClientAndShellStatus` (D8d), `TestDiscardRefusesRunningCleanSkipsRunning`, `TestKilledWhrLeavesRunningUntilReconciled`
  (discard refused, new shell refused, export refused, FR-017), `TestExportAllowedFromFailedPartial`,
  `TestRecordNeverHoldsSessionContent`. Traces FR-005 (state), FR-014, FR-017, D5, D8b, D8c, D8d; S12-C5; SC-005. The store
  mechanism is open question 2 and [research.md](research.md#r-1); tests assert the contract, not a mechanism.
  Prerequisites: 008/T003.
- [ ] T006 [P] [US1] Plan additions in `internal/runtime/plan.go` (extends 008/T006): working directory (rule of plan decision 2; literal values asserted once the 008 Plan exists, open question 10) and login shell fields (D3);
  `/bin/sh` default for the two job profiles, the `human_shell` of the workspace manifest for `developer` via `internal/workspacecfg`.
  Failing tests first: `TestPlanCarriesWorkdirAndShellPerProfile`, `TestJobShellDefaultsBinSh`,
  `TestDeveloperShellIsManifestHumanShell`, `TestPlanWorkdirValuesPerProfileFixed`. Traces FR-008 (D3). Prerequisites: 008/T006, 008/T007 (cooperative worktree mount target), 008/T011 (untrusted private copy mount target).
- [ ] T007 [US1] Shell exec plan and environment in `sandbox/shellsession/execplan.go`, over a `Shell` port that records the plan:
  full container id, explicit `unix://` endpoint, interactive pty, detach key sequence disabled (D2), the Plan's user, workdir and
  login shell; no privileged option, capability, mount, device, limit or env-file; no operator environment; environment sources of
  FR-009 only (job `GIT_CONFIG_GLOBAL` and `GIT_CONFIG_NOSYSTEM=1`, none for `developer`; identity variables only if `Create` put
  them there; `TERM` validated against `[A-Za-z0-9._+-]{1,64}` else `xterm-256color`, reusing `localshell`'s rule by narrow
  extraction; `WHR_SANDBOX_ID`); no value in argv; no command, no agent flag. Failing tests first: `TestShellExecPlanFullIdEndpointPty`,
  `TestShellExecPlanDetachKeysDisabled`, `TestShellExecPlanNoPrivilegedCapMountDeviceLimitEnvFile`, `TestShellEnvSourcesOnly`,
  `TestShellEnvJobGitConfigDeveloperNone`, `TestShellIdentityVariablesOnlyIfCreatePutThem`, `TestShellTermValidationFallback`,
  `TestShellNoEnvValueInArgv`, `TestShellNoCommandNoAgentFlag`, `TestShellCanaryHostSocketToolMountCredentialAbsent` (per profile).
  Traces FR-008, FR-009, FR-018, FR-019, FR-020, FR-021 (scope statement; no separate test); D2, D3; S12-C2, S12-C3; SC-003.
  The real environment route is a port; its implementation follows T002 (V3). Prerequisites: T004, T006, 008/T008 (canaries).
- [ ] T008 [US1] Check order and ownership in `sandbox/shellsession/preflight.go`: the nine steps of FR-005, stop at the first
  failure, each later step proven not called. Failing tests first: `TestShellCheckOrderStopsAtFirstFailure` (exit 64, 65, 68 by
  step, zero later calls), `TestShellSelectorIsInstanceIdOnly` (malformed 64, no record 65, container id, name and ADR 0009
  container are not selectors), `TestShellInvalidStateReadsRecordOnly` (`discarded` with a gone container gets
  `refused.invalid_state`), `TestShellOwnershipRefusals` (missing, partial and mismatched labels: `refused.foreign_object`, zero
  mutations), `TestShellMissingContainerNamesMissingNotForeign`, `TestShellStoppedPausedRestartingNamedNeverStarted` (D4),
  `TestShellForbiddenMountRecheckExistingCodes`, `TestShellEndpointAndClientRefusalsExit68` (non-`unix://`, absent client,
  unreachable socket), `TestShellEndpointFromFeature009SelectionFunction` (no own search; the first specified source is the only one
  used), `TestShellNoEndpointExit68RemediationNamesOptionConfigSetup` (009 FR-032 EP-3), `TestShellMalformedContainerEndpointExit64` (usage error at step 1, owner decision D9; it also asserts that no pty is requested and no later step runs). The spec wording "discovered endpoint" (FR-005 step 5, FR-008) is replaced by "selected endpoint" by the D9 spec amendment; this task's tests use the selected endpoint of the 009 function. Traces FR-002, FR-005, FR-006, FR-007 (option), FR-018 (re-check), D4, D5, D9; S12-C4,
  S12-C5, S12-C6; SC-004, SC-005, SC-006. Prerequisites: T004, T005, 008/T003, the Feature 009 endpoint selection function (unbuilt; plan and tasks merged in PR #299).
- [ ] T009 [P] [US3] Terminal and usage gate in `sandbox/shellsession/usage.go`: stdin and stdout must be terminals; no
  `--json`, trailing command or extra positional; only `--confirm <id>`, `--container-endpoint <endpoint>` (owner decision D9), `--yes` and global options accepted. Failing tests first:
  `TestShellNonTerminalExit64NoPtyRequested`, `TestShellRejectsJSONAndTrailingArguments`, `TestShellAcceptsOnlyConfirmEndpointYesGlobal` (any other option is a usage error, exit 64), `TestShellContainerEndpointValueReachesSelection` (a well-formed value is handed unchanged to an injected selection-function port, a fake, so T009 needs no 009 code and keeps its prerequisites; the malformed-value exit 64 is T008's, which also checks that no pty is requested), `TestShellUsageChecksPrecedeProviderCalls`. The non-terminal test `TestShellNonTerminalExit64NoPtyRequested` checks that no pty is requested. Traces FR-007, D9; S12-C6; SC-006. Prerequisites: 006/T025, 006/T031.
- [ ] T010 [US5] Confirmation for `untrusted_job` in `sandbox/shellsession/confirm.go` (form per FR-023, author proposal):
  warning and prompt on standard error, answer from standard input, answer must equal the instance id. Failing tests first:
  `TestUntrustedWithoutConfirmationStartsNothing`, `TestYesAloneStillPrompts`, `TestPromptAnswerOtherThanIdExit70`,
  `TestPromptEndOfInputExit70`, `TestConfirmValueMismatchExit64`, `TestConfirmFlagWithIdProceeds`,
  `TestDeveloperAndCooperativeNeverAsked`, `TestSignalDuringPromptExit72NoStateChange`, `TestPromptOnStderrStdoutEmpty`,
  `TestStderrNotTerminalNoConfirmExit70NamesConfirmFlag`, `TestNoTerminalNeverReachesConfirmation`. Traces FR-023, D1;
  S12-C8; SC-012. Prerequisites: T008, T009.
- [ ] T011 [P] [US1] Notice and help text in `sandbox/shellsession/notice.go`: one escaped line before hand-over when stderr is a
  terminal (byte-exact text fixed in plan decision 1, modeled on Feature 010 FR-010, names profile and instance id, escaped per
  Feature 006 FR-022, no model or VM backend name); `--help` states the stream is neither inspected nor filtered; nothing written
  to the stream while the client owns the terminal. Failing tests first: `TestShellNoticeByteExactOnTerminalStderr`,
  `TestShellNoticeAbsentWhenStderrNotTerminal`, `TestShellNoticeEscapesHostileValues`, `TestShellHelpStatesStreamNotFiltered`,
  `TestShellGeneratedTextNamesNoModelOrBackend`. Traces FR-010, FR-011, FR-022
  (generated text), D6; SC-011 (FR-010, FR-011 parts). Prerequisites: T007.
- [ ] T012 [US1, US4] Commit point, launch and signals in `sandbox/shellsession/run.go`: the atomic move is the last action before
  the client launch; one child with the real descriptors; no relay. Failing tests first: `TestCommitPointIsLastActionBeforeLaunch`,
  `TestLaunchFailureIsStartFailedExit74NoRollbackToReady` (D8b), `TestSignalBeforeCommitExit72NoStateChange` (SIGINT, SIGTERM,
  SIGHUP), `TestSignalAfterCommitRecordsInterruptedExit72WhrLine`, `TestShellNeverClaimsInContainerShellEnded`,
  `TestShellNoSignalConversionFromTerminalChars` (FR-013), `TestShellClientUnavailableBetweenCheckAndLaunchIsStartFailed` (plan decision 4), `TestShellWritesNothingWhileClientOwnsTerminal`. Traces
  FR-013, FR-014, FR-015 (72, 74); D8b; S12-C7 (signals, launch); SC-008. Prerequisites: T005, T007, T008, T009, T010, T011.
- [ ] T013 [US4] End-of-session classification in `sandbox/shellsession/outcome.go`: after every client exit one read-only query
  with a 5-second budget through a port, then one row of the FR-016 table. Failing tests first: `TestEndOfSessionTableFourRows`
  (exit code, instance state, reason, `whr:` line per row), `TestEndOfSessionQueryBudget5sIsTransportLost`,
  `TestStatusPassThrough0_1_2_126_127_130_255`, `TestContainerLostPassesStatusRecordsFailed` (D8a),
  `TestKilledClientRowsClientStatusPassedShellStatusRecorded` (D8d), `TestMissingShellExecutableStatusFinishedOrRow4`,
  `TestCLIFailureWritesWhrLineShellStatusDoesNot`, `TestNoDetachKeySequenceInPlan`, `TestOnlyCodes64_65_68_70_72_74FromCLI`,
  `TestNoSessionBytesOrEnvironmentLogged`. Traces FR-014, FR-015, FR-016, FR-017, D8a, D8d; S12-C6, S12-C7; SC-007, SC-008.
  The production query is T017 (V5). Prerequisites: T005, T012, 008/T004 (`ExitFor`).
- [ ] T014 [US4] Reconciliation in `sandbox/reconcile.go` (FR-024, D7): before acting on a `running` record, check through a port
  whether the exec process still runs; if not (ended or never started) the record becomes `failed`/`session_lost`; a record whose
  owner process id and start time are alive is untouched. Failing tests first: `TestReconcileMarksGoneRunningSessionLost`,
  `TestReconcileMarksNeverStartedSessionLost`, `TestReconcileLeavesLiveOwnerUntouched`, `TestReconcileConcurrentWhrBetweenCommitAndLaunch`,
  `TestAfterReconcileDiscardAndExportFollowFailedRules`. HELD: needs the verified query of V5 (T002); until then FR-024, S12-C9
  and SC-013 are not claimed and the gap stays under "Not enforced". Not a gate for FR-003. Traces FR-024, D7; S12-C9; SC-013.
  Prerequisites: T005, T002.
- [ ] T015 [US2] Conformance wiring in `internal/runtime/conformance/`: cases S12-C1 to S12-C9 keep their stable labels as identifiers (plan decision 3: no sequential numbers assigned), a fake provider `Shell`, and a second fake provider declaring
  `pty` in test code. Failing tests first: `TestConformanceShellCasesFakeProvider`, `TestSecondProviderDeclaringPtyPassesS12C1ToC8`
  (SC-009, no production contract change), `TestConformanceReportsNoMoreThanTestsProve` (008 FR-018), `TestS12C9RunsOnlyWithQuery`.
  Traces FR-022, FR-003 (gate cases); S12-C1 to S12-C9; SC-009. Prerequisites: T004, T007, T008, T010, T012, T013 (T014 for S12-C9),
  008/T008, 008/T009.

## Stage 2: container provider (fake Docker only)

- [ ] T016 [US1] Container provider `Shell` in `internal/runtime/docker/` over the `DockerAPI` port (extends 008/T013): turn the T007
  plan into one fixed docker vector (no shell, no string evaluation), explicit endpoint, full id; client environment from an explicit
  minimal allowlist as Feature 010 does, never a wholesale host copy; reuse `localdocker` and `localshell` pieces; the endpoint comes from the Feature 009 selection function. Failing tests
  first: `TestContainerProviderShellPassesConformance`, `TestContainerShellVectorFixedAndHostileValuesNeverEvaluated`,
  `TestContainerShellClientEnvironmentExcludesTokens`, `TestContainerShellEnvRouteNotInArgv` (uses the route chosen after V3),
  `TestContainerShellNeverStartsStopsOrChangesContainer`. Traces FR-006, FR-008, FR-009, FR-018, FR-019; S12-C2 to S12-C4; SC-003,
  SC-004. Prerequisites: 008/T013, T007, T008, T015, T002 (V2, V3 results fix the flags and route).
- [ ] T017 [US4] Production end-of-exec query adapter in `internal/runtime/docker/` (and the reconciliation query if V5 allows),
  read-only, 5-second budget, behind the port of T013/T014. Failing tests first: `TestExecEndedQueryAgainstFakeDaemon`,
  `TestExecQueryUnreachableDaemonIsTransportLost`, `TestExecQueryNeverMutates`, `TestExecQueryContainerStoppedOrRemovedRows`. Traces
  FR-016, FR-014; S12-C7; SC-008. Prerequisites: T013, T016, T002 (V5 must be positive; otherwise stop and ask the owner).
- [ ] T018 [US2] Capability report in `internal/runtime/docker/capabilities.go`: `interactive_shell=none` for every profile by
  default; `pty` with status `enforced` only through a reviewed switch that T026 flips; `scope` states a pty allocated by the
  container runtime for a login shell inside the instance's container and nothing about what runs in it. Failing tests first:
  `TestCapabilityReportNoneUntilGate`, `TestCapabilityReportPtyScopeText`, `TestCapabilityReportPerProfile`,
  `TestReportClaimingMoreThanTestsProveIsNonconformant`. Traces FR-003; SC-002 (first half). Prerequisites: T015, 008/T013.

## Stage 3: owner-gated amendments and command wiring

- [ ] T019 OWNER GATE (OG-4), docs only: ADR 0014 amendment adding `sandbox shell` to the `sandbox` group, and the extension of
  008/T016 and T017 (scoped to the seven existing verbs) to cover `shell`, per T003; proposed text, no short form (FR-001; ADR 0014
  alias rules 3 and 5). Acceptance: `python3 scripts/check_repo.py`, `git diff --check`; independent review CLEAR; owner approval of the exact head.
  Traces FR-001. Prerequisites: T003, 008/T017.
- [ ] T020 OWNER GATE (OG-4), docs only: ADR 0013 and `contracts/cli/exit-codes.json` amendment: add `sandbox shell` to the
  passthrough `commands`, the killed-client exception (client's own signal status), the ambiguity note; no new code or category;
  record the owner's answer to open question 1 (72 for a killed or lost client stream). Traces FR-015, SC-007. Prerequisites: T003.
- [ ] T021 OWNER GATE (OG-6), docs only: Feature 006 text: FR-019 (list gains `sandbox shell` with the exception), FR-006 (grant
  exception extends to `sandbox shell`, D6), FR-022a (no relay filtering), FR-047 (human shell for the `developer` login shell);
  FR-030 unchanged. Traces FR-010, FR-011, FR-015, D6. Prerequisites: T003.
- [ ] T022 OWNER GATE (OG-8), docs only, two PRs, (a) first: (a) only if Feature 011 has not landed them, the code
  `refused.interactive_unavailable` (with the "when" text of 011) and the `interactive_shell` field (A1; the container provider row stays
  `none`; this task never writes `pty`, which only T026 does) in the Feature 008 contracts; (b) the rest of the Feature 008 text and contracts: FR-028, FR-029, FR-035, `runtime-provider.md` (`Shell`,
  capability row after 011 A1, cases S12-C1 to S12-C9, re-verification note), `command-surface.md`, `data-model.md` (widened
  `ready` to `running` and `running` to `failed` triggers, atomic update, failure reason, Plan fields), `refusal-codes.json` "when"
  texts of `refused.host_socket_mount` and `refused.tool_mount_writable`, (b is independent of T004); 008 `tasks.md` and `plan.md` rows for the new cases.
  Traces FR-005, FR-014, FR-018, FR-004. May run before Stage 1: T004 waits for (a) only. Prerequisites: T003.
- [ ] T023 [US1] OWNER GATE (OG-4, registration; edits `contracts/cli/commands.json`): register `whr sandbox shell` with its flag list (`--confirm`, `--container-endpoint`, owner decision D9) in `internal/commands/` and `contracts/cli/commands.json`
  within the extended 008/T016 scope, with help text and the freeze tests. Failing tests first: `TestSandboxShellCanonicalNoShortForm`,
  `TestCommandTableMatchesContractWithSandboxShell`, `TestSandboxShellRejectsJSONBeforeProvider`,
  `TestExitForGainsNoNewCodeAndFreezeUnchanged` (SC-007), `TestSandboxShellHelpStatesNoFiltering`,
  `TestSandboxShellNoticeByteExactThroughCommand`. Traces FR-001, FR-002, FR-007, FR-010, FR-011, FR-015; SC-007, SC-011. May change
  `contracts/cli/` only as T019/T020 approved. Prerequisites: T012, T013, T019, T020, T021, 008/T004, 008/T016.
- [ ] T024 [US1] Native terminal wiring with `internal/ptytest` in `cmd/whr/sandbox_shell_pty_test.go` against a fake client: direct
  streams, resize reaches the child, non-terminal refused before any provider call, client environment canaries. Failing tests
  first: `TestSandboxShellPTYDirectStreamsAndResize`, `TestSandboxShellPTYRejectsNonTerminalBeforeProvider`,
  `TestSandboxShellPTYEnvironmentCanaries`, run on Linux and macOS back ends. Fake-client resize is not evidence for FR-012 (that is
  T025). Traces FR-007, FR-010, FR-012 (wiring only), FR-009; S12-C6; SC-006. Prerequisites: T023.

## Stage 4: acceptance

- [ ] T025 OWNER GATE (OG-1), real host, not CI: SC-010 runbook ([quickstart.md](quickstart.md#sc-010-owner-gate)) on a real
  Docker host with a terminal; record exact product SHA, OS, architecture, Docker and terminal versions and the owner's acceptance
  link in `docs/quality/012-container-interactive-shell/sc010.md`: a shell starts and its status passes through; resize reaches
  the pty; suspend, resume and interrupt act in the container; stopping the container from another terminal gives the
  `container_lost` row; a killed client with the shell running gives the `end_undetermined` row; no key sequence detaches; the
  environment route does not expose values in the process list. Also observe, outside SC-010's pass condition, the terminal state after a killed client (plan risk, open question 5). Failure of the
  resize or signal checks returns to the owner (FR-012 says `whr` adds handling only if the check fails; that is a new decision).
  Traces SC-010, FR-011, FR-012, FR-013, FR-016, FR-009. Prerequisites: T016, T017, T023.
- [ ] T026 OWNER GATE (OG-8, as it edits `runtime-provider.md`; it also edits Feature 011 text): flip the container provider report to `interactive_shell=pty`, status `enforced` for each supported profile
  only when S12-C1 to S12-C8 pass (T015, T016, T017, T018) and T025 evidence is recorded; land the Feature 011 text change (owner decision 1 and FR-003: the value becomes `pty` with status `enforced`)
  and the `runtime-provider.md` capability row in the same reviewed step. Failing test first: `TestCapabilityReportPtyAfterGate`
  (the switch needs the recorded evidence reference; the `none` default test of T018 stays for any profile without it). Traces
  FR-003, SC-002. Prerequisites: T015 to T018, T022, T025.
- [ ] T027 Acceptance and handoff: evidence table in `plan.md` completed with test names and revisions; the coverage table below
  checked with cited evidence; `python3 scripts/check_repo.py`, `git diff --check` and the Go tests recorded; frozen-path diffs
  (`contracts/cli/`, `docs/adr/`, Feature 006, 008, 010, 011 text) empty for every PR of T004 to T018 and T024; the held items
  (T014, FR-024, SC-013) and anything unmet listed as such. Independent exact-head review CLEAR. Prerequisites: T001 to T026
  (T014 may remain open and is then listed as deferred).

## Dependencies and execution

Stage 0 starts after approval with T001; T002 and T003 follow T001 and may run in parallel. Stage 1: T004, T005 and T006 can start
together (T004 needs 008/T001, T002 and the gated T022 (a) only; T005 needs 008/T003; T006 needs 008/T006, 008/T007 and 008/T011); T007 follows T004 and T006; T008 follows T004 and
T005; T009 is independent of them (needs 006/T025 and T031); T010 follows T008 and T009; T011 follows T007; T012 follows T005, T007
to T011; T013 follows T012; T014 follows T005 and T002; T015 follows T004, T007, T008, T010, T012, T013. Stage 2 needs 008/T013
and the T002 record. Stage 3 amendments T019 to T022 depend only on T003 (T019 also on 008/T017) and are owner gates. T023 needs
T012, T013 and T019 to T021 plus 008/T016. Stage 4 follows. Owner gates: real Docker (OG-1: none of T004 to T015, T016 to T018,
T023, T024 may use one), `contracts/cli/*`, ADR 0013, ADR 0014 (OG-4), Feature 006 text (OG-6), Feature 008 text (OG-8).
T019 to T023 and T026 (T022 (a) is the conditional contract append) are the only exceptions to the empty frozen-path diff.

| Task | Prerequisites |
| --- | --- |
| T001 | none |
| T002 | T001 |
| T003 | T001 |
| T004 | 008/T001, 008/T002, T022 (a) |
| T005 | 008/T003 |
| T006 | 008/T006, 008/T007, 008/T011 |
| T007 | T004, T006, 008/T008 |
| T008 | T004, T005, 008/T003, 009 selection function (PR #299) |
| T009 | 006/T025, 006/T031 |
| T010 | T008, T009 |
| T011 | T007 |
| T012 | T005, T007, T008, T009, T010, T011 |
| T013 | T005, T012, 008/T004 |
| T014 | T005, T002 |
| T015 | T004, T007, T008, T010, T012, T013, 008/T008, 008/T009 |
| T016 | 008/T013, T007, T008, T015, T002 |
| T017 | T013, T016, T002 |
| T018 | T015, 008/T013 |
| T019 | T003, 008/T017 |
| T020 | T003 |
| T021 | T003 |
| T022 | T003 |
| T023 | T012, T013, T019, T020, T021, 008/T004, 008/T016 |
| T024 | T023 |
| T025 | T016, T017, T023 |
| T026 | T015, T016, T017, T018, T022, T025 |
| T027 | T001 to T026 |

## Coverage

Every requirement, success criterion and decision maps to at least one task, or is explicitly deferred with its reason.

| Requirement | Tasks | Note |
| --- | --- | --- |
| FR-001 command, no short form, not registered by approval alone | T019, T023 | registration is owner-gated |
| FR-002 selector | T008, T023 | |
| FR-003 capability claim | T015, T016, T017, T018, T025, T026 | stays `none` until T026 |
| FR-004 admission | T004, T022 | contract edit only in gated T022 (a) |
| FR-005 order and state | T005, T008, T022 | |
| FR-006 ownership | T008, T016 | |
| FR-007 terminal required, accepted options | T008, T009, T023, T024 | `--container-endpoint` accepted (owner decision D9); malformed value exit 64 tested in T008 |
| FR-008 exec form | T006, T007, T016 | V2 via T002 |
| FR-009 environment | T007, T016, T025 | V3 via T002 |
| FR-010 notice | T011, T023, T024 | |
| FR-011 terminal safety | T011, T021, T023, T025 | no filtering; live part is T025 |
| FR-012 resize | T002 (V1), T024 (wiring), T025 | whr-side resize handling deferred: only if T025 fails, a new owner decision |
| FR-013 signals | T012, T002 (V4), T025 | |
| FR-014 state and record | T005, T012, T013, T022 | |
| FR-015 exit codes | T009, T010, T012, T013, T020, T021, T023 | |
| FR-016 end of session | T013, T017, T025 | |
| FR-017 killed client, orphans | T005, T013 | no reaper by design (spec) |
| FR-018 no widening | T007, T008, T016 | |
| FR-019 credentials | T007, T016 | |
| FR-020 no agent launcher | T007 | |
| FR-021 profiles | T007 (SC-003 fixtures) | scope statement, no separate test (SC-011) |
| FR-022 neutrality | T004, T011, T015 | vendor-name gate list of Feature 011 FR-022 not extended (spec) |
| FR-023 confirmation | T010 | |
| FR-024 reconciliation | T014 | held on V5; not a gate for FR-003; stays under "Not enforced" until built |
| SC-001 | T004 | |
| SC-002 | T018, T026 | |
| SC-003 | T007, T016 | |
| SC-004 | T008, T016 | |
| SC-005 | T005, T008 | |
| SC-006 | T008, T009, T024 | |
| SC-007 | T013, T020, T023 | |
| SC-008 | T012, T013, T017 | |
| SC-009 | T015 | |
| SC-010 | T002, T025 | owner gate |
| SC-011 | T011, T023, T025 | per the SC-011 mapping of the spec |
| SC-012 | T010 | |
| SC-013 | T014 | claimed only after V5 |
| D1 | T010 | |
| D2 | T007, T013 | |
| D3 | T006, T007 | |
| D4 | T008 | |
| D5 | T005, T008 | |
| D6 | T011, T021 | |
| D7 | T014 | |
| D8 (a), (b), (c), (d) | T013, T012, T005, T013 | |
| D9 | T008, T009, T023 | `--container-endpoint` on `sandbox shell` |
| S12-C1 to S12-C9 | C1 T004; C2, C3 T007, T016; C4 T008; C5 T005, T008; C6 T008, T009, T013; C7 T012, T013, T017; C8 T010; C9 T014; all in T015 | |
