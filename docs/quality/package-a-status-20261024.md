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
