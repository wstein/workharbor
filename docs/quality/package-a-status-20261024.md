# Package A: status reconciliation (2026-10-24)

Audit baseline: main `c71890d27c3d415e50fe15610a6f8f044b313142`.
Scope: Feature 010 T031 documentation, Feature 008 T001–T017 and Feature 009's
15 tasks. This record changes status/evidence only, not requirements, task text,
dependencies or owner decisions. Code and live operations are outside this PR.

## Feature 010: merged packages, open owner gates

[Tasks](../../specs/010-host-and-workspace-panes/tasks.md) and
[implementation evidence](010-host-and-workspace-panes/implementation.md) retain
all package merges, exact accepted revisions and review/CI links.

| Units | Evidence on main | Disposition |
| --- | --- | --- |
| T001–T007 / P1 | #218/#220; `apps/whr/internal/localdocker/{endpoint,client,discovery,filter}.go`, `TestLocalLsListsLabelledContainersAndVolumes`, `TestLocalDockerClientEnvironmentExcludesTokens` | Existing checked units retained. Only discovery is covered, not all 006/T038. |
| T008–T017 / P2 | #221; `apps/whr/internal/localshell/`, `TestLocalShellWorkingDirFallbackWarnsBeforeHandover`, `TestLocalShellDaemonLossExits72`; [P2 evidence](010-host-and-workspace-panes/p2.md) includes native PTY CI | Existing checked units retained; no real Docker/SSH acceptance inferred. |
| T019–T022 / P3 | #222/#224; manifest/schema/scaffold/wizard defaults, `TestManifestMissingConsoleModeDefaultsPlain`, `TestManifestExplicitHerdrPreserved`; [P3 evidence](010-host-and-workspace-panes/p3.md) | Configuration delivered. Runtime SC-007 consumer remains absent. |
| T023–T029 / P4 | #223; `integrations/herdr/`, `test_plugin_title_and_tab_label`, `test_picker_uses_approved_cli_fields`, palette tests; [P4 evidence](010-host-and-workspace-panes/p4.md) | Optional plugin delivered, standalone tests; no installed/live plugin claim. |
| T018 / G-A | [Base host result template](010-host-and-workspace-panes/sc009-base.md) has no owner observations | OPEN / UNVERIFIED; keys, detach, resize, labels and SSH reconnect require owner run. |
| T030 / G-B | [Plugin result template](010-host-and-workspace-panes/sc009-plugin.md) has no owner observations | OPEN / UNVERIFIED; with/without plugin and optional tint need real-host evidence. |
| T031 | Package/decision/status references reconciled here; owner evidence remains absent | OPEN; documentation progress does not satisfy whole-feature closure. |

The nine owner decisions OQ-2/5/6/7/8/9/10/11/12 and foreman OQ-1/4 are unchanged;
OQ-3 remains withdrawn. #216 and #217 already discharged their amendment gates.
No task checkbox in Feature 010 changes.

## Feature 008: no completed implementation tasks

[Task criteria and dependencies](../../specs/008-workspace-sandbox/tasks.md) are
unchanged. Main has none of `apps/whr/internal/runtime/`, `sandbox/` or
`gitprofile/`. Contract JSON and generic command/hostsetup tests do not establish
sandbox implementation. No checkbox changes or issue closures are justified.

