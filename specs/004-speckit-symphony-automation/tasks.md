# Tasks: Spec Kit and Symphony task automation

**Input**: [spec.md](spec.md), [plan.md](plan.md), [research.md](research.md),
[data-model.md](data-model.md), [contracts/cli.md](contracts/cli.md),
[quickstart.md](quickstart.md).
**Status**: Task proposal for independent analysis and owner review; all tasks pending.
Generation does not authorize implementation, issues, eligibility, publication or runtime activation.
**Authority**: Approved specification content at `8f4debe5cd2ee2770f5963d292a9eb60af6270d7`;
owner-accepted plan source `20c17b3ac90a232643d7c3998498465a7008318a`
([PR 38](https://github.com/wstein/workharbor-nxt/pull/38)). Historical status/next-gate
text in those reviewed artifacts is preserved; this file records the later task-generation stage.
**Tests**: Required by FR-012 and SC-002–006. New operator tests live under `scripts/`,
following the bounded automation convention, because the frozen Feature 003 verifiers
collect and pin the entire root `tests/` inventory. The plan's proposed `tests/test_task_*.py`
touch points are adapted only in location; do not modify frozen tests/verifiers or
claim their known red suite is green. Future operator discovery is
`python3 -m unittest discover -s scripts -p 'test_task_*.py' -v`.

## Existing work and external prerequisites

The four bounded corrections already have implementation or guidance below. They are
reuse inputs, not new implementation tasks or completed Feature 004 stories.
[PR 40](https://github.com/wstein/workharbor-nxt/pull/40) candidate
`af8926f77b504a11ab79ceecc0f7125a65ba8a9b` is reviewed/published but **not owner-merged**
at this draft's base. Review/CI evidence on that candidate does not establish acceptance,
provider authentication, live guard capability or full-suite green. If its candidate or
base changes, revalidate the final accepted revision rather than inheriting this pin.

| Existing bounded correction | Exact implementation / evidence | Remaining reuse boundary |
| --- | --- | --- |
| Separate proposal scope from actual synthetic integration; handle trusted base advancement and protected prerequisites | `643d292` and `3fe43da`/`99f220c`, `scripts/check_ci.py`, `scripts/test_check_ci.py`; integration `ff701be` | Generic package/source adapter and reviewed rebase correspondence remain; changed protected prerequisites still fail closed |
| Prepare an actual pinned merge before separately authorized publication | `bd7858a`, documentation `b1ae007`, `scripts/prepare_publication.py`, `scripts/test_prepare_publication.py`; integration `02b0293` | Handoff must call this tool, separately recheck refs before writing and reconcile uncertain publication |
| Bind approval to guard-validated protected content separately from source snapshot | `795dc37`, hardening `37fb3b3`/`53c5753`, `scripts/approval_binding.py`, `scripts/test_approval_binding.py`; integration `301ee52` | Real Feature 003 context/comparator and authenticated authority ports remain unavailable; no legacy fallback |
| Bound readiness/review/publication guidance and trusted operator CI validation | `bca5138`, `9df610f`, adoption `d1e24e9` and advanced-base repair `af8926f`, `AGENTS.md`, `WORKFLOW.md`, `docs/development/harness.md`; integrations `5ecd030`/`7029d1d` | Full package/outcome flow and runtime/tracker/handoff integration remain; do not broaden the existing exact operator inventory silently |

| Prerequisite | Required evidence / effect |
| --- | --- |
| P40: owner accepts the final reviewed PR 40 correction revision | Record accepted commit, exact-candidate review and actual checks in this file's appended evidence. Blocks reuse changes T018, T019, T023, T025, T038 and T043, not independent offline schema/fixture work. Do not merge PR 40 as a task action. If unavailable, stop affected tasks without recreating its tools. |
| G-entry: Feature 003 context, dependencies, approval comparison and current/index/history scope APIs | Demonstrated implementation revision and targeted acceptance evidence; blocks real T026/T029 entry, not fixtures. Legacy begin/state primitives are insufficient. |
| G-complete / G-resume: Feature 003 acceptance/content/review seal, commit binding and durable recovery/rework | Demonstrated interfaces and evidence; blocks real T036/T037/T040 and live readiness for executions requiring them. No Feature 003 task is authorized here. |
| L-live: trusted owner IDs/approval retrieval, actual runtime/build/workflow, host isolation, publication permission and safe labels | Safe full-set replacement needs supported conditional operation or verified serialization of **all** possible writers. GitHub alone currently reports false. T029/T047/T048 remain blocked until their relevant live capabilities and separate owner authorization exist. |

Use full resolved commit IDs in implementation evidence; short IDs above identify
existing local history, not new approval assertions. Preserve Features 001–003,
frozen acceptance contracts, runtime pin and product boundaries byte-for-byte.

## Format and execution rules

Every task uses `- [ ] Tnnn [P?] [USn?] description with paths`. `[P]` means disjoint
files after listed prerequisites; it never permits shared-checkout editors or bypasses
a gate. Story tasks carry their story label. New test tasks precede their implementation;
record intended initial failures separately from import/setup failures. Each future issue
must select one task, approved paths and exact acceptance argv under the existing workflow.
Missing capabilities remain blockers, not work implicitly delegated to another feature.

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Establish exact reusable inputs and disposable offline fixtures.

- [ ] T001 Record the accepted plan/spec identities, P40 acceptance or pending status, available guard interfaces and frozen Feature 003 baseline inventory in appended evidence in `specs/004-speckit-symphony-automation/tasks.md`; verify actual objects/checks without executing prerequisites, refreshing pins, or changing task semantics.
- [ ] T002 Create synthetic documentation, reviewed test-authoring and implementation-successor fixture inputs in `tests/fixtures/task-automation/documentation.json`, `tests/fixtures/task-automation/test-authoring.json`, `tests/fixtures/task-automation/implementation.json` and their safe case/evidence manifests in `tests/fixtures/task-automation/README.md`; use synthetic Feature 003 contexts and explicit fixture assurance, never real feature task mutations.

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Safe bounded local data, ownership and capability plumbing for all stories.
**Independent gate**: Disposable fixtures reject unsafe inputs, preserve frozen artifacts,
and can report structural results with execution unavailable. No external services.

- [ ] T003 [P] Add bounded input/output rejection tests in `scripts/test_task_package.py` for duplicate/unknown/missing keys, nonfinite numbers, invalid UTF-8, oversized values, escaping/symlink/hardlink/nonregular paths, concurrent replacement and secret sentinels; assert no task prose, check, credential lookup or network operation executes (depends T002).
- [ ] T004 Implement safe JSON/path primitives in `scripts/task_package.py`, reusing `scripts/single_task_state.py` traversal/durability primitives where applicable without modifying guard state; enforce “strict UTF-8 JSON, reject duplicate/unknown/missing keys and nonfinite numbers”, “repository-relative regular files, without symlink components, escapes or gitlinks” and “Full Git IDs resolve locally; never implicitly fetch arbitrary input references” (depends T003).
- [ ] T005 [P] Add journal/ownership primitive tests in `scripts/test_task_handoff.py` for process crashes, competing commands/checkouts/hosts, unsafe journal locations and unknown ownership; require retained evidence and zero reset/adoption (depends T002).
- [ ] T006 Implement the separate ignored per-issue SQLite journal and ownership port in `scripts/task_handoff.py`; key it by “repository/issue/package digest/guard attempt ID”, “Append events with monotonic sequence and durable intents/results”, hold a local process lock and require trusted cross-host ownership evidence before eligibility; never edit/substitute Feature 003's journal (depends T005).
- [ ] T007 Define host-injected guard/tracker/runtime capability ports and unavailable defaults in `scripts/task_package.py`; require “availability, implementation revision, fixture result and live observation reference when required”, derive capabilities per action, and distinguish structural preparation from `execution_eligible: false` without loading adapters from package text (depends T004, T006).

**Checkpoint**: Safe offline foundation; no demonstrated real execution capability is implied.

## Phase 3: User Story 1 — Prepare one reviewable task package (P1, MVP)

**Goal**: Prepare/validate one immutable package and concise approval summary without
checks, prerequisite execution, credentials, network or tracker mutation.
**Independent test**: Three synthetic packages share one validator; repeated preparation
reuses identity/summary; missing prerequisites and invalid matrices give precise blockers.
A reviewed assertion matrix retains raw reds; default implementation requires all pass.

- [ ] T008 [P] [US1] Add preparation/validation CLI contract tests in `scripts/test_task_automation.py` covering exactly one task, all required envelope fields/exit classes, repeat inputs, changed inputs, missing prerequisites, safe diagnostics and explicit denial of network/check/tracker access (depends T007).
- [ ] T009 [P] [US1] Add exact-outcome tests in `scripts/test_task_outcomes.py` for all three fixtures and missing/extra/duplicate cases, empty suites, setup/import/cleanup/subtest failures, skips/xfails/timeouts, unexpected green/red, red-fingerprint mismatch, mutation and baseline regressions (depends T007).
- [ ] T010 [US1] Implement immutable TaskPackage fields in `scripts/task_package.py`: “schema_version: 1”, trusted repository, “issue (repo/number or null for fixtures)”, “unique task_id”, exact opaque Feature 003 context, source/execution identities, “unique ordered check IDs, exact collected case IDs” and classification “pass” or reviewed in-test “assertion”; retain distinct spec/plan/execution/matrix/rework references and reject omitted/extra fields (depends T008).
- [ ] T011 [US1] Implement canonical package identity and immutable output reuse in `scripts/task_package.py`: “sorted keys, compact separators, ensure_ascii=False, no trailing newline”; hash immutable fields except `approval_refs`, retain the guard's excluded self-digest, exclude “Timestamps/local output paths/random attempt IDs”, and require “changed bytes create a new revision, never overwrite” (depends T010).
- [ ] T012 [US1] Implement the reusable collected-case runner in `scripts/task_outcomes.py` using strict argv and bounded deadlines; validate exactly the reviewed inventory/classifications/fingerprints/tool-input revision/raw exit, keep raw outcomes separate, default all checks to pass and expose an approved zero-exit matrix check argv to Feature 003 without copying frozen verifier/task exceptions (depends T009, T011).
- [ ] T013 [US1] Implement side-effect-free `task_package.validate(package, repository_snapshot, evidence, capabilities)` in `scripts/task_package.py`, delegating context/comparison/scope/acceptance to injected Feature 003 ports; report unmet declared prerequisites without running them and reject unavailable backends rather than assuming validation success (depends T011, T012).
- [ ] T014 [US1] Implement `prepare --input FILE --output DIR` and `validate --package FILE --offline` in `scripts/task_automation.py`; preparation never runs checks/prerequisites and offline validation never contacts runtime/tracker; enforce “1 MiB input, 100 checks, 10,000 cases, positive check deadlines, three transport retries” without truncation (depends T013).
- [ ] T015 [US1] Implement inert `effective --package FILE --offline` preview in `scripts/task_automation.py` using `scripts/task_package.py`; expose expected source/runtime/workflow/mode/checks/actions, missing capabilities and safe next actions with no activation or ready mutation (depends T014).
- [ ] T016 [US1] Produce one concise deterministic approval summary per revision in `scripts/task_package.py`; include reviewed identities/durable references, distinct spec/plan/execution/review/publication decisions, missing decisions and next action; offline approvals stay “supplied-local” and cannot authorize execution (depends T015).
- [ ] T017 [US1] Complete sanitized JSON/error handling in `scripts/task_automation.py`: missing evidence “stays null/false”, never echo raw argv/exceptions/test output/secrets, use contract exits 0/2/3/4/5/6, and expose raw red exits separately even on matched matrices (depends T016).
- [ ] T018 [US1] Add the shared package/outcome validator as an explicit additional credential-free validation entry in `scripts/check_ci.py` and regression coverage in `scripts/test_check_ci.py`, using independently trusted reviewed validator/policy pins and host isolation; preserve existing sealed gates and frozen inventories, execute only revision-approved argv, and require separate owner adoption for any new trusted pin rather than a task-specific red exception (depends P40, T017).
- [ ] T019 [US1] Run the three-fixture offline MVP and existing bounded operator regressions, recording exact revisions/commands/raw exits and no-network/no-mutation evidence in `specs/004-speckit-symphony-automation/quickstart.md`; retain raw `make check` red status and distinguish fixture approval from authentication (depends P40, T018).

**Checkpoint**: US1 may be delivered independently; all unavailable real capabilities remain explicit.

## Phase 4: User Story 2 — Queue only the approved effective execution (P1)

**Goal**: Validate content approval, actual execution and all needed capabilities before
making exactly one separately authorized issue eligible.
**Independent test**: Fake guard/runtime/tracker ports accept valid readiness and reject
semantic drift, unauthenticated approvals, unavailable pins/capabilities and source
mismatches before a write. Checkbox/verified EOF evidence changes retain approval only
through demonstrated guard comparison. GitHub without safe labels never queues.

- [ ] T020 [P] [US2] Add package source tests in `scripts/test_task_source.py` for accepted/proposal/synthetic separation, independent base advancement, affected prerequisites, missing history, commit/revert, dirty index/worktree, modes/types, wrong parent order, merge conflicts/tree tampering and reviewed rebase correspondence (depends T019).
- [ ] T021 [P] [US2] Add approval/readiness/runtime integration tests in `scripts/test_task_readiness.py` for fixture versus authenticated assurance, progress-only versus semantic changes, original approval identity retention, stale/deleted/dismissed approval objects, unavailable guard/runtime/labels and before-entry drift; assert zero eligibility writes on every invalid case (depends T019).
- [ ] T022 [P] [US2] Add authenticated tracker-port tests in `scripts/test_task_tracker.py` for repository/actor/action/revision/content binding, changed approval objects, denied external actions, three bounded transport retries and credential/sentinel exclusion from files/logs/children (depends T019).
- [ ] T023 [US2] Implement source snapshot adaptation in `scripts/task_source.py`, reusing the adopted `scripts/check_ci.py` proposal/integration checks and `scripts/prepare_publication.py` merge preparation rather than reimplementing them; record accepted/base/head/wrapper identities separately, inspect every intervening commit, revalidate affected protected prerequisites and block unavailable evidence (depends P40, T020).
- [ ] T024 [US2] Implement explicit rebase-equivalence evidence validation in `scripts/task_source.py` for old/new commits and parents, independently verified patch correspondence, protected task content and complete resulting tree; require fresh exact-candidate review and renewed affected approval for conflicts/overlap/changed patches, never silently refresh pins or accept range-diff alone (depends T023).
- [ ] T025 [US2] Connect package approval references to the existing `scripts/approval_binding.py` adapter from `scripts/task_package.py`; retain original approval revision/package/binding and separate current raw snapshot/comparison, accept only “identical” or “progress-only”, and reject “renewal-required” or “ambiguous” without adding a whitespace/section normalizer (depends P40, T021, T024).
- [ ] T026 [US2] Implement the Feature 003 context/dependency/comparison/scope adapter in `scripts/task_package.py`; call demonstrated interfaces with fresh evidence and use unavailable defaults until G-entry is evidenced, never copy its schema/comparator or fall back to legacy begin (depends T025; real integration additionally G-entry).
- [ ] T027 [US2] Implement the narrow host-side approval/issue GitHub port in `scripts/task_tracker.py`; authenticate immutable configured owner/provider IDs and exact repository/feature/task/action/original revision/content binding, store object digest/state/times/verifier identity, refetch at readiness and keep spec/plan/execution/matrix/rework/review/publication decisions distinct (depends T022, T025).
- [ ] T028 [US2] Add actual Symphony `be10a1b79df723d6d7612b5651c8522704dafb2e` source/build provenance, workflow bytes and non-secret effective-configuration observation to `scripts/prepare_symphony.py`, with `scripts/test_task_runtime.py`; compare guard/runtime/settings/argv/action evidence at readiness and entry, and state that rendering is inert and host isolation/secret behavior requires live observation (depends T021, T026, T027).
- [ ] T029 [US2] Implement `ready --package FILE --live-config FILE` in `scripts/task_automation.py`; validate all capabilities of the entire approved execution, ownership from `scripts/task_handoff.py`, authenticated approvals, effective source/runtime/checks and approved external actions before one selected issue's eligibility write; current GitHub safe-label=false blocks with remediation and leaves unrelated issues unchanged (depends T024, T026–T028; real eligibility additionally G-entry, G-complete/G-resume as requested and L-live).
- [ ] T030 [US2] Revalidate effective execution at entry and after checks through `scripts/task_package.py`, using guard-owned mode/scope/acceptance and immutable identities; reject drift without baseline adoption or reset, preserve dirty retries for guard recovery, and require renewed approval for runtime/workflow/argv changes (depends T029).
- [ ] T031 [US2] Update `WORKFLOW.md` and `docs/development/symphony.md` to invoke approved package/effective/readiness gates while retaining one-task execution, owner authority, unavailable-capability blockers, existing concurrency and final-label ordering; document operator recovery without activating runtime or adding a fork (depends T030).
- [ ] T032 [US2] Record the offline readiness rejection matrix, actual capability gaps and exact raw outcomes in `specs/004-speckit-symphony-automation/quickstart.md`; distinguish fake safe-label/approval results from provider capability/live observation and leave unavailable live gates blocked (depends T031).

**Checkpoint**: US2 offline readiness is testable independently with ports; live eligibility remains conditional.

## Phase 5: User Story 3 — Resume and finish one handoff (P1)

**Goal**: Reconcile interrupted attempts and reuse one implementation/commit/PR/evidence,
finishing durable evidence before one safe final-label operation.
**Independent test**: Disposable attempts interrupted before/after preparation,
completion, commit binding, PR/evidence writes and labels preserve one implementation
identity and PR. Failed revalidation blocks without reopening. Lost responses and
concurrent labels reconcile safely or remain explicitly incomplete.

- [ ] T033 [P] [US3] Extend crash/ownership/uncertain-write tests in `scripts/test_task_handoff.py` across every state boundary and competing checkout/host, including ambiguous PR/evidence matches, changed operation inputs, corrupt/missing completion evidence and failed handoff-only revalidation; assert no duplicate implementation/reset/unauthorized rework (depends T032).
- [ ] T034 [P] [US3] Extend tracker tests in `scripts/test_task_tracker.py` for stable publication/evidence keys, ref drift, lost responses, conditional label conflicts, unrelated-label preservation, unsupported GitHub no-write and immediate agent stop after ready removal; assert recovery evidence precedes blocked transitions (depends T032).
- [ ] T035 [US3] Extend `scripts/task_handoff.py` with “prepared → validated-offline → ready → guard-active → completed-awaiting-handoff → reviewed → published → evidence-durable → final-label-intent → handed-off” derivation, durable intent/result events, bound guard implementation identity and separate verification HEAD; changed inputs conflict, failure appends blockers without reopening (depends T033).
- [ ] T036 [US3] Implement guard-backed `resume` and authorized-rework dispatch in `scripts/task_automation.py` using `scripts/task_handoff.py`; derive mode from demonstrated Feature 003 inspect/resume/status/rework APIs, preserve immutable completion/commit history, and block missing/corrupt evidence or failed completed-task revalidation until explicit owner corrective scope/checks link the prior implementation (depends T035; real integration additionally G-complete/G-resume).
- [ ] T037 [US3] Implement fresh handoff verification in `scripts/task_handoff.py`: rerun guard scope/acceptance against bound implementation, validate commit binding and exact-candidate independent review plus publication authorization, and persist evidence before external actions; unavailable APIs block and cannot be replaced by local checkmarks (depends T036; real integration additionally G-complete/G-resume).
- [ ] T038 [US3] Implement idempotent authorized PR/evidence publication in `scripts/task_tracker.py`, called by `scripts/task_handoff.py`; reuse exact task/attempt/bound-commit matches, invoke existing prepublication preparation with trusted checker pins, recheck base/head immediately before a write, journal intents and query remote identity/content after uncertainty before bounded retry; multiple matches block (depends P40, T034, T037).
- [ ] T039 [US3] Implement final-label handling in `scripts/task_handoff.py` and `scripts/task_tracker.py`: persist observed set/version, desired set, evidence digest and recovery instructions before the single provider operation; preserve unrelated labels, remove ready, add human-review/blocked and leave issue open; conflict rereads/recomputes, unsupported provider does not write, unknown/failed results stay incomplete without any required later agent action (depends T038).
- [ ] T040 [US3] Implement `handoff` and complete offline/live `resume` routing in `scripts/task_automation.py`; enforce check → commit binding → exact review → authorized publication → durable evidence → final label order, return uncertainty/incomplete operator recovery accurately and never merge/release/deploy (depends T039; real actions additionally relevant G/L-live evidence and authorization).
- [ ] T041 [US3] Document retained-attempt and unknown-final-write operator reconciliation in `docs/development/symphony.md`, including failed revalidation, ownership ambiguity and owner-authorized rework; preserve existing approval/review/implementation evidence and never prescribe journal deletion, reset or invented clean baselines (depends T040).
- [ ] T042 [US3] Run all offline interruptions/concurrency/uncertainty scenarios and append exact check/revision/mutation evidence to `specs/004-speckit-symphony-automation/quickstart.md`, proving SC-003/SC-005 offline with supplied ports while explicitly withholding live/unattended claims (depends T041).

**Checkpoint**: Full offline flow is reproducible. Live final labels remain blocked without demonstrated capability.

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Verify reusable boundaries, provide an honest runbook and leave live proof gated.

- [ ] T043 Review and finalize shared CI/package trust integration in `scripts/check_ci.py`, `scripts/test_check_ci.py` and `docs/development/harness.md`; prove three packages use one flow with independent validator/policy provenance, strict additional tests and no new per-task exceptions, preserve existing accepted gates and require renewed owner adoption for changed policy inventory (depends P40, T019, T032, T042).
- [ ] T044 Run all new operator tests, adopted bounded operator regressions, frozen Feature 003 verifiers, repository/diff/shell/prerequisite checks and raw `make check`; record commands/versions/exits and unchanged Features 001–003 evidence in `specs/004-speckit-symphony-automation/quickstart.md`, rejecting skips/missing tools and never representing known raw exit 2 as passing (depends T043).
- [ ] T045 Update `docs/quality.md` and `docs/development/symphony.md` with implemented versus fixture-verified versus blocked live capabilities and exact durable evidence, confirming no product AI dependency/runtime fork or implicit Features 001–003 execution (depends T044).
- [ ] T046 Implement AdministrationObservation capture/validation in `scripts/task_handoff.py` with `scripts/test_task_measurement.py`; retain “Sample/task/category, package/source/runtime/tool revisions, interval boundaries, active administration seconds, explicit implementation/computation/waiting exclusions, retry relationship and evidence”, count all preparation/approval/check/configuration/handoff repair and reject missing/selectively omitted samples (depends T042; separate files from T043–T045 except documented handoff ownership).
- [ ] T047 Observe normal completion, interruption after completion before handoff and verified resume on a separately owner-authorized disposable issue/checkout using actual pinned Symphony, recording checkout/task/check/commit/PR identities and attributable approvals/exact review in `specs/004-speckit-symphony-automation/quickstart.md`; prove no duplicate implementation/PR or unrelated labels and keep unknown final writes incomplete for operator reconciliation (depends T045, T046, G-entry, G-complete/G-resume, L-live and separate live authorization; otherwise blocked, never replaced by dry runs).
- [ ] T048 Measure five consecutive eligible tasks including documentation, implementation, test-authoring and interrupted retry, publishing individual revision-bound intervals/exclusions and assessing SC-001's less-than-ten-minute administration target in `specs/004-speckit-symphony-automation/quickstart.md`; update `docs/quality.md` only to the observed result and record misses honestly (depends T047 and separate measurement authorization; estimates/guides/offline runs prove no saving).

## Dependencies & Execution Order

```text
T001 → T002 → (T003 → T004, T005 → T006) → T007
T007 → US1 T008–T019 → US2 T020–T032 → US3 T033–T042
US3 → T043 → T044 → T045 ─┐
US3 → T046 ───────────────┴→ [G/L-live + owner authorization] T047 → T048
P40 → T018, T019, T023, T025, T038, T043
```

The per-task dependencies are authoritative; numeric ranges above summarize phases,
not permission to run intervening tasks implicitly. All stories are P1 in the approved
specification; use its listed order. US2 consumes US1 packages and US3 consumes US2
validation, while each has an independent disposable-port acceptance test. Do not
claim cross-story production independence or a blanket all-Feature-003 dependency.

Within a phase, finish test authoring before the associated implementation, then run
its targeted checks and capture exact outcomes. T018/T043 can add validation but cannot
authenticate their own policy adoption. Author/reviewer approval of this task proposal
remains separate from execution approval, implementation acceptance and publication.

## Parallel Examples

After T007, US1's T008 (`scripts/test_task_automation.py`) and T009
(`scripts/test_task_outcomes.py`) can run in separate author checkouts. T010–T017 share
package/CLI files and run sequentially. Foundational T003 and T005 are similarly disjoint.

After T019, US2's T020 (`scripts/test_task_source.py`), T021
(`scripts/test_task_readiness.py`) and T022 (`scripts/test_task_tracker.py`) can run
in separate checkouts; integrate reviewed tests before implementation. Source work and
tracker implementation can overlap only after their stated dependencies and with no
shared-file edits; the package/CLI integrations stay serial.

After T032, US3's T033 (`scripts/test_task_handoff.py`) and T034
(`scripts/test_task_tracker.py`) can run separately. T035–T040 share journal/tracker/CLI
integration and stay serial. T046 can follow T042 while T043–T045 proceed in disjoint
files, with separate checkouts and a fresh final combined check after integration.

## Implementation Strategy

1. Obtain independent analysis and owner approval of this exact task proposal; create
   one-task issues only under separate authorization. Resolve P40 only through normal
   owner acceptance, then record its final accepted revision/evidence.
2. Deliver safe foundations and US1 as the offline MVP. Synthetic ports prove reusable
   preparation/outcome behavior; absent Feature 003/provider capabilities remain blocked.
3. Add US2's source/approval/effective/readiness gates and US3's durable reconciliation
   in order, using the four existing corrections. Demonstrate only the guard capabilities
   needed for each action; do not implement another feature's tasks here.
4. Finish full offline rejection/recovery and CI evidence. No task-specific exception,
   check weakening, review transfer, queue change or live claim follows from fixture success.
5. Stop at missing real guard/runtime/authentication/safe-label/publication evidence.
   Only separate owner authorization and demonstrated live gates permit T047/T048.
   Report SC-001/SC-006 as unverified until actual observations exist.

## Coverage and completion evidence

| Requirement / criterion | Primary tasks |
| --- | --- |
| FR-001–004; SC-004 | T002–T004, T008–T019, T025–T027, T043 |
| FR-005–007; SC-002 | T007, T020–T032 |
| FR-008–010; SC-003/SC-005 | T005–T006, T033–T042 |
| FR-011–013 | T003–T007, T017, T022, T026, T028, T031, T043–T045 |
| FR-014; SC-001/SC-006 | T046–T048 |

48 pending tasks: setup 2, foundation 5, US1 12, US2 13, US3 10, final 6.
No existing bounded correction is checked off as full Feature 004 completion.
Append verified completion/review/evidence references below without rewriting approved
requirements/task descriptions/dependencies/order. Semantic changes return to owner review.
