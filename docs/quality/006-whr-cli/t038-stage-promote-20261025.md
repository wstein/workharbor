# T038 local stage/promote implementation evidence

Measurements below were made on 2026-10-25 UTC, using Go 1.27.2 and Git 2.56.0
on Linux/arm64. This record covers Git capture/result transfer, official-CLI orchestration,
durable state, verification, promotion and the actual default command bridge.
Final integrated checks and independent/live acceptance are distinguished below.
It does not mark T038, FR-068 or the feature complete.

## Accepted contract and scope

The execution authority is the owner-accepted profile at
[`115dc61c5bf40d4426fd19573bbe57e2e878c62c`](https://github.com/wstein/workharbor/blob/115dc61c5bf40d4426fd19573bbe57e2e878c62c/specs/006-whr-cli/contracts/local-stage-execution-profile.md),
accepted through [PR #236](https://github.com/wstein/workharbor/pull/236).
The profile supersedes the earlier pending/proposed wording. The accompanying
[interaction contract](../../../specs/006-whr-cli/contracts/local-stage-promote.md)
and FR-066/FR-067 boundaries remain applicable; source trust, mutation consent,
explicit home-deletion flags and live-operation authorization are separate.

The Git component constructs a writable independent clone from a generated,
config-free reader and a verified private raw-object export. Full history covers
ancestors of admitted heads, tags, remote-tracking refs, notes, detached HEAD and
non-gitlink index objects. Those admitted refs retain their names and IDs; this
is not a claim to clone every arbitrary source ref namespace or `.git` file.
Other source refs are bound as inert provenance data. Source replacement refs
are not applied or copied; managed readers set `GIT_NO_REPLACE_OBJECTS=1`.
Source alternates may supply required local objects read-only and are materialized;
candidate export rejects alternates and replacement refs.

Result preparation freezes changed/new HEAD, heads, tags and notes with exact
OIDs, closure manifests and create-only operation-ref mappings. Ordinary later
candidate ref movement is permitted while the frozen graph remains available;
it does not replace the independently verified execution-input version. All
ref-count/collision checks precede destination imports. Original publication
uses safe private administration sharing only the original live ref storage,
including packed refs, so original config/includes remain inactive. It runs the
accepted `start`/`create`/`prepare`/`commit` protocol with `--no-deref` and no update
or delete command. Original worktree, index, HEAD, existing refs and FETCH_HEAD
remain outside transfer effects.

The CLI adds only canonical `workspace stage` and `workspace promote`, plus the
existing `ws` group alias. It adds no bare aliases. Stage requires explicit
`--source` and distinguishes exact source-fingerprint trust from mutation consent.
Promotion forwards only explicit `--volumes`/`--force` choices. Dry-run permits
neither flag, force alone or both; volumes without force refuses 70. Applying
requires both flags and confirmation. `--yes` supplies neither source trust nor
deletion flags. Existing writer confirmation acknowledges the cooperative pause
on input writers; no new quiescence flag is introduced.

## Authoring checkpoints

These are immutable local authoring checkpoints, supplied for integration. They
are not public review verdicts; cherry-picks have different SHAs. The integrating
author must bind final checks/review to the final published head.

| Component | Authoring commit | Scope |
| --- | --- | --- |
| Git staged-object fix | `514d76df3d7d7bae7646a7ceaa31388a133e3356` | Reimport verified objects needed only by the captured index |
| Git capture | `ca91128fab9ba79c0da0137049f41573a9bd0d33` | Config-free bounded reader, safe config, index representation and recursive submodules |
| Git transfer | `f49c1acc6500c1aea720acd64f541ca87089860f` | Frozen mapping/closure, private export/import, create-only publication and original-binding checks |
| Closed JSON schema | `7b6d1e3f4d3dcb908cb30383afec99e7312e3316` | Required/nullable fields, typed targets, bounded inventories and outcome constraints |
| CLI adapters/registry | `d95f07e092df521373212304ade4f3d94e973d86` | Flags, aliases, independent trust/consent, prepared transaction handling and escaped human preview |
| Literal source path | `2e9c2dd263dea0790c1c550ca35d4b0cb1ddc680` | Preserve every nonempty explicit source string for physical backend admission |

The Git author imported shared parent dependencies separately; they are not Git
component changes to cherry-pick again. The command work started from the
committed shared API `8f5e97e5ae9ea4c2ea31b96beee2d4cb4049323e`.

## Git capture and transfer RED/GREEN

`TestLocalStageGitCapturePreservesHistoryAndDirtyIndex` initially failed when
`git show :file` reported a bad object: cloning the reachable export omitted an
orphan staged blob. Reimporting the complete verified pack into the new clone
made the same test pass, preserving the distinct staged, working and untracked
bytes together with normal parent history.

Additional semantic RED/GREEN probes closed actual behavior gaps:

| Test | Observed RED | Observed GREEN |
| --- | --- | --- |
| `TestLocalStageResultOriginalIncludesNeverLoad` | Publication in original Git context loaded a deliberately malformed include and failed 73 | Private publication context transferred results without loading the source include |
| `TestLocalStageResultCandidateRemoteSymrefRemainsOrdinaryData` | Ordinary remote-tracking symbolic ref incorrectly refused 68 as an export ref | Non-export symbolic remote ref admitted; only the two changed HEAD/main result mappings exported |
| `TestLocalStageResultOriginalBindingRejectsMatchingSymbolicResult` | Validator accepted a textual operation symref whose resolved OID matched | Exact operation-ref representation check refused 68 with `original Git operation result ref changed` |

Focused coverage includes SHA-1/SHA-256 identities; safe branch/upstream and
credential-free initial remotes; conflict stages, split index and intent-to-add;
locally complete shallow-marked history; dirty populated submodule HEAD/index/work;
source alternates materialized without retained alternate pointers; and staged
blobs absent from HEAD history. Result tests cover zero changes, later candidate
commits, existing loose/packed collisions, late loose/packed/dangling-symref races,
cancellation after import, the complete 1,025-ref refusal, multi-repository partial
inventory and exact additive original-binding checks. Candidate config symlinks,
FIFO config, alternates, replacement refs and symbolic export refs refuse with
typed reasons. Hostile config/include/helper/hook sentinels are checked.
Fixture Git disables auto-maintenance/GC so detached fixture housekeeping cannot
confound the managed-reader sentinel checks.

Executed at the Git authoring head `f49c1acc6500c1aea720acd64f541ca87089860f`:

```sh
go -C apps/whr test ./internal/localdocker -run '^TestLocalStage(Git|Result)' -count=1 -timeout=120s
go -C apps/whr vet ./internal/localdocker
git diff --check
```

All passed. The focused run took 26.530 seconds: 16 top-level test declarations,
29 leaf cases and 33 pass events including parent subtest records. This was not
the entire `localdocker` suite; the integrated author owns that gate. Formatting
was applied with `gofmt` to the changed Go files.

Three compiled Go overlay mutations produced behavioral failures after successful
compilation. No mutation remains in product source:

| Mutation | Exact test selector | Observed failure |
| --- | --- | --- |
| Replace collision reason `Git result ref already exists` with `Git result target ref invalid`, preserving exit 68 | `^TestLocalStageResultTransferCollisionsAndCancellation/preexisting$` | Required class-specific reason did not match |
| Remove `--no-deref` only from real publication `update-ref` | `^TestLocalStageResultTransferCollisionsAndCancellation/late_dangling_symref$` | `dangling symref redirected transaction into owner branch` |
| Replace private publication Git-dir with `repo.Original.GitDir` | `^TestLocalStageResultOriginalIncludesNeverLoad$` | Malformed original include caused the forbidden active-context publication failure |

To reproduce an overlay probe, copy the named source file outside the checkout,
make the one substitution above, and write a Go overlay JSON object with
`Replace` mapping the absolute original Go filename to that copy. Run:

```sh
go -C apps/whr test -overlay=/absolute/path/to/overlay.json ./internal/localdocker -run '<exact test selector above>' -count=1 -timeout=30s
```

Exit 1 with the listed assertion is the expected mutation result. The restored
sources must pass the focused command above.

## Command and schema RED/GREEN

The complete pre-edit command package passed once on the shared API base
`8f5e97e5ae9ea4c2ea31b96beee2d4cb4049323e` (14.130 seconds):

```sh
go -C apps/whr test ./internal/commands -count=1 -timeout=120s
```

`TestLocalLifecycleContractAmendment` first failed because neither lifecycle
command existed. It now checks the exact canonical paths, flags, metadata,
`ws` dispatch, help and absence of bare aliases. The closed-schema tests first
failed because malformed lifecycle results and missing partial inventories were
accepted. They now check required nullable keys, nonnull arrays, unknown fields,
object/ref/output target shape and action coupling, object-format/OID lengths,
private clone mode, operation-ref namespace, verification image identity,
preview-only planned effects, unverified-reason coupling and partial-73 inventory.
A preview with `verification: null` is valid before an image identity is known;
a nonnull verification record requires its specified image digest.

`TestLocalStageDryRunAndExactTrustConsent` first observed unavailable-adapter 64,
then passed with injected prepared transactions. Argument/refusal tests assert
the exit, error category and reason in the JSON envelope and stderr. They cover
missing/local/invalid flags, missing/conflicting selectors, absent explicit
source, wrong exact trust, and preparation categories 64/65/68/71/72.
Interactive tests retain one bounded reader across the separate trust and
mutation prompts; declines are 70 and explicit cancel is 72. JSON never prompts.
`--yes` still requires separate interactive source trust when `--trust` is absent.
Promotion tests check the full deletion-consent matrix and fresh confirmation,
including disclosure of terminated primary exec sessions and preserved original
home. Human previews escape paths, names, typed effects and result mappings;
diagnostics go to stderr in JSON mode.

`TestLocalLifecycleCancellationAfterPreparationIsNotPreviewSuccess` initially
returned preview 0 after cancellation and now returns interrupted 72, closes once
and never applies. Partial/unverified outcomes retain inventory and their typed
reason. Transactions close exactly once after preview, refusal or apply. Cleanup
failure before effects propagates the typed refusal; cleanup failure after a
successful apply becomes partial 73. An existing apply error is preserved when
cleanup also fails, with the cleanup diagnostic retained on stderr. Public arrays,
including nested ref creates, are normalized without mutating the preview.

At `d95f07e092df521373212304ade4f3d94e973d86`, the complete package passed in
15.621 seconds: 299 leaf cases / 323 pass events, including 67 new lifecycle leaf
cases / 73 pass events. Commands:

```sh
go -C apps/whr test ./internal/commands -count=1 -timeout=120s
go -C apps/whr vet ./internal/commands
git diff --check
```

`gofmt` was applied. After that complete run,
`TestLocalStagePreservesLiteralExplicitSourcePath` exposed an additional usage-64
refusal of a legitimate whitespace-only directory name. Commit
`2e9c2dd263dea0790c1c550ca35d4b0cb1ddc680` changed only the empty-string check
and its regression. The following focused check passed (0.172 seconds); the
earlier complete-suite count is historical, not a claim of a new 300-case run:

```sh
go -C apps/whr test ./internal/commands -run '^TestLocal(StagePreservesLiteralExplicitSourcePath|LifecycleInvalidArgumentsRefuseBeforeBackend)$' -count=1 -timeout=30s
```

The CLI source was tested with compiled overlays using the same `Replace` method:

| Mutation in `commands/local_lifecycle.go` | Exact selector | Observed failure |
| --- | --- | --- |
| Replace the missing-trust message with a different source-approval reason, preserving 70 | `^TestLocalStageUnattendedTrustAndConsentAreIndependent$` | Required `--trust` reason missing from stderr/envelope |
| Bypass exact fingerprint comparison | `^TestLocalStageUnattendedTrustAndConsentAreIndependent$` | Wrong trust applied successfully (0) instead of refusing 68 |
| Bypass promotion deletion-option gate | `^TestLocalPromoteDeletionConsentMatrix/apply_neither$` | `--yes` alone applied successfully (0) instead of refusing 70 |

Each mutation exited 1 after compilation. Separately, temporarily setting
`$defs.localLifecycleResult.additionalProperties` to true in the runtime schema
made `TestLocalLifecycleSchemaClosesResultsAndTargets/unknown_result` fail with
`schema accepted unknown_result`. The original schema bytes were restored and
`go -C apps/whr test ./internal/commands -run '^TestLocalLifecycleSchema' -count=1 -timeout=30s`
passed again. No mutation, test-only source hook or runtime bypass is retained.

## Backend and default-handler integration

The production constructors are connected to the canonical command registry.
`TestLocalLifecycleDefaultHandlersStageThenPromote` exercises real handler dispatch,
JSON envelopes and diagnostics through installed test-owned executable stand-ins.
It asserts preview disclosure of cooperative writer pause, source hooks' writable
workspace/mount/network access, trust and omitted postAttach execution.
No stand-in executes an actual Docker operation, source hook or smoke script.

S1 binds independent source capture, Git state, installed tool bytes and admitted
substitutions to a stable fingerprint: independent previews allocate different
private paths and operation IDs while preserving the fingerprint. S2 retains an
independent writable clone, records allocation intent before native calls, and
keeps the primary running during official `up`, hooks and health/trusted-stdin
smoke. S3 validates the retained receipt, immutable image, execution dependencies,
original bindings and whole cleanup plan before importing any Git objects or
replacing the primary. S4 reaches those paths through default command handlers.

The private raw `source-metadata.json` projection is taken from the complete
stage override. The pinned CLI serializes its raw image contribution, while its
returned configuration is substituted. Build/feature source contributions must
match the full final metadata entry; a plain image's independent contribution is
never removed merely because hook text matches. See upstream
[metadata serialization](https://github.com/devcontainers/cli/blob/v0.89.0/src/spec-node/imageMetadata.ts)
and [config resolution](https://github.com/devcontainers/cli/blob/v0.89.0/src/spec-node/configContainer.ts).

Meaningful backend RED/GREEN checkpoints (rebased equivalents are reachable in
this branch; recorded outcomes preceded the patch-identical pure rebase):

| Test / guard | RED | GREEN |
| --- | --- | --- |
| Full stage success and missing-smoke retention | `194d57da`: compiled allocation-only Apply returned partial | `97761b2e`: resource/hooks/verification paths passed 10.591 s |
| Durable home intent before native create | `a9b722e4`: fake native create rejected missing retained nonce binding | `64be3987`: intent, receipt and versioned artifacts bound before create, 9.527 s |
| Safe actual Git status | `cc3532b6`: corrupted native index passed the constant predicate | `a206fda4`: config-free private reader validates status; normal candidate work remains admitted, 1.801 s |
| Raw metadata origin | `b777d761`: full-stage artifact absent | `d3407595`: raw override projection transitively receipt-bound, 4.906 s |
| Preeffect tool/substitution drift | `3c55f357`: home was allocated before refusal | `1378bc47`: typed 68 before persistent state/allocation, 3.560 s |
| Anchorage greeting facts | `aa996f8e`: fresh initializer facts absent, precise partial 73 | `8836e9b9`: synthetic E producer, create-only W publication before hooks/smoke, unchanged A through promotion, 9.752 s |
| Unverified cleanup then fresh stage | `a75184d6`: retained clone permanently blocked fresh staging | `1296937a`: known unverified receipt/resources absent, journal archived after fresh consent; new OP/home/clone, old clone retained, 6.977 s |
| Separate initializer deadline | `65eb5bdc`: synthetic host phase escaped its deadline | `0ac61584`: short independent host/resource timing, missing/raw/mismatched progress refusals, 0.168 s |

`TestLocalStageAnchorageFactsRemainIndependentThroughPromotion` additionally
changes admitted A facts before promotion and requires typed 68 with the specific
receipt-bound facts reason, then restores them for successful promotion. The
synthetic initializer is test-owned and does not invoke umbrella scripts.
`TestLocalStageInitializerProgressSeparatesResourceBudget` correlates the pinned
CLI's structured outer start/stop events by exact admitted resolved argv and
start timestamp; child raw events cannot finish initialization. Host phase is
bounded at 600 seconds, resource phase at 1800, combined up at 2400, within the
3600-second stage deadline. Missing/ambiguous progress fails closed. Upstream
[event shape](https://github.com/devcontainers/cli/blob/v0.89.0/src/spec-utils/log.ts)
and [host command events](https://github.com/devcontainers/cli/blob/v0.89.0/src/spec-common/commonUtils.ts)
define the tested protocol. There is no duplicate initialize invocation.

Promotion helper measurements include actual stage-produced receipt → immutable
replacement → verified new primary → candidate/home cleanup, preserving the
original home and retained clone. The six required promotion guard declarations
passed 17 leaf cases in 104.801 s before the later wrong-image leaf was added.
Final focused source-origin/success/post-cleanup-drift cases passed 20.308 s;
actual default dispatch/contract checks passed 21.316 s. Full integrated checks
below supersede these historical component counts.

Compiled mutations failed after successful compilation: wrong collision reason
while preserving 68; bypassing immutable-image checks; bypassing the substitution
trust guard; skipping post-home-cleanup input validation; reporting candidate's
passed verification after replacement smoke failed; wrong baked-origin reason
while preserving 68; and removing actual preview write-access disclosure.
The final stage reason mutation replaces only `admitted environment changed since
preview` with `admitted tool changed since preview` in `stage_apply_unix.go`.
`go -C apps/whr test -overlay OVERLAY ./internal/localdocker -run
'^TestLocalStageRejectsChangedToolsAndSubstitutionsBeforeEffects/environment$'
-count=1` fails while exit 68 remains intact (0.725 s). Use the earlier overlay
recipe; no mutant is retained in source.

## Final integration and remaining acceptance

Final integrated check results are recorded after execution below. This record
claims no independent CLEAR verdict, hosted CI result or live operation evidence.
The CI acceptance manifest appends 102 new critical literal Go test IDs, retaining
all baseline entries; fake re-execution entrypoints are harness helpers, not
standalone acceptance IDs.

Delivered scope: canonical stage/promote commands, source trust and consent,
independent Git capture/result transfer, accepted candidate home intent/guards,
retained durable state, official CLI phases, verification, cleanup preflight and
actual default-handler integration. Existing merged ls/rm/shell/drift contracts
remain separate. This does not close the whole feature or historical issue #142.

Open gates: independent review at the final exact head, hosted native CI and
separate owner-authorized real-host acceptance. macOS unknown state-root sharing,
unknown/VM/forwarded native mount correspondence, missing pinned installed tools,
or other named unsupported capabilities refuse 68 before effects. Linux native
admission samples matching primary bind/host facts; it does not claim proof of an
identical mount namespace. Maximum-size stress allocations are not claimed.

For a separately authorized native run, the owner should first record installed
CLI 0.89.0 and pinned tool identities, pause writers, and run `whr workspace stage
--local --source /absolute/source --smoke --dry-run PRIMARY --json`. Inspect the reported trust
fingerprint and human stderr disclosures, then apply with the exact `--trust`
digest and explicit confirmation. Inspect that the original primary/home/source
are preserved and the candidate uses the independent clone/fresh home. Preview
`whr workspace promote --local --dry-run PRIMARY --json`, then explicitly consent with both
`--volumes --force`; verify immutable replacement and hooks/smoke before candidate
cleanup. Preserve the receipt and clone, and independently inspect all failure
inventory before any retry. These are an owner runbook, not executed evidence.

No standalone product build, installation, real Docker/Dev Container lifecycle,
provider invocation, remote Git operation or actual source hook/smoke execution
was performed by this author. Temporary real-Git fixtures and test-owned fake
executables are the measured scope.

The fresh-main rebase onto `79f19ada10c53467074e17a8db42dcc2d55c5776`
compared all 74 authoring commits with `git range-diff`: 74 equal patch rows,
no changed or dropped patch. Rebased code checkpoint was
`ddd4efabb05b4258931b28a4f06a6a03ce736405`.
The first complete affected run exposed the existing table-level unavailable
command diagnostic oracle: new stage/promote handlers returned usage 64 but
omitted `workspace.stage/promote is not available`. `eebcbbe6` preserves that
unavailable wording while explaining the required `--local` capability. Both
`TestMutatingHostCommandsRefuseUnsupportedPlatform` and
`TestLocalLifecycleInvalidArgumentsRefuseBeforeBackend` then passed (0.215 s).

Darwin/arm64 test compilation passed for `internal/localdocker` and
`internal/commands` using `GOOS=darwin GOARCH=arm64 go test -c -o TEMPFILE`.
These binaries were not run. `go vet` passed for localdocker, commands and
workspacecfg. Formatting and repository bootstrap checks passed; bootstrap checks
explicitly do not evaluate product runtime. Final full-suite/harness measurements
follow after execution; the first affected run's command-package failure above
is not hidden or counted as green.

Final affected package measurements on the rebased implementation before the
subsequent narrow shell fix:

| Complete package | Result | Top-level / leaf / pass events | Skips |
| --- | --- | --- | --- |
| localdocker | GREEN, 294.118 s | 154 / 511 / 553 | 0 |
| workspacecfg | GREEN, 7.306 s | 136 / 743 / 775 | 0 |
| commands, complete rerun after unavailable-wording fix | GREEN, 38.870 s | 138 / 314 / 340 | 0 |

Executed with `go test -json ./internal/localdocker ./internal/commands
./internal/workspacecfg -count=1 -timeout=10m`, then the justified full commands
rerun `go test -json ./internal/commands -count=1 -timeout=5m`. The first combined
process exited 1 because of the now-fixed command diagnostic failure; individual
localdocker/workspacecfg packages were green. All 101 then-appended critical IDs
had genuine passing outcomes; this is affected-package evidence, not whole-app
harness qualification.

`umask 022; make check` ran on pristine `f04bc1c6`. Repository bootstrap and its
raw 434 Python tests passed (386.401 s). At the 600-second overall bound (observed
613 s at interruption), the second qualified Python pass was still running; no
application build/test qualification had begun. One exact cwd-filtered owned
checker was interrupted through a PID descriptor; its normal cleanup drained
owned children. Make exited 2. **Full make check is incomplete, not passing**;
hosted native CI remains required. No standalone build/install was invoked.

The final read-only audit found a real tool-binding mismatch: official CLI string
initialize commands execute `/bin/sh`, while discovery could admit an earlier
`PATH` executable named `sh`. `TestLocalStagePinsOfficialHostShellInsteadOfAmbientSh`
failed in `8f79f0e6` with the fake ambient shell admitted instead of the physical
`/usr/bin/dash`. The fix resolves and binds actual `/bin/sh` explicitly; the test
also verifies its exact digest and a typed 68 when its recorded binding differs,
without modifying or executing the system shell. The focused shell/progress/
Anchorage stage→promote group passed (8.734 s). This new guard is the 102nd
critical inventory ID. The historical full-suite numbers above are not claimed
as a complete rerun after this final correction.

After the shell correction, actual default-handler dispatch passed again:
`go -C apps/whr test ./internal/commands -run
'^TestLocalLifecycleDefaultHandlers' -count=1 -timeout=90s` (12.308 s).
Affected-package vet and Darwin/arm64 test compilation passed again. The final
extension of `TestLocalStageAnchorageFactsRemainIndependentThroughPromotion`
also stages afresh after successful promotion: it proves a new operation, home
and independent clone, preserves the previous clone, and retains the original
facts. That genuine full sequence passed in 13.701 s. It uses only test-owned
synthetic tools and temporary Git; this is not real-host lifecycle evidence.

The final source-only repository/bootstrap, formatting and diff checks passed.
Full make check remains incomplete as recorded above. Required hosted CI,
exact-head independent executed review and owner native acceptance are open;
this record does not claim them as passing or mark task/feature completion.


## Independent review correction: executable inputs

The executed review at `9eb6d589` found a High: `sh scripts/setup.sh` could
consume a file outside the original fixed roots, and an arbitrary
`git rev-parse HEAD` hook incorrectly inherited the exit-only status predicate.
Both candidate and original script changes were accepted. Semantic RED commit
`cd7e1314` reproduces these failures (7.952 s). `eaacea26` adds the actual official
0.89 plural merged lifecycle shape: image/Feature `postCreateCommands` were
ignored at preview, and a late unsupported command incorrectly reached verified
success (both RED, 5.848 s). The production correction is `23434b43`.

Admission now supports an explicit non-expanding literal command language and
recursively named literal shell files. Dynamic expansion, evaluation, pipelines,
unadmitted interpreters and opaque unknown bodies refuse with capability 68.
Directly named file bytes join the persisted execution manifest for both A and W;
this does not hash every project file or prohibit unrelated result-only edits.
Source, image and Feature origin arrays all undergo admission, without rewriting
the official execution order or parallel groups. An unsupported contribution
first revealed after `up` returns partial 73 with observed candidate/home
inventory, before any container hook. Post-attach remains skipped.

A bounded catalogue supports the inspected Anchorage greeting profile: 17 full
body digests, repository/path and immutable revision, plus recursive helper
inventory. The recorded Anchorage revision is
`0f72d7226b6921e7d0c2642aca034ef44fe26c56`; BundleDock is
`9f16cb790c108cf7165fc8bfb1ede2180717dd3b`. These hashes were derived only after
comparing inspected bytes with their immutable tracked blobs. Names alone confer
no exemption. Unknown changed opaque bodies refuse; all source bytes still need
the owner's exact trust fingerprint. Tests use checked-in bounded exact-byte
fixtures, never a live umbrella checkout, and never execute these scripts.
Immutable image/tool programs and declared network behavior remain trusted
runtime dependencies: this is not arbitrary shell dependency inference.

Known greeting/smoke actually consumes HEAD, the selected branch/upstream and
computed ahead/behind counts, and porcelain status. Its receipt records this
predicate instead of claiming exit-only Git consumption. A changed consumed HEAD
requires fresh staging; an unrelated candidate result branch remains eligible.
Arbitrary Git commands remain unsupported. The literal standalone status form
records only its admitted success predicate and config bindings. Unsupported
active config dependencies, extra status rules, replacements or grafts refuse
rather than silently using a different context. The preview discloses this
capability boundary and the known profile's Git staleness behavior.

The required focused group passed (48.934 s), including named candidate/original
drift, opaque forms, recursive exact catalogue bodies, actual known
classification→receipt→HEAD refusal, plural preflight/late-origin admission,
Anchorage facts stage→promote→fresh stage and legitimate result transfer. The
selected-upstream/counts plus unrelated-ref positive refinement passed (7.029 s).
Compiled overlays then failed for semantic reasons: removing named paths admitted
both changed scripts (8.126 s); swapping the candidate reason preserved exit 68
but failed the specific reason assertion (4.329 s); bypassing the known Git
predicate accepted the changed HEAD (6.653 s). Overlay recipes replace only the
named path assignments, candidate reason, or known predicate case; no mutant is
retained in product code. Seven additional literal critical IDs are appended to
the acceptance inventory (109 new T038 IDs total). These are fake-tool/temporary
Git proofs, not real-host lifecycle acceptance.


The complete affected run at `23434b43` qualified commands green (36.401 s;
138 top-level / 314 leaf / 340 passing events; zero skips). Localdocker completed
in 249.713 s, within the unchanged 300-second native command budget, but failed
one existing smoke-quota leaf: new grammar admission ran before the existing
256-KiB check, returning capability 68 instead of the quota-specific reason.
The other 519 leaves completed successfully; this run is **not** reported green.
`9b10eb4d` restores the original quota-first order. Its focused quota/unsupported/
merged-origin group passed (3.677 s). A justified complete localdocker rerun
qualifies that correction separately; no timeout or scheduling change was made.

Affected vet, Darwin/arm64 test compilation, repository bootstrap and formatting
checks passed after the dependency correction. Default `typos` reported only
two occurrences of the valid POSIX shell flag `"-ue"`. A rerun with the narrowly
scoped temporary configuration below passed; no repository spelling rule was
weakened and all other words were checked:

```toml
[default]
extend-ignore-re = ['"-ue"']
```


Final qualification at source head `9b10eb4d`, still based on `79f19ada`:

| Package/check | Actual outcome | Top-level / leaf / passing events | Skips |
| --- | --- | --- | --- |
| complete localdocker rerun | GREEN, 261.612 s | 162 / 520 / 563 | 0 |
| complete commands at broad binding fix `23434b43` | GREEN, 36.401 s | 138 / 314 / 340 | 0 |
| actual DefaultHandlers after quota-order correction | GREEN, 17.081 s | focused native fake-tool dispatch | 0 |

Commands were `go -C apps/whr test -json ./internal/localdocker -count=1
-timeout=10m` and the earlier combined affected-package command; the final
constructor-order correction also ran
`go -C apps/whr test ./internal/commands -run
'^TestLocalLifecycleDefaultHandlers' -count=1 -timeout=90s`. The native harness
300-second budget remains unchanged; both measured complete localdocker runs
finished within it. No scheduling change or omitted assertion was needed.
All 109 new literal T038 critical IDs have genuine passing events across these
runs and the recorded unchanged workspacecfg qualification. Existing inventory
IDs remain intact. Affected vet and repository bootstrap passed again after the
quota correction; formatting and diff checks are clean.

Upstream merged W1 while these checks ran: `origin/main` became `3dd1a167`.
These measurements deliberately qualify the fixed author source on old base
`79f19ada`; they are not described as checks on a future rebase or rewritten SHA.
The coordinator owns fresh-main integration preserving W1 changes/critical IDs,
commit-message repair preserving product trees, and exact-head independent
review before publication. Hosted native CI, full make-check completion and
owner real-host acceptance remain open as previously recorded.