| Task / issue | Observed deliverable and reason to remain OPEN |
| --- | --- |
| T001 / #151 | Capability/refusal spec contracts exist; runtime types and `TestRefusalCodesMatchContract` are absent. |
| T002 / #152 | `runtime/admit.go`, `TestAdmitTable` and fail-closed capability tests absent. |
| T003 / #153 | Sandbox identity, state, labels and retention package/tests absent. |
| T004 / #154 | `sandbox.ExitFor` absent; the same-named ADR exit mapping test in hostsetup covers a different feature. |
| T005 / #155 | Git host-only key catalogue exists as a spec contract; profile location/readers/findings and `TestHostOnlyKeysCatalogMatchesContract` absent. |
| T006 / #156 | Developer profile/mount plan and stale-probe/canary tests absent. |
| T007 / #157 | Agent profile/scrubbing implementation and `TestAgentProfileMatchesContract` absent. |
| T008 / #158 | Fake runtime/conformance suite and profile canary tests absent. |
| T009 / #159 | Second fake provider and provider-independence tests absent. |
| T010 / #160 | PARTIAL: OD-5 resolved through Feature 009 by [#168](https://github.com/wstein/workharbor/pull/168), merge `9d297da28887ceee35ed6e5669647f07ffb4f220`. Required amendments file and remaining OD-1/2/4 proposal/approval evidence absent. |
| T011 | Copy policy/tracked-base/dirty/secret/submodule fixture tests absent. |
| T012 | Result export/hashes/hostile-config/symlink tests absent. |
| T013 | Sandbox Docker provider/conformance tests absent. Existing 006 local discovery/removal is a foundation, not this provider. |
| T014 | Sandbox cancellation/retention/deadline/cleanup implementation absent. |
| T015 | Prerequisite implementation and acceptance evidence missing; no Feature 008 final review/runtime acceptance. |
| T016 | No sandbox command group or refusal output field registered; prerequisite implementation and approval gates remain open. |
| T017 / #161 | Sandbox ADR 0014 amendment/approval absent. [#167](https://github.com/wstein/workharbor/pull/167) added host setup, not sandbox. |

