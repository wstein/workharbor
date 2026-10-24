# T030 custom projects placeholder regression

This test-only unit closes the custom-`projects_path` placeholder evidence gap
identified in the [T030 core record](t030-init-core-20261024.md) and merged
[PR #241](https://github.com/wstein/workharbor/pull/241). Existing production
behavior already places `.gitkeep` under the manifest's projects path. No
production defect, behavioral fix or whole-T030 completion is claimed.

## Bounded coverage and measured checks

[TestInitGitkeepUnderProjectsPath](../../../apps/whr/internal/scaffold/gitkeep_test.go)
uses the nested custom path `source/components` with two temporary fixtures:
one creates the directory chain and one reuses existing real directories.
It checks the retained empty placeholder bytes, exactly one typed create action
in the preview, the written empty file, successful Git-free validation, absence
of the default `projects` directory, byte-preserved existing v1 manifest and
identity of the reused directory. An identical rerun must apply no actions and
preserve the placeholder's inode and modification time.

Tests were measured on 2026-10-24 with Go `go1.27.2 linux/arm64`, umask 022,
and `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off`. Commands ran from `apps/whr`.
The first executed new-test check was a deliberate mutation RED, followed by
the old-test control and then unchanged-production GREEN:

| Check | Measured result |
| --- | --- |
| `go test -count=1 -overlay=<overlay.json> -run '^TestInitGitkeepUnderProjectsPath$' -v ./internal/scaffold` | Exit 1: both custom-path subcases fail at the missing `source/components/.gitkeep` proposal assertion. This is an assertion failure, not a compilation/setup failure. |
| Same overlay, `go test -count=1 -overlay=<overlay.json> -run '^TestInitApplyCreatesAndValidates$' -v ./internal/scaffold` | Exit 0: the previous default-path test still passes, demonstrating the coverage gap. |
| `go test -count=1 -run '^(TestInitGitkeepUnderProjectsPath\|TestInitApplyCreatesAndValidates\|TestInitRerunIsNoOp\|TestInitPreservesExistingSettings)$' -v ./internal/scaffold` | Exit 0: four top-level tests and the two new subcases pass; no failures or skips. |

The compiler overlay replaces only
`p.file(s, o, doc.ProjectsPath+"/.gitkeep", []byte{})` in
[proposal.go](../../../apps/whr/internal/scaffold/proposal.go) with
`p.file(s, o, "projects/.gitkeep", []byte{})`. To reproduce, copy that source to
a temporary file, make the single replacement, and supply a Go overlay JSON
`Replace` entry from the absolute original source path to that temporary file.
No tracked production source was changed. This is fresh mutation evidence for
the regression assertion, not historical RED evidence for the original D1
implementation.

Measured source was `8bd542e2f7c8ddb99884b49bfe0f74d1aeb6197a`. The unpublished
topic advanced to `f2d96805383410f401826877b634d2c03780e977` before its first
commit; `git diff --quiet` between those bases for `apps/whr`,
`contracts/cli/output.schema.json` and `contracts/workspace` returned 0.
Unchanged focused test results were retained. No accepted or published history
was rewritten. The new test is discovered by ordinary scaffold package tests;
this bounded unit changes no CI inventory or task/progress checkbox.

`gofmt -l` on the test produced no output; the repository source checker,
`typos` on both new files and `git diff --check` passed. The source checker
reports bootstrap checks only, with product runtime not evaluated.

## Remaining literal names and proposed units

At the measured base, 17 of T030's 31 literal `Test*` names were absent.
This unit adds one; the other names below are a concrete future test plan,
not implemented or passing-test claims. Existing combined coverage remains
valid, and future standalone tests must add the stated focused assertions.

| Literal T030 name | Existing mapping and focused follow-up |
| --- | --- |
| `TestInitGitkeepUnderProjectsPath` | Delivered here: custom nested path, preview/write agreement, directory reuse and no-op preservation. |
| `TestInitNeverOverwritesWithoutOverwriteFlag` | Current `TestInitPublicRerunNoOpAndOverwriteConsent`; consent unit should assert byte and identity preservation when only yes is supplied. |
| `TestInitOverwriteRequiresYesAndPath` | Current options/public consent tests; consent unit should exercise yes/path combinations and verify missing-path remediation before any write. |
| `TestInitExistingManifestPreservedWithoutRequestedEdit` | Current `TestInitPreservesExistingSettings`; consent unit should assert exact v1/v2 authored bytes and no implicit default materialization. |
| `TestInitManifestEditsNeedOverwrite` | Current settings/public consent tests; consent unit should assert explicit-default edits require the manifest path and authorized replacement retains unrelated settings. |
| `TestInitNonInteractiveWithoutYes` | Current `TestInitModes` and unsafe-input tests; consent unit should pin exit 70 and `--yes` remediation independently of overwrite permission. |
| `TestInitSymlinkTarget` | Current discovery/hostile/exclusive-create tests; path unit should assert overwrite permission cannot authorize a symlink and its outside target stays untouched. |
| `TestInitExistingDirectoriesReused` | Current apply/no-op tests and this custom-path test; path unit should bind directory identities and verify rollback retains pre-existing directories. |
| `TestInitIdenticalFileSkipExisting` | Current no-op tests; path unit should pin the exact `skip_existing` action without overwrite permission and unchanged bytes, inode and time. |
| `TestInitBundlesLeftUnchanged` | Current unsafe-input test refuses unsupported bundles in memory; path unit should assert on-disk bytes survive refusal, with and without requested manifest edits. |
| `TestInitOverwriteTempFilePrefixDetected` | Current discovery/apply-time scan tests; path unit should assert the deterministic prefix blocks a consented overwrite without removing the orphan or editing the target. |
| `TestInitProjectDeclaredNotPinnedWarning` | Current `TestInitProjectURLRules`; result unit should assert the warning in both preview and applied result without creating a project checkout. |
| `TestInitVersion2NotCreationReadyWarning` | Current source acceptance and preservation tests; result unit should separately pin new/replacement preview/result warnings and unchanged-v1 behavior. |
| `TestInitExitCodes` | Current public closed-envelope tests; result unit should pin the init-specific outcomes with JSON code/result and stderr consistency. |
| `TestInitExitCodeExistingFileVsReplaced` | Current consent/path-appearing tests; result unit should pair pre-existing unapproved file 70 with post-preview replacement 68 and retained foreign bytes. |
| `TestInitNoGitNoNetwork` | Current positive execution traps and closed-environment source tests; isolation unit should run the init source child in an actually isolated network namespace and retain no-Git/credential-home assertions. |
| `TestInitDevcontainerFlagNotInRelease` | Historical unavailable assertion is superseded by accepted T033 selection. Current `TestInitDevcontainerSelectionValidated` and public selection tests cover availability; do not add a false refusal merely to satisfy this old name. Reconcile the historical name with the authoritative requirements separately. |

Consent/preservation, path and result assertions are independent small units;
consent authoring runs in a separate lane. The safety sequence after this unit
is overwrite per-event cancellation, owned-child actual signals at writer
steps, then isolated-network and physical full-filesystem fixtures. The existing unexported
apply callback can support writer-step handshakes without production test hooks.
Actual signal evidence for that writer and the existing main entrypoint remain
separate claims. Root assigns subsequent units; this plan does not execute them.

## Safe fixture capability inspection and remaining gates

Bounded capability probes on the same Linux host found `unshare -rn -- true`
returns 0, while `unshare -n -- true` returns 1, Operation not permitted.
The invoking process has effective capability mask zero. In a child created
with `unshare -rnm`, mounting a 1 MiB tmpfs at an owned temporary directory
returns 0 and reports filesystem type `tmpfs`; that private mount disappears
with its namespace. No storage was filled and no network connection attempted.
These probes identify viable isolated fixtures for later tests; they do not
prove init network denial or physical ENOSPC rollback. Tests are already
authorized, subject to actual native permissions; these gaps are not an
additional approval requirement.

T030 and SC-018 stay open for the other literal assertions and incomplete
overwrite cancellation, every-step actual signals, physical ENOSPC,
process-wide network-denial and historical RED proof described in the core
record. A new mutation RED cannot supply the missing historical evidence.
Full `make check`, required published-head CI and independent review remain
handoff gates. No standalone build, installation, race run, full application
gate, host-runtime activation or task/progress change was performed here.
