# Candidate-home intent and binding library

Scope: the first Package B unit prepares and checks the identity/mount portion of
`TestCandidateHomeNewOpaqueName` under the
[accepted execution profile](../../../specs/006-whr-cli/contracts/local-stage-execution-profile.md#owner-acceptance-2026-10-24).
This is a library deliverable, not volume allocation, stage orchestration or full
acceptance of that allocation row. T038/T039/T040 remain incomplete.

## Contract and evidence

`localdocker.NewCandidateHomeIntent` accepts a separate operation ID, admitted
daemon/endpoint, exact primary ID/home and `/home/captain` or `/home/vscode`.
It draws 16 bytes from `crypto/rand`, encodes all bits as 32 lowercase hex
characters and projects only `whr-stage-home-<nonce>` as a writable volume mount.
No source path or user name contributes to the name. It refuses malformed scope
and conflicting identity (68), and sanitizes entropy failures (74), with no retry.
The caller supplies the physically admitted socket; this pure library checks its
canonical Unix endpoint spelling without filesystem or daemon inspection.

`CandidateHomeIntent.Bind` compares fresh supplied candidate metadata with the
intent: same daemon/endpoint/primary, distinct full lowercase 64-hex candidate ID,
fixed candidate name/staging label and exact single writable home mount. Explicit
primary/temp role conflicts, duplicate or substituted home mounts refuse 68.
`CandidateHomeBinding.Check` also refuses a replacement candidate ID. All returned
plans/identities are value copies; caller maps/slices are never retained.

| Test | Evidence |
|---|---|
| `TestCandidateHomeNewOpaqueName` | Both destinations; exact deterministic 128-bit encoding; independent operation ID; public constructor names; immutable intended and observed identity copies. |
| `TestCandidateHomeScopeAdmission` | Malformed operation/container IDs, daemon bounds, noncanonical/remote endpoint, primary home and destination refuse before reading entropy. |
| `TestCandidateHomeEntropyFailureAndNoRetry` | Missing/short/failed entropy yields sanitized 74; generated identity conflict yields 68 and consumes no second draw. |
| `TestCandidateHomeBindingRefusesDrift` | Daemon/endpoint/primary/candidate/name/role/mount changes, replacement IDs and zero-value objects refuse 68. |
| `TestCandidateHomePlanningExecutesNoEffects` | Executable canaries for Docker/official CLI/Git/shell remain untouched; no state root is created. |

All five IDs are required by the CI application inventory. The actual red-first
run first failed to compile against absent APIs, then compiled against signature
stubs and failed naming, scope, entropy and binding assertions before behavior was
implemented. The red-phase source blob is
`5adbdba2e1bcc560471dac524735fc6471e6852d`; its test blob is
`c71012c4ae595328ef882654348e412cc41c2576`. They are committed together in the
branch's first `test(localdocker)` commit, so red evidence survives removal of
local logs. From `apps/whr` in an isolated checkout of that red commit, run:

```sh
go test ./internal/localdocker -run '^TestCandidateHome(NewOpaqueName|ScopeAdmission|EntropyFailureAndNoRetry|BindingRefusesDrift)$' -count=1
```

Observed assertion excerpts: `home nonce must encode 128 independent random bits`
with an empty plan, `want zero value and conflict` with a nil error, and
`replacement candidate accepted: <nil>`. The command compiled and exited 1.
Author logs are retained outside the checkout at
`/tmp/whr-b-candidate-home-evidence-20261024/` (`red.log`, `red-behavior.log`,
`green-focused.log`, `green-package.log`). The behavioral red exit was 1.

Executed source checks: Go formatting, focused tests, `go vet` and the full affected
`localdocker` package without race instrumentation; repository source validation
and whitespace checks. Full `make check` is a separate handoff gate: it also runs
build/install operations, which were outside this author's source-test scope.
No real Docker, build, hook, installation or native Linux/macOS execution evidence
is claimed by these library tests.

## Preconditions retained for allocation

A future consumer must admit the physical endpoint and daemon, select the exact
source/primary, capture/trust executable inputs and hold the daemon lock. It must
check complete fresh candidate/retained-intent/volume collisions and all primary,
shared and protected home metadata; fsync private allocation intent, recheck
absence, then correlate operation/nonce/driver/creation observation and all mounts
after attempted creation. Matching names, labels or this binding never prove
ownership, reservation, daemon-wide exclusivity or durable receipt persistence.
Unknown/partial attempts block restaging; there is no adoption, cleanup bypass or
orphan-recovery API. The original primary home and the whole `anchorage-home-*`
staging/temp refusal remain protected by unchanged FR-067 code and regressions.

The bounded pre-implementation analysis found no constitution conflict. Historical
proposal approval text was superseded by the explicit owner acceptance above;
missing stage/receipt consumers explain this unit's explicit library boundary.
Future orchestration remains responsible for the full allocation row and every
later collision, consent, verification and cleanup unit.
