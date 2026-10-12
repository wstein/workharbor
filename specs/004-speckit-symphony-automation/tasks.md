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
**Tests**: Required by FR-012 and SC-002–006. New operator tests use `scripts/`
as the existing tooling convention. The accepted generic runner collects both root
`tests/` and `scripts/test_*.py`; required IDs are a minimum and new green regressions
need no per-task hash update. Preserve existing assertions and historical evidence;
changes to required inventory or retained outcomes need owner review under
[ADR 0007](../../docs/adr/0007-explicit-ci-acceptance.md). Focused future discovery is
`python3 -m unittest discover -s scripts -p 'test_task_*.py' -v`.

## Existing work and external prerequisites

The bounded corrections from [PR 40](https://github.com/wstein/workharbor-nxt/pull/40)
and explicit acceptance policy from [PR 43](https://github.com/wstein/workharbor-nxt/pull/43)
are accepted on this proposal's `main` base
`8300295e47dc4b5b86da7cee7f0da99f8cc3bb70`.
They are reuse inputs, not new implementation tasks or completed Feature 004 stories.
The former PR 40 candidate `af8926f77b504a11ab79ceecc0f7125a65ba8a9b`
is historical provenance, not a pending merge gate or current CI authority. Accepted
repository checks establish neither authenticated provider approval nor live guard
capability; revalidate affected inputs and exact-candidate review after later changes.

| Existing bounded correction | Accepted implementation / evidence | Remaining reuse boundary |
| --- | --- | --- |
| Exact source/checkout identity and actual synthetic integration | `scripts/check_ci.py` `source_context`/`clean_checkout`, `scripts/test_check_ci.py`, accepted source above | Package source/rebase adapter and guard-backed head scope/history remain; current CI does not supply the retired historical scope engine |
| Prepare an actual pinned merge before separately authorized publication | `scripts/prepare_publication.py`, `scripts/test_prepare_publication.py`; exact `--source` checker-code identity | Handoff must call this tool, separately recheck refs before writing and reconcile uncertain publication; a source pin is not owner adoption |
| Bind approval to guard-validated protected content separately from source snapshot | `scripts/approval_binding.py`, `scripts/test_approval_binding.py`, accepted source above | Real Feature 003 context/comparator and authenticated authority ports remain unavailable; no legacy fallback |
| Generic CI collection/outcome validation and review/publication guidance | `scripts/check_ci.py` `collect`/`Outcomes`/`evaluate`, `docs/development/ci-acceptance.json`, `AGENTS.md`, `WORKFLOW.md`, `docs/development/harness.md`; ADR 0007 | Package validator/outcome integration and runtime/tracker/handoff remain; reuse the accepted runner instead of creating CI authority or another classifier |

| Prerequisite | Required evidence / effect |
| --- | --- |
| A-tools: accepted bounded tools and explicit CI policy (satisfied at the source above) | Record and revalidate the actual reused revision/contract in implementation evidence. PR 40/43 need no new merge/adoption task; later semantic changes still require normal owner review. This does not satisfy G/L capabilities or authorize integration/publication. |
| G-entry: Feature 003 context, dependencies, approval comparison and current/index/history scope APIs | Demonstrated implementation revision and targeted acceptance evidence; blocks real T009 head-scope and T010/T013 entry, not fixtures. Legacy begin/state primitives are insufficient. |
| G-complete / G-resume: Feature 003 acceptance/content/review seal, commit binding and durable recovery/rework | Demonstrated interfaces and evidence; blocks real T014–T016 and live readiness for executions requiring them. No Feature 003 task is authorized here. |
| L-live: trusted owner IDs/approval retrieval, actual runtime/build/workflow, host isolation, publication permission and safe labels | Safe full-set replacement needs supported conditional operation or verified serialization of **all** possible writers. GitHub alone currently reports false. Real T013/T019/T020 actions remain blocked until their relevant live capabilities and separate owner authorization exist. |

Use full resolved commit IDs in implementation evidence. Preserve Features 001–003
guards/approval contracts, historical acceptance records, runtime pin and product
boundaries; do not turn qualified CI acceptance into full-green task evidence.

## Format and execution rules

Tasks are cohesive deliverables: write meaningful tests first, implement the bounded
behavior, then return final acceptance/runbook evidence together. Initial intended
failures are development evidence, not separate issue handoffs. `[P]` permits disjoint
files in separate checkouts only after actual input prerequisites. Each future issue
selects one deliverable and freezes its proposed paths/acceptance below under owner
approval; this draft grants no execution authority. Missing guard/provider capabilities
block their actions without preventing disposable fixture development.

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Establish reusable offline inputs and record actual external prerequisites.

- [x] T001 Create the three synthetic package inputs and safe case/evidence manifests in `tests/fixtures/task-automation/documentation.json`, `tests/fixtures/task-automation/test-authoring.json`, `tests/fixtures/task-automation/implementation.json` and `tests/fixtures/task-automation/README.md`, with fixture/network-denial tests in `scripts/test_task_fixtures.py`; record actual plan/spec/A-tools/guard identities and required baseline inventory in the fixture README without running prerequisites or changing real task artifacts.

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Safe local data, separate journal/ownership and unavailable-by-default ports.
**Independent gate**: Fixtures reject unsafe inputs and preserve frozen artifacts without services.

- [x] T002 Implement bounded JSON/path primitives in `scripts/task_package.py` with tests first in `scripts/test_task_package.py`; reuse `scripts/single_task_state.py` traversal/durability primitives without modifying guard state; enforce “strict UTF-8 JSON, reject duplicate/unknown/missing keys and nonfinite numbers”, “repository-relative regular files, without symlink components, escapes or gitlinks” and “Full Git IDs resolve locally; never implicitly fetch arbitrary input references”; reject hardlinks/nonregular files, races and secret disclosure (depends T001).
- [x] T003 [P] Implement the separate ignored per-issue SQLite journal and ownership port in `scripts/task_handoff.py` with crash/concurrency tests first in `scripts/test_task_handoff.py`; key it by “repository/issue/package digest/guard attempt ID”, “Append events with monotonic sequence and durable intents/results”, hold a local process lock and require trusted cross-host ownership evidence before eligibility; preserve unknown ownership and never edit/substitute Feature 003's journal (depends T001; independent of T002/T004).
- [x] T004 Define host-injected guard/tracker/runtime capability ports and unavailable defaults in `scripts/task_package.py` with tests in `scripts/test_task_package.py`; require “availability, implementation revision, fixture result and live observation reference when required”, derive capabilities per action and distinguish structural success from `execution_eligible: false`; never load adapters from package text (depends T002, not T003).

**Checkpoint**: Safe offline data/capability foundation. Durable ownership T003 is required
for readiness/handoff, not a prerequisite for inert package preparation.

## Phase 3: User Story 1 — Prepare one reviewable task package (P1, MVP)

**Goal**: One immutable package and concise approval summary, with reusable offline validation.
**Independent test**: Three synthetic packages use one flow; repeat inputs reuse identity;
invalid prerequisites/matrices block; reviewed assertions retain raw reds and default
implementation requires all pass. Preparation performs no checks/prerequisite/network/tracker work.

- [x] T005 [US1] Deliver the package contract in `scripts/task_package.py` with tests first in `scripts/test_task_package.py`: “schema_version: 1”, trusted repository, “issue (repo/number or null for fixtures)”, “unique task_id”, exact opaque guard context, source/execution identities and distinct spec/plan/execution/matrix/rework references; canonical identity uses “sorted keys, compact separators, ensure_ascii=False, no trailing newline”, hashes immutable fields except `approval_refs`, retains the guard excluded self-digest, excludes “Timestamps/local output paths/random attempt IDs” and requires “changed bytes create a new revision, never overwrite”; implement side-effect-free shared validation delegating context/comparison/scope/acceptance to injected Feature 003 ports and report unmet prerequisites without running them (depends T004).
- [x] T006 [P] [US1] Deliver the shared exact-case runner in `scripts/task_outcomes.py` with tests first in `scripts/test_task_outcomes.py`; reuse accepted `scripts/check_ci.py` `Outcomes`/`evaluate` and collection validation, extracting shared parts and updating that caller only as needed without changing repository acceptance policy. Add package-specific “unique ordered check IDs, exact collected case IDs”, safe fingerprints/tool-input revision/raw exits and bounded approved argv/deadlines; reject missing/extra/duplicate/empty/skipped/xfail/setup/import/cleanup/subtest/timed-out/mutating/unexpected/regressing package outcomes, retain actual test-body classification, default all pass and expose an approved zero-exit matrix argv to Feature 003 without copying task exceptions (depends T001; fixture development is independent of package/CLI integration; no second classifier or CI authority).
- [x] T007 [US1] Deliver `prepare --input FILE --output DIR`, `validate --package FILE --offline` and `effective --package FILE --offline` in `scripts/task_automation.py`, deterministic concise summary/output reuse in `scripts/task_package.py`, and tests first in `scripts/test_task_automation.py`; enforce “1 MiB input, 100 checks, 10,000 cases, positive check deadlines, three transport retries” without truncation, contract exits 0/2/3/4/5/6, missing evidence “stays null/false”, distinct assurance/review/live/raw-exit fields and sanitized diagnostics; preparation never runs checks/prerequisites, offline commands never contact runtime/tracker, preview never activates/queues, and unavailable guards remain ineligible; update `specs/004-speckit-symphony-automation/quickstart.md` to the scripts-based discovery command and record the three-fixture MVP acceptance/runbook with raw reds separate (depends T005, T006; not T003/live capability gates).
- [x] T008 [US1] Integrate T005/T006's shared package validator and outcome entry into accepted `scripts/check_ci.py`, with regression tests in `scripts/test_check_ci.py` and runbook documentation in `docs/development/harness.md`; reuse its collector/classifier and reviewed minimum-ID manifest, require strict new operator tests, preserve exact retained assertions/raw reporting and bind only approved inputs/argv to the package interface. Add no external trust anchor, policy adoption ceremony, historical-tree engine or task-specific red exception; changed policy/required inventory/retained outcomes follow ordinary owner PR review and merge under ADR 0007 (depends T007; accepted A-tools reused; fixture CI tests may start after T005/T006).

**Checkpoint**: T007 is the first runnable offline preparation entry. T008 integrates
that same validator into CI; this remaining package integration reuses existing CI and does not readopt it.

## Phase 4: User Story 2 — Queue only the approved effective execution (P1)

**Goal**: Validate content approval, actual execution and every needed capability before
one separately authorized issue becomes eligible.
**Independent test**: Disposable ports accept valid readiness and reject semantic drift,
unauthenticated approvals, missing pins/capabilities and source mismatches before a
write. Guard-proven checkbox/verified EOF evidence normalization alone retains approval.
GitHub without safe labels never queues. Fixtures do not require live interfaces or CI adoption.

- [ ] T009 [US2] Deliver package source/rebase evidence in `scripts/task_source.py` with tests first in `scripts/test_task_source.py`; reuse accepted `scripts/check_ci.py` `source_context`/`clean_checkout` for exact source/index/worktree/types/modes and `scripts/prepare_publication.py` for actual merge preparation. Preserve accepted/base/head/wrapper identities; delegate package head scope/commit-revert history and protected-prerequisite validation to demonstrated Feature 003 ports, not the removed CI history engine. Validate explicit old/new commits/parents, independently verified patch correspondence, protected task content and complete resulting tree, requiring fresh exact-candidate review and renewed affected approval for conflicts/overlap/changed patches; never silently refresh pins or accept range-diff alone (input T005; accepted A-tools reused; real head-scope calls require G-entry, fixture tests independent of guard/T007/T008).
- [ ] T010 [US2] Deliver content approval and Feature 003 guard bridging in `scripts/task_package.py` with tests first in `scripts/test_task_readiness.py`, calling existing `scripts/approval_binding.py`; preserve original approval revision/package/binding separately from current raw snapshot, accept only “identical” or “progress-only” and reject “renewal-required” or “ambiguous”; delegate context/dependency/comparison/scope to demonstrated guard APIs with unavailable defaults and no copied schema/comparator/legacy fallback (input T005; accepted adapter A-tools; real guard integration G-entry, fixtures need neither).
- [x] T011 [P] [US2] Deliver the host-side authenticated approval/issue tracker port in `scripts/task_tracker.py` with tests first in `scripts/test_task_tracker.py`; bind immutable configured owner/provider IDs to exact repository/feature/task/action/original revision/content binding, store object digest/state/times/verifier identity, refetch at readiness and reject edited/deleted/dismissed/stale records; distinguish “supplied-local” from “authenticated-provider”, keep spec/plan/execution/matrix/rework/review/publication decisions distinct, bound retries and exclude credentials from files/logs/children (depends T005; independent tracker fixtures do not depend on T009/T010/CLI/CI).
- [ ] T012 [P] [US2] Deliver actual Symphony `be10a1b79df723d6d7612b5651c8522704dafb2e` source/build provenance, workflow bytes and non-secret effective-configuration observation in `scripts/prepare_symphony.py` with tests first in `scripts/test_task_runtime.py`; compare guard/runtime/settings/argv/action evidence, distinguish rendering from activation and require actual host isolation/secret-behavior observations for live claims (depends T004/T005; disposable runtime fixtures require no tracker/guard implementation or CI adoption).
- [ ] T013 [US2] Deliver readiness and entry/post-check revalidation in `scripts/task_automation.py` and `scripts/task_package.py` with tests in `scripts/test_task_readiness.py`; require all capabilities of the entire approved execution, ownership, authenticated approvals, effective source/runtime/checks and approved actions before one selected eligibility write; drift blocks without adopting baselines/resetting dirty retries, runtime/workflow/argv changes renew approval and current GitHub safe-label=false performs no write; integrate gates/runbook into `WORKFLOW.md`, `docs/development/symphony.md` and `specs/004-speckit-symphony-automation/quickstart.md`, retaining existing concurrency and one-task/final-action rules (implementation depends T003, T007, T009–T012; early fixture tests only T001/T005; real entry additionally G-entry, G-complete/G-resume as requested and L-live).

**Checkpoint**: Readiness is independently testable with ports; real eligibility remains conditional.

## Phase 5: User Story 3 — Resume and finish one handoff (P1)

**Goal**: Reuse one implementation/commit/PR/evidence across interruptions and finish
checks/review/publication/evidence before one safe final-label operation.
**Independent test**: Interrupt every boundary from preparation through completion,
commit binding, publication/evidence and labels. Preserve one implementation/PR;
failed revalidation never reopens work. Unknown writes/concurrent labels reconcile
safely or stay explicitly incomplete. These tests need fixtures, not a completed runbook/live provider.

- [ ] T014 [US3] Deliver durable progress and guard-backed `resume`/authorized-rework routing in `scripts/task_handoff.py` and `scripts/task_automation.py` with tests first in `scripts/test_task_handoff.py`; derive “prepared → validated-offline → ready → guard-active → completed-awaiting-handoff → reviewed → published → evidence-durable → final-label-intent → handed-off”, retain immutable guard implementation identity and separate verification HEAD, stable intent/result keys and ownership; changed inputs conflict, failed revalidation/missing/corrupt evidence append blockers without reopening, and only explicit owner corrective scope/checks linked to prior implementation permits guard-owned rework (implementation inputs T003/T005/T007; crash/ownership fixtures may start after T001; real integration G-complete/G-resume).
- [ ] T015 [US3] Deliver fresh handoff verification and idempotent authorized PR/evidence publication in `scripts/task_handoff.py` and `scripts/task_tracker.py` with tests in `scripts/test_task_handoff.py` and `scripts/test_task_tracker.py`; require fresh guard scope/acceptance, bound commit, exact-candidate independent review and publication authorization; reuse exact task/attempt/commit PR/evidence matches, call existing prepublication preparation using exact reviewed checker-code pins (identity, not adoption), recheck base/head immediately before writes, persist intents and query exact remote identity/content after uncertainty before bounded retry, blocking ambiguous matches/ref drift (implementation T009–T011/T014; accepted A-tools reused; uncertainty fixtures may start after T001/T005; real actions additionally relevant G/L-live evidence and authorization).
- [ ] T016 [US3] Deliver final-label reconciliation and `handoff` routing in `scripts/task_handoff.py`, `scripts/task_tracker.py` and `scripts/task_automation.py` with tests in `scripts/test_task_handoff.py` and `scripts/test_task_tracker.py`; persist observed set/version, desired set, evidence digest and recovery before one conditional operation/proven serialization, preserve unrelated labels/remove ready/add human-review or blocked/leave issue open, recompute conflicts and never write through an unsupported provider; enforce checks → commit binding → exact review → authorized publication → durable evidence → labels, with unknown/failed final writes incomplete and no required later agent action; include all-boundary interruption/ownership/revalidation/no-duplicate acceptance evidence and operator recovery in `docs/development/symphony.md` and `specs/004-speckit-symphony-automation/quickstart.md`, never prescribing reset/journal deletion/baseline invention (implementation T013/T015; final-label fixtures may start after T001/T005; real operations relevant G/L-live evidence and authorization).

**Checkpoint**: Full offline recovery is reproducible. No merge/release/deploy action
or live final-label capability follows from fixture success.

## Phase 6: Polish & Cross-Cutting Concerns

- [ ] T017 Complete combined feature acceptance and status evidence in `specs/004-speckit-symphony-automation/quickstart.md` and `docs/quality.md`: run canonical `make check` (all new operator suites, retained regression matrix, direct twelve T004 CLI and twelve T003 state cases, repository/diff/shell/prerequisite checks) and separately record raw `make check-raw`; record exact revisions/versions/exits, unchanged Features 001–003 guard/approval and product/runtime boundaries, strict new-test results and implemented versus fixture-verified versus blocked-live status. The current repository baseline qualifies 133 cases with five exact raw assertions; raw 41 cases remain 36 passes/five failures/Make exit 2, not full green. Historical frozen-verifier entry points are records, not acceptance authority (depends T008/T013/T016; combined acceptance, no repeated CI adoption).
- [ ] T018 Deliver AdministrationObservation capture/validation in `scripts/task_handoff.py` with tests first in `scripts/test_task_measurement.py`; retain “Sample/task/category, package/source/runtime/tool revisions, interval boundaries, active administration seconds, explicit implementation/computation/waiting exclusions, retry relationship and evidence”, count all preparation/approval/check/configuration/handoff repair and reject missing/selectively omitted samples (depends T014; independent of docs/CI/provider completion).
- [ ] T019 Observe normal completion, interruption after completion before handoff and verified resume on a separately owner-authorized disposable issue/checkout using actual pinned Symphony, recording checkout/task/check/commit/PR identities and attributable approvals/exact review in `specs/004-speckit-symphony-automation/quickstart.md`; prove no duplicate implementation/PR or unrelated labels and keep unknown final writes incomplete for operator reconciliation (depends T017/T018, G-entry, G-complete/G-resume, L-live and separate live authorization; otherwise blocked, never replaced by dry runs).
- [ ] T020 Measure five consecutive eligible tasks including documentation, implementation, test-authoring and interrupted retry, publishing individual revision-bound intervals/exclusions and assessing SC-001's less-than-ten-minute administration target in `specs/004-speckit-symphony-automation/quickstart.md`; update `docs/quality.md` only to observed results and record misses honestly (depends T019 and separate measurement authorization; estimates/guides/offline runs prove no saving).

## Proposed acceptance contracts and allowed paths

These are reviewable **proposals**, not approved execution contracts or passing evidence.
Before an issue runs, the owner freezes its exact revision, listed paths and argv, plus
conditional G/L evidence and the accepted A-tools source. New deliverable suites finish strictly green; initial
red tests stay inside the deliverable. No new expected-red matrix is invented here.

`U(name)` below is exactly the JSON argv array
`["python3","-m","unittest","discover","-s","scripts","-p",name,"-v"]`.
Use supported Python 3.11+; freeze its absolute executable in the eventual execution
contract if `python3` is older. Each row permits only the implementation/test/doc paths
named in that task; reused guard/bounded helper files are read-only unless named as an
implementation path. Fixture/output journals remain disposable/ignored, not new tracked scope.

| Deliverable | Exact proposed argv | Expected final outcome / prerequisite evidence |
| --- | --- | --- |
| T001 fixtures | `U("test_task_fixtures.py")` | Exit 0; three safe packages/manifests, no network/real feature mutation; record source/A-tools/guard availability, not invented approval |
| T002 safe data; T004 ports; T005 package | `U("test_task_package.py")` | Exit 0 for each implemented increment; strict malformed/path/race rejection, deterministic identity, unmet prerequisites and unavailable ports fail closed; dependencies T001 → T002 → T004 → T005 |
| T003 ownership journal | `U("test_task_handoff.py")` | Exit 0 for implemented journal cases; crash/competing ownership preserved; T001 only |
| T006 outcomes | `U("test_task_outcomes.py")` | Exit 0; complete three-fixture exact matrix, all injected invalid outcomes rejected; raw fixture reds retained with fixture matrix digest, no real-task matrix approval |
| T007 offline entry/MVP | `U("test_task_automation.py")`, `U("test_task_package.py")`, `U("test_task_outcomes.py")` | Each exit 0; three-package CLI/summary reuse, no preparation checks or offline network/tracker mutation, missing guard ineligible; T005/T006 |
| T008 package/CI integration | `["python3","-I","-B","scripts/test_check_ci.py","-v"]`, `["make","check"]` | Each exit 0; package validation shares the accepted collector/classifier, new green suites are discovered without per-task hashes, minimum IDs and exact retained raw assertions remain enforced; T007 and accepted A-tools, no external policy adoption or removed `--self-test` |
| T009 source/rebase | `U("test_task_source.py")` | Exit 0; every invalid history/index/mode/wrapper/ref/equivalence rejected; T005; accepted A-tools; G-entry for real head-scope/history |
| T010 guard/content bridge | `U("test_task_readiness.py")` | Exit 0 for implemented bridge cases; original approval retained only through verified binding/comparison; unavailable default blocks; T005/accepted A-tools; demonstrated G-entry for real calls |
| T011 authentication | `U("test_task_tracker.py")` | Exit 0 for implemented approval/issue cases; fixture assurance distinct, stale/mismatched authority rejected, no secrets; T005; trusted owner/provider evidence for live retrieval |
| T012 runtime | `U("test_task_runtime.py")` | Exit 0; configured text/rendering insufficient, pin/workflow/config drift blocks; T004/T005; real provenance/isolation observations separately required |
| T013 readiness | `U("test_task_readiness.py")`, `U("test_task_automation.py")` | Each exit 0; invalid execution writes nothing, one valid fake issue only, GitHub safe-label=false blocks; T003/T007/T009–T012; relevant G/L evidence before real eligibility |
| T014 resume | `U("test_task_handoff.py")`, `U("test_task_automation.py")` | Each exit 0 for implemented resume cases; one immutable identity, no unauthorized rework/reset; T003/T005/T007; G-complete/G-resume for real execution |
| T015 publication | `U("test_task_handoff.py")`, `U("test_task_tracker.py")` | Each exit 0 for implemented publication cases; fresh binding/review/authorization, stable exact PR/evidence, unknown writes reconciled; T009–T011/T014/accepted A-tools plus actual action authority |
| T016 final handoff | `U("test_task_handoff.py")`, `U("test_task_tracker.py")`, `U("test_task_automation.py")` | Each exit 0; every interruption/write/concurrency boundary, evidence-before-labels, no lost unrelated labels/duplicate PR/required post-label action; T013/T015; live safe-label capability separately required |
| T017 combined acceptance | `["make","check"]`, `["make","check-raw"]` | Canonical exit 0 only when the reviewed repository matrix and all operator/direct/static checks qualify. Current baseline: canonical 133 cases =128 passes/five exact raw assertions; raw 41 =36 passes/five failures, Make exit 2. New green cases increase totals without hash refresh; no raw red or missing check becomes full-green task evidence. T008/T013/T016 |
| T018 measurement model | `U("test_task_measurement.py")` | Exit 0; complete intervals/revisions/exclusions, missing/selective samples rejected; T014 |
| T019 live observations | `["python3","scripts/task_automation.py","resume","--package","PACKAGE.json","--attempt","ATTEMPT","--live-config","LIVE.json"]`, `["python3","scripts/task_automation.py","handoff","--package","PACKAGE.json","--attempt","ATTEMPT","--live-config","LIVE.json"]` | Freeze exact disposable package/attempt/config paths and separately approved normal/interruption sequence before running; success requires attributable actual observations, no duplicate/unrelated mutation, uncertainty remains incomplete. No runnable live contract until T017/T018 and G/L/owner gates |
| T020 measured sample | `U("test_task_measurement.py")` plus approved T019 observation sequence for each eligible sample | Validator exit 0 and five consecutive complete individual records; separately report achieved/missed <10-minute target, never infer it from validator success; T019 and owner-authorized measurement |

T017 uses the canonical runner's direct T003/T004 classes and existing static checks.
Retain historical verifier artifacts/evidence without requiring their frozen entry
points to match current/future implementation. No new phase exception is authorized;
any changed required IDs/retained outcomes need owner-reviewed migration under ADR 0007.

## Dependencies & Execution Order

```text
T001 → T002 → T004 → T005 → T007 → T008 (package integration)
T001 → T006 ─────────────────┘
T001 → T003 ───────────────────────────────┐
T005 → T009 (G-entry scope), T010 (G-entry), T011, T012 ├→ T013
T004 → T012 ───────────────────────────────┘
T003 + T005 + T007 → T014 → T015 (T009–T011) → T016 (T013)
T008 + T013 + T016 → T017 ─┐
T014 → T018 ──────────────┴→ [G/L + owner authorization] T019 → T020
```

All stories are P1; deliver US1 first, then integrate US2/US3. Package/source/approval/
runtime/handoff **fixture test authoring** uses only required synthetic inputs and can
start before CLI/CI/docs/live provider completion. The per-task production dependencies
and external action gates still apply. US2 consumes US1 package data; US3 consumes
validated execution/ownership, while both have independent disposable-port acceptance.
No blanket all-Feature-003 dependency or implicit prerequisite execution is permitted.

## Parallel Examples

- US1: after T001, T003 journal and T006 runner use disjoint implementation/test files;
  package T002/T004/T005 remains serial. T007 integrates package and outcomes; T008
  then adds the package interface to accepted CI without another policy adoption.
- US2: after T005, source T009, tracker T011 and runtime T012 can develop with disposable
  ports in separate checkouts; fixture tests do not wait for real guard or provider access.
  Package bridge T010 shares files with T005 and waits for that contract. T013 integrates
  reviewed outputs and requires ownership; no shared-file parallel edits.
- US3: crash/final-label fixtures can start after T001/T005. Journal T014 and tracker
  work T011 can proceed in separate files once their actual inputs exist. T015/T016
  integrate sequentially; T018 follows T014 in its journal owner checkout and does not
  wait on runbooks/CI. Final combined acceptance follows all integrated changes.

## Implementation Strategy

1. Obtain independent analysis and owner approval of this exact proposal. Freeze one
   deliverable's paths/argv/prerequisites per authorized issue, with tests and final
   evidence together. A-tools is already accepted; later changes follow normal owner
   review and merge, never a prerequisite merge performed by a task.
2. Deliver foundations, package, runner and T007 as the offline MVP. The first new
   fixture deliverable is T001; runnable preparation is T007 and requires T001/T002/
   T004/T005/T006, not an assertion that one missing CLI file enables the whole feature.
3. Reuse the four bounded corrections for CI/source/content/publication, add real
   bridges only when demonstrated and retain fixture-only/ineligible defaults otherwise.
   Finish US2/US3 acceptance/runbooks within their cohesive deliverables.
4. Complete combined offline rejection/recovery evidence. Stop at missing real guard,
   runtime, authenticated approval, safe-label or publication evidence. Separate owner
   authorization plus demonstrated live gates alone permit T019/T020 and SC-006/SC-001 claims.

## Coverage and completion evidence

| Requirement / criterion | Primary deliverables |
| --- | --- |
| FR-001–004; SC-004 | T001–T002, T005–T008, T010–T011 |
| FR-005–007; SC-002 | T004, T009–T013 |
| FR-008–010; SC-003/SC-005 | T003, T014–T016 |
| FR-011–013 | T001–T004, T007–T013, T015–T017 |
| FR-014; SC-001/SC-006 | T018–T020 |

20 pending deliverables: setup 1, foundation 3, US1 4, US2 5, US3 3, final 4.
No existing bounded correction is checked off as full Feature 004 completion.
Append verified completion/review/evidence references below without rewriting approved
requirements/task descriptions/dependencies/order. Semantic changes return to owner review.
