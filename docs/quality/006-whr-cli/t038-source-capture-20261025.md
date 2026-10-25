# T038 source-capture evidence

This is the first replacement slice of PR #261, limited to the source/Git capture
library needed by stage. It adds no lifecycle command, Docker mutation, result
import or promotion. T038/T039 and owner-host acceptance remain open.

## Initial 9a4e51e2 evidence (historical)

The capture portion of #261's findings is addressed here:

| Finding | Implementation and regression |
| --- | --- |
| F9 | Combined packed/loose ref counting; streaming main/shared-index validation and hashing. `TestLocalStageGitCombinedRefQuota`, `TestLocalStageGitIndexHeaderQuotaBeforeBody`, `TestLocalStageGitSharedIndexHeaderQuota`, `TestLocalStageGitIndexRejectsQuotaWithoutReadingBody`, `TestLocalStageGitIndexStreamsLargeExtension`. |
| F11 | Unsupported symbolic HEAD namespaces refuse before cloning: `TestLocalStageGitRejectsCustomHEADBeforeCapture`. |
| F15 (shared capture subprocess helper) | Final group kill precedes ownership release even after leader-observation failure: `TestLocalStageCaptureObservationFailureKillsOwnedGroup`, both timer cases. Stage runner progress precedence belongs to the next slice. |
| F16 (capture only) | Remove unused snapshot/working-copy wrappers and the stale descriptor-error check. Promotion/stage dead-code findings remain separate. |
| F20 (credential remotes) | Omit credential-bearing scp-style URLs: `TestLocalStageGitCredentialFreeSCPRemotes` and the real clone-config assertions in `TestLocalStageGitFormatsRefsAndSafeConfig`. |

Commit `7c2547a4` contains assertion-red Git regression cases, before their fixes.
Commit `802c7f01` contains the assertion-red owned-group cases before its fix.
The extraction commit retains the prior capture tests from #261; it is not a claim
that the original feature's entire implementation history was red-first.
Run from `apps/whr`:

```sh
go test ./internal/localdocker -run '^TestLocalStage(Trust|Capture|Git)' -count=1
go test -json ./... -count=1
go vet ./...
GOOS=darwin GOARCH=arm64 go vet ./internal/localdocker
```

All four commands exited zero locally. The full Go run recorded 3,210 passing test
identities, no failures and one local skip: `TestInitPhysicalENOSPCPremountedFallback`
requires unavailable local namespace capabilities. This is not full acceptance;
native CI must run that existing case without a test-level skip. At initial publication the 18 capture
regression names were added to the CI minimum; the delta below retains them and adds
20 more. This paragraph describes that older run, not a current-head full-whr result. Native CI and independent review
of the published head remain separate gates.

Seven compiled Git mutants were killed: remove combined ref quota, remove custom
HEAD refusal, admit scp credentials, remove main-index quota, bypass shared-index
validation, swap the refusal reason while retaining exit 68, and read the whole
index before validation. Each failure was an assertion, not a compile failure.

## Boundaries and remaining acceptance

Capture tests preserve full history and dirty index/work bytes, including SHA-1,
SHA-256, populated submodules, split/conflict/intent-to-add state and locally
complete shallow history. Host configuration is read as data; source executable
configuration is not inherited by the clone. These fixtures do not prove stage
provisioning or official Dev Container CLI behavior.

Credential/generated-output omissions are listed in capture evidence. A tracked
file omitted by that policy appears deleted in the candidate; committing there can
turn the omission into a result change. Later promotion previews must disclose
this; no result import or automatic integration is present in this slice.

The named S1 acceptance tests in plan.md span later primary selection, source
preparation and Stage CLI wiring. This PR supplies only their capture prerequisite;
it does not relabel these broader criteria as complete. The final stage/composition
record must map each literal acceptance ID to its test coverage. No completion box
is changed here.

## PR 265 initial delta at 33b090ac (historical)

This delta appends focused commits to reviewed 9a4e51e2; no published history is
rewritten. Stage, Docker and promotion remain outside this package.

