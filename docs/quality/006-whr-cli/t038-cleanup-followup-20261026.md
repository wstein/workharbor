# Cleanup refusal follow-up

Date: 2026-10-26. Base: `d5b1d708a3acfd283600f0ac68559a384b71f60d`.
Test commit: `76de7cf2f6269a9eebfaeebd226ccdb61f4f1439`.

This tests-only follow-up to #272/#274 adds two refusal cases using one existing
fake-Docker fixture. It changes no product behavior or task completion status.
`TestThrowawayDisposalRefusesCandidateDriftAndMountedResidualHome` checks:

- A receipt-bound candidate whose name changed refuses disposal with
  `candidate home identity or mount binding refused`, exit 68.
- When the candidate is already absent, an unrelated container mounting its
  residual home refuses disposal with
  `local stage candidate cleanup home remains mounted`, exit 68.

Both cases require no removal calls and preserve the home and running primary.
They also check that the renamed candidate or unrelated container remains.
The inputs and exact assertions are in
[throwaway_followup_test.go](../../../apps/whr/internal/localdocker/throwaway_followup_test.go).
The existing fixture and no-removal assertion come from
[throwaway_binding_test.go](../../../apps/whr/internal/localdocker/throwaway_binding_test.go).

## Compiled mutation evidence

Each probe used a private `go test -overlay` replacement of one production file;
tracked production source stayed unchanged. The six successfully compiled probes
first ran the relevant existing tests below. This characterizes existing coverage;
it is not a RED-first implementation claim. Only the two surviving behaviors
received new tests. Every reported kill is an assertion failure with exit 1.

| Probe | Mutation | Existing test selection | Existing result | New-case result |
|---|---|---|---|---|
| M8 | Swallow candidate `Bind` error | `TestThrowawayDisposalBindingRefusals` | Survived, exit 0 | `renamed_candidate` fails at the exact refusal assertion |
| M9 | Disable mounted-home refusal | `TestThrowawayRemoveFinishesResidualAfterContainerDeletion` | Survived, exit 0 | `mounted_residual_home` fails at the exact refusal assertion |
| M10 | Delete candidate records before resources | `TestThrowawayRollbackResidualReportsObservedResources/container` | Killed: retry cannot read cleanup journal | No additional test |
| M11 | Skip disposal after restored rollback | `TestThrowawayPromoteRollbackDisposesCandidate` | Killed: candidate artifacts remain | No additional test |
| M12 | Leave `ownedAttempt` false | `TestThrowawayFailedStageDisposesArtifacts/missing_smoke` | Killed: failed-stage resources remain | No additional test |
| M13 | Remove candidate skip from generic removal | `TestThrowawayRemoveDisposesBoundCandidateWithoutVolumeFlags` | Killed: removal loses private receipt | No additional test |

For M9, the historical mutation's indentation was adapted to the current source.
An added `_ = m` only keeps the disabled guard's loop variable compilable.
An earlier M9 compilation failure is excluded from the six compiled results.
The initial broad probe selection and unchanged `make check` baseline were
canceled to preserve resources; neither is passing evidence.

The new test baseline passed with exit 0. M8 and M9 each returned nil/exit 0 at
`throwaway_followup_test.go:37`, where the corresponding exact reason and exit 68
are required. Both new-case mutant runs exited 1. The selected existing tests
also passed without overlays (exit 0). This isolates each added test's causal
assertion from compilation failures and unrelated refusal paths.

Commands, overlay replacements, and raw results are retained outside the
repository in `/tmp/whr-cleanup-probes-20261026/`. The new baseline command was:

```sh
go test -count=1 -timeout 5m -run '^TestThrowawayDisposalRefusesCandidateDriftAndMountedResidualHome$' ./internal/localdocker
```

The mutant commands used the same module and package with `-overlay` and the
corresponding `/renamed_candidate` or `/mounted_residual_home` subtest selector.

## Verification limits

`go vet ./...`, formatting, and `python3 scripts/check_repo.py` passed.
`go test -count=1 -timeout 30m ./...` passed in `apps/whr` (exit 0); the
local-Docker package completed in 939.825 seconds. The run began after the new Go source was final,
before that file was committed. The file was then committed without changing its
bytes; both the tested file and test commit have blob
`39dfccd84f11b2ec84c734f63a565022c4a13c4b`. This is working-tree test evidence,
not a pristine committed `make check` result. The required script-suite command
`python3 -m unittest discover -s scripts -p 'test_*.py'` exited 1: 450 tests,
eight failures and seven errors, all in unchanged `OfflineAutomationTests`.
Preparation returned exit 5 with `output-conflict`; later cases lacked artifacts.
A single-fixture trace pinned the refusal to `scripts/task_package.py:961`, the
final output inventory check. The same representative preparation on an untouched source archive of the base
commit returned the identical exit 5, `output-conflict`, and refusal location.
This establishes a baseline local-check limitation, without claiming its cause.
These failures are not passing checks.
The raw script output is `scripts-tests.txt` in the external evidence directory.
`make check` is incomplete: its initial raw phase was canceled, and its application
phase includes standalone Go builds, dependency installation, and Mix/assets builds
outside this task's authorization. `mix` is unavailable on this host. No tool was
installed. Native CI owns required acceptance, race, and macOS checks.

These are fake-Docker fixture results. No real-host Docker or browser verification
was performed, and no product runtime, deployment, or activation claim is made.