Tracker [#162](https://github.com/wstein/workharbor/issues/162) and #151–#161 stay
open; no completion labels are added. #160/#162 still contain historical OD-5
proposal wording; the authoritative resolution is #168, not completion of T010.
[Spec-only #150](https://github.com/wstein/workharbor/pull/150),
[#163](https://github.com/wstein/workharbor/pull/163) and
[#173](https://github.com/wstein/workharbor/pull/173) supply documentation, not
missing runtime implementation. No Feature 008-specific test package exists to
run at this baseline. Its original approval and live-operation gates are unchanged.

## Feature 009: implemented libraries and open integration

[Tasks](../../specs/009-host-setup-wizard/tasks.md) retain their criteria and
dependencies. This reconciliation marks only T001 and T004's delivered library
scope, based on the source/tests below. It does not invent a historical aggregate
task-acceptance record: older #180/#198 descriptions kept completion separate;
this new status assessment is itself subject to the package's independent review
and owner merge. Owner-Mac measurements and live-operation gates are not checked.

| Task | Main evidence / remaining reason | Status |
| --- | --- | --- |
| T001 | [#174](https://github.com/wstein/workharbor/pull/174), [#180](https://github.com/wstein/workharbor/pull/180); `hostsetup/catalogue/{catalogue,production}.go`, `production_test.go: TestInitialCatalogueEntries` proves exact 13 IDs/defaults/four config markers/minimums; `assets/README.md` records owner H40 measured minimums. Loader/schema tests pass. | CHECKED: catalogue library criteria evidenced; #176's remaining test-quality follow-up stays open. |
| T002 | `hostsetup/{detect,plan,native}/`; `TestDetectStates`, `TestDetectCLTGitShimNotExecuted`, `TestPlanHashAndClasses` and fixture/macOS CI exist. Owner-Mac Cellar/Caskroom/shim measurement receipt required by task absent. | OPEN: source delivered, explicit measurement acceptance missing. |
| T003 | `hostsetup/pkgmgr/`, [#201](https://github.com/wstein/workharbor/pull/201)/[#202](https://github.com/wstein/workharbor/pull/202); `TestBrewArgvAndEnvFixed`, privilege/timeout/network/canary tests and native ownership tests. | OPEN: substantial library implementation; T002 prerequisite acceptance remains missing. Real privileged use is OG-9, not inferred from fakes. |
| T004 | [#198](https://github.com/wstein/workharbor/pull/198)/[#207](https://github.com/wstein/workharbor/pull/207); `hostsetup/dotfiles/{dotfiles,native_unix}.go`, `TestDotfileDiffBackupAtomic`, consent/symlink/drift/dry-run/failure tests and native descriptor/race/rollback tests; macOS dotfiles CI exists. | CHECKED: temp-home library criteria evidenced; no actual user-home mutation/acceptance claimed. |
| T005 | `hostsetup/gitmove/` and move/refusal/rollback tests absent; 008/T005 port absent. | OPEN: not implemented. |
| T006 | [#194](https://github.com/wstein/workharbor/pull/194)/[#195](https://github.com/wstein/workharbor/pull/195)/[#197](https://github.com/wstein/workharbor/pull/197); `hostsetup/profile/`, sealed `plan/apply.go`, durable lock/protocol/checkpoint and adapter tests, `TestApplyRerunNoOpStillJournalsAndLocks`, `TestConcreteRerunLeavesBothEnginesUnchanged`, `TestRunLockExclusive`. | OPEN: source delivered; T002 prerequisite and aggregate engine acceptance incomplete. |
| T007 | `hostsetup/fix/` and setup/fix parity tests absent. Existing doctor and router callback are prerequisites, not the repair engine. | OPEN: not implemented. |
| T008 | [#203](https://github.com/wstein/workharbor/pull/203); `hostsetup/plan/{exit,result}.go`, `route.go`; `TestExitMappingMatchesADR0013`, `TestPrecedence73`, `TestResultRedaction`, `TestLinuxSetupRoutesToEnrollment`. | OPEN: internal result/router only; AP-6 public result/reason decision and schema half remain gated. |
| T009 | AP summaries/text applications exist, but required `amendments.md` is absent on main; #183 explicitly omitted unpublished historical proposal. | OPEN: required deliverable/acceptance missing. |
| T010 | `hostsetup/wizard/` and setup-specific prompt/opt-out/state tests absent. 006 wizard is only a prerequisite. | OPEN: not implemented. |
| T011 | No `commands/setup*.go`, macOS setup/doctor-fix registration or host-profile checks. | OPEN: integration absent; AP-6 and named amendment gates remain. |
| T012 | [#166](https://github.com/wstein/workharbor/pull/166); Feature 006 amendment texts already applied, as task records. | OPEN: retained owner acceptance gate; do not reapply texts or infer exact final acceptance. |
| T013 | [#167](https://github.com/wstein/workharbor/pull/167), merge `7d870fa7710ad2ae0318463bb3ad990d2c8376c9`; ADR 0014 host setup/fix text exists. | OPEN: AP-6 and exact owner-gate acceptance remain separate. |
| T014 | Feature 008 carrier text is applied at `9d297da28887ceee35ed6e5669647f07ffb4f220`; [#168](https://github.com/wstein/workharbor/pull/168) records the resolution. | OPEN: task explicitly retains remaining acceptance; not missing text to reapply. |
| T015 | Several libraries and checks exist, but missing integration/tasks and owner measurements prevent full acceptance. | OPEN: acceptance table, remaining prerequisites and owner evidence incomplete. |

Source paths in this table are beneath `apps/whr/internal/`. The audit reran
`go test -timeout 90s ./internal/hostsetup/...` (all nine packages passed) and
`go vet ./internal/hostsetup/...`. No brew, sudo, runtime activation or owner-host
operation was run. macOS CI is existing evidence, not a new owner-Mac observation.

[#176](https://github.com/wstein/workharbor/issues/176) stays open: production
`TestInitialCatalogueEntries` addresses its exact-entry finding, but isolated
agent ID/command rejection, reverse cask/tap mismatch, privileged/host maxima and
a duplicate-requires fixture independent of self-cycles still need tests. No
issue is closed or given a completion label by this package.

## Validation boundary

At the audit baseline, focused `TestLocal|TestManifest` tests passed in
`internal/localdocker`, `internal/localshell` and `internal/workspacecfg`. The
standalone optional Herdr suite passed all 37 tests; the palette verifier passed.
These are source/fixture checks, not SC-009 host evidence. Repository/link,
spelling and whitespace checks are recorded on the PR head. Full local
`make check` and required CI results must be reported separately; no absent check
or runtime gate is represented as passing here.