| Finding | Implementation and executed regression |
| --- | --- |
| F1 | Entire recursive clone is built in an identity-checked private root, cleaned on failure and atomically renamed without replacement only after fresh checks. Replaced roots are retained and identified rather than removed. `TestLocalStageGitFailedSubmoduleLeavesNoClone`, `TestLocalStageGitAtomicPublicationPreservesRacedEmptyDestination`, `TestLocalStageGitCleanupRetainsReplacedPrivateRoot`. |
| F2 | Working directories start writable/private, then receive recorded modes bottom-up. `TestLocalStageGitReadOnlyWorkingDirectories`. |
| F3 | All reported direct capture/config/index/working-copy/process-start IO sites return fixed typed conflicts; host path text is not forwarded. Typed existing refusals and context failures retain their class. `TestLocalStageGitCopyFailureIsClassifiedWithoutHostPath`, `TestLocalStageGitStartAndIndexIOFailuresAreClassified`. |
| F4 | Source and temporary-parent aliases resolve physically; `.git` and administrative pointer aliases still refuse. `TestLocalStageGitSymlinkedTemporaryParent`, `TestLocalStageCaptureSymlinkRefusalReasons`; the complete capture selection passed with TMPDIR behind a symlink. Existing macos-pty CI now runs that selection and the native wait-without-reap test with no skips. Darwin vet passed; actual Darwin execution awaits native CI. |
| F5 | Helper and leading-dash remote forms refuse, with IPv6 SCP and local-path controls. `TestLocalStageGitUnsafeRemoteFormsRefused`; SSH password admission is independently asserted. |
| F6 | A poisoned system/global/environment fixture now exists, plus explicit required argv/env policy checks. N/N+1 working-byte, copy, entry, submodule, graph and administrative quota tests; drift at each admission checkpoint and status mismatch assert both exit 68 and exact reason. `capture_security_contract_test.go` contains these independent cases. |
| F7 | Remove always-false strict/selected branches, redundant object-store call, unused tool assignment, overwritten Cancel and ignored release argument. Later candidate-specific capabilities must be defined in their own slice. |
| F8 | Bound operation-wide object administration to 1,000,000 entries and 32 alternate stores. Audit each physical store once, while freshly rechecking store identity and alternates at every source snapshot. `TestLocalStageGitObjectAdministrationEntryBoundAndSingleAudit`, `TestLocalStageGitObjectStoreQuotaBoundary`. |
| F9 | Bind raw info/exclude into the source digest, copy it inertly into reader and clone, and preserve clone-detected safe core booleans. `TestLocalStageGitInfoExcludeRetainsCleanStatus`, `TestLocalStageGitPreservesDetectedPlatformCoreValues`. |
| F10 | Declared reftable storage refuses before loose-ref parsing, with its precise reason. `TestLocalStageGitReftableRefusalNamesStorage`. |
| F11 | Historical commit 652efb8 is preserved. Its `TestLocalStageGitIndexStreamsLargeExtension`, `TestLocalStageGitIndexRejectsQuotaWithoutReadingBody` and `TestLocalStageGitCaptureVersionFourIndex` were added with the fix and are characterization/coverage, not claimed RED-first. New failure-first and characterization boundaries are recorded below. |

### Resource sizing and remaining limits

A read-only 2026-10-25 walk of Anchorage, excluding .git at every depth and not
following symlinks, counted 83,769 entries including the root, 70,103 under wt/.
The former working-tree limit of 100,000 had only 16,231 entries of headroom.
The new separate working limit is 500,000 entries (about six times this snapshot),
while Git ref/index metadata retains its separate 100,000 ceiling. Working bytes
remain 64 MiB/file and 2 GiB total; submodules remain depth 8/count 32, with
1,000,000 reachable objects and 8 GiB expanded bytes per operation. All limits
remain subordinate to the caller deadline; large admitted counts do not promise
completion on every host. Object-store entry and store-count limits additionally
bound the administrative walk; they do not authorize a larger object graph.
The internal policy seam supplies small budgets for real functional boundary
fixtures; a separate contract test pins the production ceilings. Environment and
argv checks intentionally cover mandatory isolation defenses even when another
isolation layer would prevent the same attack after removing one defense.

The once-per-operation object audit checks file types and symlinks initially.
Subsequent snapshots still bind directory identity and freshly read alternates;
the verified captured object graph/pack determines the immutable result. This is
not a claim of detecting every new loose object or arbitrary same-user ABA race.
Absolute and out-of-tree working symlinks remain unsupported; an in-tree .venv
with an absolute interpreter link can therefore refuse. This slice does not claim
that merely increasing the entry ceiling makes the entire live Anchorage tree
stageable, nor a path-specific UX improvement for that nonblocking limitation.

### TDD, mutations and checks

Test-only c50ce573 exposed cleanup, permissions, raw IO, alias, reftable and URL
failures. Its exclude fixture initially lacked an optional info directory; that
setup error was not semantic RED. Test-only 0f25b941 corrected it and the original
implementation then failed the dirty-status assertion. Test-only 3c17a95a exposes
lost platform core facts before the config correction. Test-only 085544a7 fails
on missing administrative entry/store quotas and inconsistent root counting before
their fixes. Its already-correct environment/byte/graph/drift guards are
characterization tests, independently qualified by mutation failures. The native
wait control and extra publication/retained-root/IO cases are also characterization;
no blanket claim that every new test was RED is made.

