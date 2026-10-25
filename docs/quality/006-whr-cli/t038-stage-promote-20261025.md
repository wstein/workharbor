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

Meaningful backend authoring RED/GREEN checkpoints (pre-rebase SHAs identify
retained authoring objects):

| Test / guard | RED | GREEN |
| --- | --- | --- |
| Full stage success and missing-smoke retention | `4fcd614f`: compiled allocation-only Apply returned partial | `821de644`: resource/hooks/verification paths passed 10.591 s |
| Durable home intent before native create | `68c091b8`: fake native create rejected missing retained nonce binding | `9149b32f`: intent, receipt and versioned artifacts bound before create, 9.527 s |
| Safe actual Git status | `f28dec82`: corrupted native index passed the constant predicate | `e8396c43`: config-free private reader validates status; normal candidate work remains admitted, 1.801 s |
| Raw metadata origin | `782a15d6`: full-stage artifact absent | `820eb4bc`: raw override projection transitively receipt-bound, 4.906 s |
| Preeffect tool/substitution drift | `45380e60`: home was allocated before refusal | `946a6e9b`: typed 68 before persistent state/allocation, 3.560 s |
| Anchorage greeting facts | `443462a4`: fresh initializer facts absent, precise partial 73 | `fb2b646e`: synthetic E producer, create-only W publication before hooks/smoke, unchanged A through promotion, 9.752 s |
| Unverified cleanup then fresh stage | `70af00bd`: retained clone permanently blocked fresh staging | `e2375ed1`: known unverified receipt/resources absent, journal archived after fresh consent; new OP/home/clone, old clone retained, 6.977 s |
| Separate initializer deadline | `c010d4a5`: synthetic host phase escaped its deadline | `85e9d37a`: short independent host/resource timing, missing/raw/mismatched progress refusals, 0.168 s |

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
The CI acceptance manifest appends 101 new critical literal Go test IDs, retaining
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
--source /absolute/source --smoke --dry-run --json`. Inspect the reported trust
fingerprint and human stderr disclosures, then apply with the exact `--trust`
digest and explicit confirmation. Inspect that the original primary/home/source
are preserved and the candidate uses the independent clone/fresh home. Preview
`whr workspace promote --dry-run --json`, then explicitly consent with both
`--volumes --force`; verify immutable replacement and hooks/smoke before candidate
cleanup. Preserve the receipt and clone, and independently inspect all failure
inventory before any retry. These are an owner runbook, not executed evidence.

No standalone product build, installation, real Docker/Dev Container lifecycle,
provider invocation, remote Git operation or actual source hook/smoke execution
was performed by this author. Temporary real-Git fixtures and test-owned fake
executables are the measured scope.
