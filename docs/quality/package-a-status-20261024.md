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