Thirty-one compiled Go-overlay mutants failed intended assertions, not build
errors: isolated argv/env/inclusion/protocol defenses, file and graph ceilings,
working-copy quotas, submodule depth/count, all three drift rechecks, status
equality, absolute/admin symlinks, SSH password admission, object entry/store
bounds and audit caching, cleanup, no-replace publication, staging identity and
platform config preservation. Original production source bytes were unchanged
by these probes. Reproduce by removing each named guard and running its literal
regression; small-budget N/N+1 fixtures avoid multi-gigabyte test allocations.

Current author checks:

- `go test -json ./internal/localdocker ./internal/commands -count=1`: PASS,
  455 and 264 passing identities, zero failures/skips (34.580 s / 18.500 s).
- Complete capture selection with symlinked TMPDIR: PASS, 28.902 s.
- Linux vet of both packages, Darwin/arm64 vet of localdocker, gofmt, repository
  validation and diff whitespace: PASS.
- Twenty new literal test IDs join the 18 inherited capture IDs in required CI.
  macOS execution, native required CI and exact-head review are pending, not
  inferred from Linux tests or cross-vet. No local -race run was used.

Required Python script discovery and the pristine make-check attempt are recorded
with their actual outcomes in the PR body; they are not replaced by these focused
Go checks. No real Docker/devcontainer or host lifecycle check was performed.
No task/feature completion state is changed. The dependent stage slice must adapt
removed unused internal signatures when it is restacked; its acceptance remains
separate.

## Follow-up delta: N1 to N4

The accepted working-entry ceiling is restored to 100,000. The read-only sizing
measurement of 83,769 Anchorage entries did not authorize the former 500,000
increase; that increase is withdrawn. A larger working-entry ceiling needs a
separate owner-accepted profile amendment. No measurement is an owner decision.
The initially proposed administration ceilings are withdrawn from both code and
the accepted profile. Neither a separate audited-entry ceiling nor an alternate
store-count ceiling changes admission. The accepted reachable-object/byte budgets
and cancellation/deadline checks remain. Additional ceilings require a separate
owner decision before implementation; historical cap evidence above does not
describe current behavior.

Per-repository Git scratch now lives outside the recursive clone tree, and the
working directory modes are applied deepest-first only after all populated
repositories have been captured. `TestLocalStageGitSubmoduleUnderReadOnlyDirectory`
was assertion-RED before the fix and preserves history and the final 0555 mode.
The original standalone working-copy helper still applies its modes bottom-up.

SSH URL hostnames/usernames and SCP-style users/hosts beginning with a dash are
refused; the unsafe remote table covers both newly reported forms and user forms.
The accepted-ceiling and remote assertions were RED before their corrections.

The seven newly reported controls have direct regressions: object-store symlink
and file-type refusal, re-audit of a changed physical store identity, exclude-info
symlink refusal, 256-KiB exclude N/N+1, exclude drift in the recursive binding, and
reader/candidate status parity under a detected filemode=false platform fixture.
Refusals assert exit 68 and their specific reason. The identity fixture starts
with a valid info directory, then replaces the store with a new inode and FIFO.
The platform fixture substitutes one existing platform config value as data; an
initial duplicate-key setup failure is not RED evidence. These already-correct
controls are characterization tests qualified by compiled mutations.

Current local affected packages passed: localdocker 466 and commands 264 test
identities, no failures/skips (35.796 s / 18.132 s). Linux vet, Darwin/arm64 vet,
formatting and repository validation passed. Native macOS capture/wait evidence
exists for historical 33b090ac in run 40212143976 (78 PASS lines), not for the new
head. That run's bootstrap/check failed the stale synthetic-merge-parent guard;
the branch is now rebased onto fresh main e186036 with a new event required.
The PR body records the current script/make-check outcomes and exact native gates.
No Docker/owner-host lifecycle evidence or task completion is claimed.

## N1 completion: remove unapproved caps from implementation

Merely marking the two new administration caps proposed did not meet the review's
instruction to keep accepted values in code. They are now removed, including the
unused limit fields and quota-only tests. The accepted profile paragraph is
restored; no new numerical limit is added to it.

TestLocalStageGitAlternatesBeyondUnapprovedStoreCap was assertion-RED with 33
ordinary local alternate stores before the correction. It passes afterward.
TestLocalStageGitObjectAdministrationSingleAudit replaces the withdrawn quota test:
the same physical store is audited once within an operation, while a fresh
operation refuses an introduced unsafe node with exit 68 and the specific reason.
The published-limit regression still pins the accepted working, object, byte and
submodule limits. CI inventory replaces only two IDs introduced by this unmerged
PR; no main-baseline test ID is dropped. All seven N4 control regressions remain.
The PR body records final affected-package and verification results for this round.
