# T030 result warnings and exit reporting acceptance

This bounded unit adds four dedicated literal T030 IDs in two new files:
`apps/whr/internal/scaffold/init_acceptance_results_test.go` and
`apps/whr/internal/commands/init_acceptance_results_test.go`. No existing files
or product behavior change. Base:
`f2d96805383410f401826877b634d2c03780e977`; test commit:
`2a9466a5dd534db50cb5d2502f674be60902db66`. Measured on 2026-10-24 with Go
`go1.27.2 linux/arm64` and temporary filesystem fixtures.

## Dedicated assertions

| Test | Accepted behavior exercised |
| --- | --- |
| `TestInitProjectDeclaredNotPinnedWarning` | Preview and applied result each carry exactly one project.declared_not_pinned finding at warning severity, with valid reports. The retained declaration is written exactly as previewed, including the derived projects/project path, but that checkout does not exist. Git checks stay not_run and runtime enforcement stays not_in_this_release. |
| `TestInitVersion2NotCreationReadyWarning` | New v2 and consented v1 replacement each carry exactly one schema_version 2/not creation-ready/T035 warning in both preview and result. No-edit v1 carries no v2 warning and retains its deliberately noncanonical bytes. Written bytes equal the retained proposal, and new/replaced manifests validate as v2. |
| `TestInitExitCodes` | Init-specific outcomes 0, 64, 65, 68, 70, 72, 73 and 74 in human and JSON modes. JSON is exactly one document with canonical workspace.init, schema version 1, matching process exit and ok, and matching named error code. Failures leave human stdout empty and emit exactly one CLI diagnostic line; JSON error text agrees with that line. Successful warnings remain exit 0. The injected partial outcome retains structured recovery paths and per-path diagnostics. |
| `TestInitExitCodeExistingFileVsReplaced` | Existing regular manifest edited without path consent returns 70 naming --overwrite workharbor.json. A consented edit whose manifest changes in place or is replaced after preview returns 68. Both human/JSON modes retain the original or foreign bytes, identity, mode and modification time, without new projects, agent workflow, Claude directory or journal. |

There are 26 fixture/mode scenarios: one project declaration, three v1/v2
warning scenarios, eight exit outcomes in two output modes, and three manifest
conflict scenarios in two output modes. These are dedicated assertions, not
aliases for existing tests. CLI error lines are matched at line starts so
`whr:agents-block` markers in the ordinary proposal preview are preserved.

## Real cases and injected reporting seam

Exit cases use actual init execution through Execute and the production
handlers: invalid shell choice (64), absent directory (65), invalid existing
manifest (68), missing unattended yes (70), cancellation as preview is emitted
(72), and a one-shot preview writer failure (74). Warning-only successful
initialization returns 0. The 73 case alone uses the existing private
initWithApply seam to supply an incomplete-rollback result with two remaining
paths; this tests reporting without claiming an executed rollback failure.
The real rollback acceptance remains in its separate T030 unit.

The error mapping follows FR-063 and ADR 0013. Authentication/authorization and
incompatible-contract codes 66/67/69, and timeout 71, are not invented as local
init cases. Existing table/schema freeze tests remain unchanged. This unit does
not restore the historical wave1a devcontainer/wizard refusal: accepted current
selection and interactive behavior are preserved.

## Explicit regression RED evidence

Four temporary source overlays each exited 1 with assertion failures against
the final test source. Product files in the checkout were never edited.

| Source under apps/whr/internal | Single mutation | Failing dedicated test |
| --- | --- | --- |
| workspacecfg/validate.go | Remove the project.declared_not_pinned c.add call. | TestInitProjectDeclaredNotPinnedWarning |
| scaffold/proposal.go | Change the readiness-warning predicate from SchemaVersion == 2 to == 1. | TestInitVersion2NotCreationReadyWarning |
| output/output.go | Change the failure envelope's ExitCode from int(code) to 0. | TestInitExitCodes |
| scaffold/apply.go | In conflict(), change PathConflict to PathHumanDecisionMissing. | TestInitExitCodeExistingFileVsReplaced |

To reproduce, copy the specified source to a temporary file, make the single
replacement, and use Go's JSON `{"Replace": {"original": "replacement"}}`
overlay with absolute paths. From apps/whr run
`go test -count=1 -overlay=<overlay.json> ./internal/<package> -run
'^<dedicated-test>$'`, selecting scaffold or commands as appropriate. Local
inputs/logs are under `/tmp/whr-t030-result-mutants-w5lxw29v`; the definitions
above are durable. These are regression probes of accepted behavior, not an
absent-implementation RED phase or evidence of a new product defect.

## Green checks and limits

From apps/whr with `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off`:

```sh
go test -count=1 ./internal/scaffold -run '^TestInit(ProjectDeclaredNotPinnedWarning|Version2NotCreationReadyWarning|OutputValidates|ProjectURLRules)$'
go test -count=1 ./internal/commands -run '^TestInit(ExitCodes|ExitCodeExistingFileVsReplaced|PublicExitCodesAndClosedEnvelopes|PublicPartialRecoverySurvivesMessageClip|PublicJSONWritesAndValidates)$'
go vet ./internal/scaffold ./internal/commands
```

All passed, including five existing adjacent report/schema regressions.
`python3 scripts/check_repo.py` passed (bootstrap source checks; product runtime
not evaluated). Targeted `typos`, `gofmt -l` and `git diff --check` passed.

No task/progress ticks, product changes, full suite, race, standalone build,
installation, runtime network, macOS or full make execution is included.
Independent review, pristine full gates and publication remain separate.
Other T030 consent, path, gitkeep, cancellation/signals and no-Git/no-network
acceptance units retain their separate evidence. T030 or T034 completion is not
claimed by these four tests.
