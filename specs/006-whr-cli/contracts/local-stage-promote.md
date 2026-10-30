# T038: local stage/promote amendment

**Current cleanup override (owner, 2026-10-25):**
[new whr-managed staging is throwaway](throwaway-staging-amendment-20261025.md).
Normal confirmation covers its container, candidate home, clone/result tree and
candidate metadata. The old retained-clone and separate home-deletion consent
rules below are superseded only for that scope. The accepted execution-profile
body is unchanged; the separate amendment is authoritative for disposal.

**Home backup override (owner, 2026-10-29):** the
[home clone/backup amendment](home-clone-backup-amendment-20261029.md) adds
explicit `workspace home clone|backup` commands. The "no automatic volume
backup" statement below still holds for stage/promote/removal, which run no
backup; the separate commands are the supported way to save a home first.

Status and authority: interaction amendment following owner direction
[LS-1 and its linked owner decision record](../spec.md#ls-1-owner-direction-2026-10-24).
The approval record is the owner-authored approval or owner merge of
[PR #229](https://github.com/wstein/workharbor/pull/229). Until that record this text
is proposed; that record approves only this interaction, not implementation,
cleanup naming/flags, the S0 execution profile or live operations. S0 remains gated
regardless of whether the interaction amendment has merged.

This contract replaces D1/D2 alternatives in the [historical decision brief](local-stage-promote-proposal.md).
[FR-068](../spec.md), FR-063 and [the plan](../plan.md#t038-stagepromote-amendment-2026-10-24)
are reconciled in the same PR. Existing removal code, FR-067 protections, Feature
010 shell selection and owner/live-operation gates are unchanged.

## Selected interaction

Canonical forms, with the existing `ws` group alias and no new bare short forms:

```text
whr workspace stage --local --source <dir> [--smoke] [--dry-run] [--yes] [--trust <digest>] <id|name>
whr workspace promote --local [--dry-run] [--yes] <id|name>
```

`--local` is boolean. Exactly one primary selector is required: exact container
name or full 64-hex container ID; no guessed current workspace, directory selector,
name prefix or role/label batch. The source directory is explicit for stage;
promote uses the source/candidate binding in the receipt. It must not implicitly
restage, rebuild from the source directory or adopt a candidate by name.

Reuse FR-066 discovery and the local Unix endpoint admission, pinned throughout
the transaction. A unique recognized running primary is required. No labelled
match is 65, ambiguous selection is 64, and a recognized wrong-role/non-running
container is 68. Foreign unlabelled matches are not labelled matches. FR-067
removal's distinct foreign-target 68 mapping is unchanged. Match first, then check
role/state; never pick the first discovery result.

Resolve the explicit source and selected primary's workspace-folder label to
physical host paths. Source-to-primary correspondence must hold. Missing,
unresolvable or mismatched source/label bindings refuse 68 before effects.
Symlinks and macOS `/private` aliases require actual host path resolution, never
prefix stripping. Selecting a staging/temp container explicitly is 68. Paths,
names, diagnostics and displayed child output are escaped per FR-022; JSON is
escaped and never mixed with raw child output.

## Source trust and confirmation

Preview identifies the full primary ID/name, endpoint identity without credentials,
source directory, fingerprint, fixed candidate name, image/build intent,
primary/staging home identities, effects and verification policy. Dry-run may read
bounded files and Docker metadata but never runs a source hook, smoke, pull, build,
container mutation or volume mutation; it must not leave an execution receipt.

Building or running repository hooks requires explicit owner trust in the exact
previewed source fingerprint, separate from general mutation confirmation. An
interactive user confirms that fingerprint; unattended/JSON stage application
supplies its exact digest through `--trust` and also `--yes`. Dry-run needs neither
trust nor mutation consent and may report the fingerprint for a later apply. `--yes` alone grants no source
trust. Missing required trust/consent is 70; a changed or mismatched binding is 68.
Promotion needs fresh preview/confirmation (`--yes` unattended/JSON), matching
receipt and source binding. It runs **no host hook**. Any container lifecycle hook
it runs must be bound to the receipt-trusted fingerprint after a byte recheck.
A missing binding refuses 68 before effects; if detected after partial effects,
report 73 and never run the unverified hook. No new trust is inferred from --yes.
S0 must prove this separation and byte binding, including official CLI phase flags. A declined confirmation returns 70 with no mutation; an explicit
interrupt/cancellation remains 72 before effects. Changed source requires a new stage
and explicit trust, not acceptance of a stale receipt.

The fingerprint must cover the actual admitted local configuration/build/hook
inputs, source/submodule identities and the smoke bytes. It must not be merely a
commit ID while executing changed working-tree files. The exact capture set,
canonicalization, digest encoding, limits, environment/substitution handling and
execution-byte binding are a required **execution-profile gate** below. No dirty
working-tree, hermetic-build or complete input-closure support is claimed yet.

Docker clients retain the explicit minimal environment and empty private Docker
configuration, without credential helpers, contexts, secrets or implicit remote
endpoints. The official CLI/hook process needs its own explicit minimal allowlist
and private user/cache directories; inheriting the full host environment is not
an acceptable shortcut. Local caller trust never manufactures remote grants,
identity/provider approval contexts or a control-plane operation.

## Stage and verification

Stage leaves the selected primary running. FR-068 fixes candidate name
`whr-anchorage-staging`, label `anchorage-staging=1`, and a distinct staging home.
There is one candidate per admitted Docker daemon. A name/label collision,
unrelated candidate or abandoned attempt refuses; no automatic adoption, deletion
or whole-stage retry. A private full admitted configuration drives the official
Dev Container CLI; `--override-config` is a full replacement, not a partial delta.
The provider's remote trust/context APIs are not a local-mode API.

A candidate must be running with the expected immutable image ID and mount/user
configuration. If it declares a Docker healthcheck, require a passing result.
Without a declared healthcheck, a successful workspace smoke is required before
promotion. Running alone is insufficient. `stage --smoke` explicitly requests
smoke even if a native healthcheck exists. Without --smoke, do not implicitly run
that workspace script during stage or promote; absence of required verification
is never a successful promotion. S0 must specify the unverified-stage outcome and
explicit verification/recovery path before that state can be implemented. A missing, failing or stale required
verification prevents promotion before the primary is stopped.

Reuse `.devcontainer/smoke-container.sh`, as in Anchorage: bounded, fingerprint-
verified **host bytes over stdin** to fixed argv `bash -s -- <container-workspace-path>`
in the exact candidate, as its expected user. Do not execute the host
`.devcontainer/smoke.sh` orchestrator, infer another entrypoint or treat an
in-container pathname as proof of trusted bytes. Require the admitted interpreter.
No raw script output/environment belongs in the receipt. Displayed stdout/stderr
is untrusted and escaped per FR-022. Existing Anchorage smoke assumes `vscode` and
writes its candidate home; it is not a generic application-health guarantee.

## Promote, data protection and interruption

Bind the candidate receipt to the exact daemon, primary/candidate full IDs, source
fingerprint, immutable verified image ID, primary/staging home identities and
verification result. Reinspect immediately before effects; a mutable tag, name,
label, success line or exit zero alone is not verification. Labels are usability
guards. Promotion recreates the primary with the **same verified image**, preserves
its original home, and verifies the resulting primary before any candidate cleanup.
It must not rebuild a Dockerfile or resolve a mutable tag as an image substitute.

The preview names the primary and full ID and warns that all its exec sessions,
including Feature 010 shell panes, terminate on replacement. Require confirmation;
unattended/JSON promotion without `--yes` is 70. Do not claim a complete exec-session
inventory or preservation by Herdr. Existing shell exit/liveness rules remain.

The [owner decision of 2026-10-24](https://github.com/wstein/workharbor/pull/226#issuecomment-6382864474)
retains the entire `anchorage-home-*` guard, including container-only refusal.
Removing existing Anchorage staging/temp containers with that home template is
not covered by this task. No recovery cleanup path for those protected candidates
is defined here; a FR-067(e) change is a separate decision and task. Do not rename
existing homes, weaken the guard or introduce an internal force bypass.

The owner decision of 2026-10-25 makes new whr-managed staging throwaway.
Normal prompt/`--yes` covers complete candidate disposal; promotion no longer
requires separate `--volumes --force` flags. The primary's original home and
all existing `anchorage-home-*` guards remain protected. See the
[separate disposal amendment](throwaway-staging-amendment-20261025.md) for the
complete artifact inventory and failure/rollback matrix. The prior cleanup
consent decision is superseded for new candidates only.

Before clone disposal on promote, verify the required result refs in the original
repository. Successful promote then removes candidate container/home, clone/result
tree and candidate receipts/journals/metadata. Ordinary confirmed removal also
deletes unfetched and uncommitted clone work; its normal preview discloses that
loss. There is no keep flag, automatic volume backup or later clone-cleanup step.
Dry-run previews these effects without applying them. Failed stages and promotion
rollback clean their owned artifacts as the amendment specifies; incomplete or
uncertain cleanup reports the existing failure category and actual residuals.

## Exit categories and output

Use existing ADR 0013 codes; no numeric table change:

| Code | Stage/promote case |
|---|---|
| 0 | Successful operation, or valid dry-run with no mutation. |
| 64 | Bad arguments/flags, missing/conflicting selectors, ambiguity. |
| 65 | No labelled primary match. |
| 68 | Wrong role/state, missing/unresolved/mismatched source label, source/receipt drift, collision, unsupported capability, failed verification or protected/shared home refusal before effects. |
| 70 | Missing required source trust or normal mutation confirmation; separate deletion flags still apply outside the new-candidate exception. |
| 71 | Local deadline with nothing applied, under this proposed FR-063/ADR 0013 amendment; identify local phase/target, not a fictional remote operation ID. |
| 72 | Interrupt/cancellation before effects. |
| 73 | Partial or uncertain effects; list attempted/applied/remaining resources and recovery. Takes precedence over 68/71/72 after partial effects. |
| 74 | Unclassified internal failure only. |

Use the existing JSON envelope and canonical command IDs `workspace.stage` and
`workspace.promote`. Result names, closed schemas, phase journal/receipt shape,
private state location, stable recovery identity and exact error/result inventory
must be completed in the execution profile. No remote operation API is implied.
Never retry an uncertain mutation automatically. Receipt absence is not proof that
nothing happened; a rerun must inspect and report retained resources, not adopt
an unrelated container or rerun hooks.

## Execution-profile gate before implementation

The selected interface/health amendment is reviewable on its own. It is **not a
ready implementation plan** until the following engineering contract is completed
and independently reviewed with the same artifact approval rules:

1. A capability matrix covering actual source/effective fields, image metadata,
   Dockerfile/context, user/UID handling, mounts, substitutions and all hook kinds.
   Test the actual Anchorage shape; do not silently substitute the remote provider's
   narrower matrix or claim compatibility from its tests. Unsupported cases refuse
   before effects. Respect FR-030: no native-agent launcher or credential relay.
2. A bounded capture/fingerprint algorithm and proof that official resource/hook
   phases and stdin smoke execute the same admitted bytes. Specify handling of
   initialized submodules, untracked/ignored inputs, symlinks, Git metadata, source
   changes and external inputs. Resolve this before claiming working-tree support.
3. Exact official CLI/Docker argv and tool/version admission, fixed endpoint,
   environment and phase budgets; full override construction for verified-image
   promotion while preserving source semantics. Promotion runs no host hook; prove
   container hooks use receipt-trusted bytes after a recheck, or refuse 68 before
   effects (73 after partial effects). Separate stage host hooks from container hooks. Prove new-primary image identity independently.
4. The separately approved candidate-home/cleanup decision and task, with proof
   of compliance under **unchanged** FR-067, plus exact state/lock/receipt durability,
   per-action rechecks, crash and cancellation semantics. Existing protected Anchorage cleanup remains excluded.
5. Closed JSON/schema and command-table changes, exact red-first tests, and a
   reproducible offline fake Docker/official-CLI acceptance matrix. Do not copy
   Go internal packages across module boundaries or forge remote authenticated
   contexts to obtain provider effects. Evaluate narrow pure-source extraction
   explicitly if needed; no T009/control-plane prerequisite may be added silently.

These are engineering deliverables, not new D1/D2 questions. Registration, product
code and resource operations remain deferred until that gate is met. No host smoke,
Docker operation, installation, build or activation is authorized by this document.

## Proposed clone/result revision (owner session direction, 2026-10-24)

The owner explicitly requested a writable independent real Git clone with full
history and ordinary commits/rebases/pushes, result fetch without overwriting
existing work, retained project container hooks on promote, and a justified
restriction table. The prior read-only/synthetic-Git/hook-free profile is not
accepted. This records session direction; exact revised-profile approval and S0
remain open. See [the revised execution proposal](local-stage-execution-profile.md).

That proposal explicitly adds original-repository **objects and exclusively new
operation refs** at promote, without checkout/reset/merge, existing ref moves,
index/worktree changes, FETCH_HEAD writes or autonomous remote push. Original
workspace binding remains; fetched code is available for owner integration, not
automatically active. These Git metadata effects amend prior source-preservation
claims and must appear in preview/receipt/failure inventory.

Result-only candidate commits/HEAD/ref moves do not by themselves invalidate
unchanged image/config/user/mount/hook/smoke/dependency verification. Original
source provenance stays bound; any Git identity/state actually consumed as an
execution input also stays bound and requires fresh stage on change. Promotion
preview/confirmation covers exact result refs/OIDs and the unchanged trusted
execution manifest, without treating --yes as trust in changed source execution.
This explicit clarification preserves normal Git work/result fetch and D1/D2;
it adds no rerun-verification or replacement attestation interface.

No existing-candidate rerun-verification interface is added. Current collision,
no-adoption and changed-input refusal remain. The 2026-10-25 throwaway amendment
replaces retained-clone recovery: removal deletes unfetched work, and a subsequent
stage starts fresh with explicit trust/verification.
The proposal explicitly amends unconditional byte binding to a cooperative phase
contract: pause ordinary writers, compare actual trusted inputs before/after each
phase, refuse detected drift 68 before effects or 73 after effects, and report that
hashes neither freeze paths nor detect changed-and-restored bytes. Hooks retain
normal project paths; no hostile/concurrent-writer isolation is claimed. This
tradeoff requires exact-profile review; this paragraph does not close S0.
No D1/D2, host-hook, home guard, naming or deletion-consent decision is reopened.
