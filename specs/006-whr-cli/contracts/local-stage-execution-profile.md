# Proposed local stage/promote execution profile

Status: **revised proposal; S0 incomplete**, 2026-10-24. The owner explicitly asked
in this session to revise the prior profile before implementation and specified:
(1) a writable independent Git clone with full history and ordinary commits,
rebases and pushes, (2) fetch results back without overwriting existing work,
(3) retain project container lifecycle hooks during promotion, and (4) explain
restrictions individually and remove those without a relevant justification.
This records that session direction, not an invented prior GitHub approval.
Exact-profile independent review and owner approval remain pending. No product
code, schema, public flag, task edge, runtime operation or compatibility is claimed.

The [interaction contract](local-stage-promote.md), [FR-068/LS-1](../spec.md),
[home decision](candidate-home-cleanup-decision.md) and
[cleanup task proposal](candidate-home-cleanup-task-proposal.md) remain the
foundation. D1/D2, no-host-hook promotion, FR-067 and deletion consent do not change.
Clone/result-transfer, receipt-bound refresh and conditional phase binding are **proposed
focused amendments**, not behavior already authorized by the old interaction text.

## Writable candidate and full-history Git capture

Candidate project data is an independent real Git clone for the root and each
populated submodule, retaining original commits and full reachable history. It is
mounted read-write at the original container workspace path. Normal edits, commits,
rebases and owner-initiated pushes are allowed. No synthetic commits, protected
project-file overlays or permanent source-write ban. Candidate work leaves the
original worktree, index and existing refs untouched. The owner may configure
credentials in the candidate for their own Git activity; whr neither supplies host
credentials nor autonomously pushes to any remote.

Capture must preserve **all three** original states: HEAD/tree identity, index
stages and actual modified/deleted/untracked working bytes, recursively including
dirty submodules. Plain clone alone is insufficient. Capture original physical
source/primary correspondence separately; private clone paths never select a
primary. Recognized source changes after capture require fresh trust, not a stale
commit-only fingerprint. Candidate clone data survives container/home cleanup;
uncommitted and untracked work is retained and reported, never silently discarded.

Proposed fixed capture bounds: 100,000 filesystem entries, 2 GiB working bytes,
64 MiB/file, 32 populated submodules, depth 8, configuration/smoke 256 KiB, pathname 4096 B;
full Git object graph additionally 1,000,000 objects/8 GiB per operation (including all required local alternate objects). Exceeding
bounds refuses 68 before hooks; it never silently truncates or makes a shallow clone.
Normal clone full history means all ancestors reachable from admitted heads/tags/
remote-tracking/notes refs and detached HEAD, plus index objects; source reflogs and
unreachable garbage are not ordinary clone history. Reject shallow/partial source
objects only when the required complete graph cannot be obtained locally; no fetch
or missing-object download is implicit in stage/dry-run.

Safe clone construction protocol (private local artifacts, no source writes):

1. Resolve .git/worktree/submodule administration physically. Use a generated empty
   Git directory to read the resolved object directory/index; never run original
   repository configuration, templates, hooks, filters, fsmonitor or maintenance.
   Read refs/HEAD, `ls-tree -rz`, `ls-files --stage -z`, `cat-file --batch` through
   fixed bounded protocols. Source alternates may be resolved read-only, then
   materialized; no private clone may retain an alternate or source hardlink.
2. Export and verify raw reachable real objects into a self-contained private bare
   repository. Retain actual IDs, refs and parent history; do not copy the entire
   .git directory, config, credential helpers, include files, hooks, worktree
   pointers, reflogs or arbitrary state. Git object history is owner repository
   content, not an ambient credential store; do not falsely promise historical
   secret scanning. Record exactly which refs/object graph were captured.
3. `git clone --no-local --no-hardlinks --no-checkout --template= EXPORT CLONE`
   creates the real independent clone. Materialize captured raw index entries
   (including conflict stages) and working bytes without checkout filters or hooks;
   preserve each submodule's actual HEAD/gitlink/index/working state. Normalize only
   administrative paths to standalone private repositories. Validate equivalent
   source/candidate status categories and IDs; unsupported index representation
   refuses with its precise limitation, not a fabricated clean tree.
4. Generate safe initial candidate Git configuration: core/submodule layout and
   admitted branch/upstream/credential-free remote URLs only; no URL rewrites,
   credential helper, SSH command or hooks inherited from the host. Refs retain
   original names/IDs. This safe private export, not untrusted source upload-pack/config, is the only
   clone transport; even preview never invokes source upload-pack/packObjectsHook.
   Initial sanitation does not prohibit the owner's later
   ordinary Git configuration or pushes; those are their own actions and can
   invalidate a managed verification receipt.

Managed Git calls use env from empty: recorded PATH, private HOME/TMPDIR,
GIT_CONFIG_NOSYSTEM=1, GIT_CONFIG_GLOBAL=/dev/null, GIT_CONFIG_COUNT=0,
GIT_NO_REPLACE_OBJECTS=1, GIT_OPTIONAL_LOCKS=0, GIT_TERMINAL_PROMPT=0;
fixed core.hooksPath=/dev/null, core.fsmonitor=false and maintenance.auto=false.
GIT_DIR/WORK_TREE/OBJECT_DIRECTORY/INDEX_FILE are assigned only to private admitted
readers, never inherited. Objects/configs remain ordinary user data outside managed
operations; this is not a hostile same-UID or same-Docker-user sandbox.

## Execution inputs, trust and verification after editing

Fingerprint actual admitted config/Dockerfile/context/hook/smoke/dependency bytes,
source/submodule identities, resolved substitutions and tool identities. Preserve
actual working bytes, not just HEAD. No-follow regular-file capture, symlink
resolution within admitted roots and repeated enumeration/content checks reject
inconsistent source capture. Private capture is coherent selected input, not a
single-instant host snapshot. Exclude host credential stores, sockets/devices and
ignored private files; an excluded required executable input refuses explicitly.
Generated host.env/smoke.env are not blindly imported as ignored executable input.

Canonical digest: `sha256:<64-lowercase-hex>` of UTF-8 canonical JSON, sorted keys
and path/type/mode/size/content-digest records, LF terminator. Normalize assigned
private roots only to $CAPTURE/$USERDATA/$DRIVER placeholders; preserve original
source identity and raw executable bytes. Operation/home IDs belong to allocation
intent. Unchanged preview/apply must produce equal source digests.

Trusted host initialization/build uses a separate captured execution tree, never
the mutable original or candidate clone. Its relative layout includes Anchorage's
initialize.sh and sibling BundleDock helper. CLI 0.89 awaits initialize before
resource creation; generated host.env is validated before container hooks/smoke.
Build uses captured Dockerfile/context. Stage leaves the primary running.

Actual Anchorage execution inventory: .devcontainer scripts/config/assets including
setup, greeting-info, prompt TOML/logo, pins-hash, smoke-container and its three
Makefile tests/fixture helpers; root Makefile/.gitignore/.gitmodules; BundleDock's
volume helper and referenced publication receipts/docs. Image/tool programs,
network build dependencies and generated home startup files remain explicit
trusted runtime dependencies, not a new hermeticity requirement. Unknown dependency
patterns need honest capability handling; arbitrary shell closure is not proved.

**Editing/receipt rule:** editing is allowed. A receipt verifies a particular
candidate ID/image/config/input version, never all future candidate work. Changes
to executable/config/build inputs or Git identities invalidate that verification.
Result-only refs/objects are distinct from code activation; no commit is checked
out into the original by promotion. The new primary keeps the original workspace
bind: fetched candidate commits are available for later owner integration, **not
automatically active working-tree code**. Preview states this distinction. Before promotion compare the current version
with the receipt and refuse stale verification before primary stop.

**Proposed refresh amendment:** `workspace stage --local --refresh --source ORIGINAL
[--smoke] [--dry-run] [--yes] [--trust DIGEST] PRIMARY` is reserved here for review,
not registered. It selects only the existing exact receipt-bound candidate (never
name-based adoption), previews its current clone input digest and requires fresh
explicit trust/consent. It reruns requested verification against that version only
if config/build/image/host-initializer inputs still equal the staged environment;
otherwise it requires a fresh stage after guarded cleanup, not implicit rebuild.
It runs no host hook and does not replace resources or change homes. Without
--refresh default stage collisions still refuse. Original source drift still
requires a new stage/trust. This is the concrete proposed exception to the current
“Changed source requires a new stage”/no-adoption interaction, not a quiet relaxation.

**Proposed bounded phase-binding amendment — cooperative quiescence:** retain
fingerprint trust and exact-candidate verification, and make the execution claim
conditional on the caller pausing ordinary writers to the listed admitted input
paths during each managed hook/build/smoke phase. This includes editor autosave,
Git checkout/rebase, formatters and background project writers; ordinary candidate
work remains available between operations. This explicitly amends the prior
unconditional captured-byte binding language; hashes do not freeze files and the
amendment is not already approved by D1/D2.

Literal phase rules for owner review:

1. Preview lists the trusted input version, hook/smoke argv, expected user/cwd,
   permitted generated outputs/home effects, deadline and no-concurrent-input-writer
   condition. Existing mutation confirmation acknowledges that operating condition;
   fingerprint trust stays separate. No new trust is inferred from --yes.
2. Immediately before launch, hash actual admitted input paths in the real phase
   view and require equality with the trusted execution manifest. Check exact
   container/image/source/mount/user identities. Mismatch before effects is 68;
   no source hook/smoke runs. Stage uses the writable clone, promote the unchanged
   original live workspace. No pathname rewrite or protected project-file overlay.
3. Execute official hooks or exact stdin smoke in that view under the quiescence
   condition. Trusted code generation, image tools/network dependencies and declared
   output/home writes retain their explicit trust; this is not hermetic execution.
4. Re-enumerate/hash the same inputs after the phase and before success or the next
   mutation; validate outputs separately. Detected source/identity drift, unexpected
   writes, cancellation or uncertain effects invalidate verification and return 73
   with phase/resources/recovery. Recheck before primary stop and each later phase.
5. Evidence is conditional: pre/post inputs match the trusted version **and** the
   cooperative no-writer assumption held. Hashes cannot detect a writer changing
   and restoring bytes mid-phase. Preview/receipt state this limitation; no hostile
   same-UID/Docker-owner or ordinary concurrent-writer guarantee is claimed.

Receipt separates original source binding, candidate Git/worktree version and the
execution-byte manifest; the previewed trust digest binds all three. Refresh may
update candidate identities/results with fresh trust without pretending the original
HEAD moved. Promote requires original binding unchanged and actual original hook
bytes equal to that trusted execution manifest. Candidate hook/config changes not
present in the original cannot become active merely through fetched refs; refuse
before replacement and retain the clone/results for owner integration/restaging.

This is the concrete proposed resolution without adding a privileged runner. If
binding despite concurrent writers is required, official CLI alone lacks a per-exec
alternate mount view; that stronger profile needs a demonstrated additional runtime
capability. Do not silently restore read-only project restrictions or call hashes
enforced snapshots. Exact-profile approval chooses this tradeoff before source;
implementation tests then exercise it rather than circularly preceding all code.

## Tool, endpoint, argv and output contract

Use official Dev Container CLI 0.89.0, trusted installed Node/Docker/Git/shell tools;
record actual absolute paths, versions and content digests, recheck before launch.
Retain existing Git >= 2.39.5 baseline; do not invent Node 26-only, Docker 27-only or
API 1.46 safety gates. Test actual official-CLI/runtime capability and report exact
unsupported capability. No npx, install, download fallback or new native runner.
Pin the physical admitted Unix socket and daemon ID throughout. Every Docker call
uses --host unix://SOCKET and empty private DOCKER_CONFIG; no ambient endpoint,
context auth, credential helper or forged remote provider admission.

CLI/host env from empty: recorded PATH, private HOME/TMPDIR/cache/user-data,
DOCKER_CONFIG and fixed DOCKER_HOST, LANG=C, LC_ALL=C, TZ=UTC; only explicit
nonsecret ANCHORAGE name/hostname/role/host-label/IP substitutions, with unset/empty
preserved. ContainerEnv/remoteEnv come from image plus trusted config, not host PATH.
Ordinary owner Git processes in the candidate are outside this managed env policy.

W=phase workspace, C=captured config, O=full override, D=private fixed-endpoint driver,
U=private user data, L=explicit ordered identity/role labels. Promote omits host
initialize from O; keeps receipt-bound source lifecycle contribution once, image
metadata contribution once and official origin order/object parallelism.

```text
devcontainer read-configuration --workspace-folder W --config C --override-config O
  --docker-path D --mount-workspace-git-root false --mount-git-worktree-common-dir false
  --log-format json --log-level info L --include-merged-configuration true
devcontainer up --workspace-folder W --config C --override-config O --docker-path D
  --mount-workspace-git-root false --mount-git-worktree-common-dir false
  --log-format json --log-level info L --user-data-folder U
  --skip-post-create true --skip-post-attach true --default-user-env-probe none
  --update-remote-user-uid-default never --omit-syntax-directive true
  --gpu-availability none --include-configuration true --include-merged-configuration true
devcontainer run-user-commands --workspace-folder W --config C --override-config O
  --docker-path D --mount-workspace-git-root false --mount-git-worktree-common-dir false
  --log-format json --log-level info L --user-data-folder U --container-id FULL_ID
  --skip-post-attach true --default-user-env-probe none
docker --host unix://SOCKET exec --interactive --user REMOTE_USER
  --workdir CONTAINER_WORKSPACE FULL_ID bash -s -- CONTAINER_WORKSPACE
```

Container hooks run through the official phase in stage **and promote**; no hook-free
promotion default. Preserve containerUser/remoteUser, home, env/mount precedence,
overrideCommand and all supported lifecycle kinds; postAttach remains suppressed
only because this operation does not attach, and preview says so. Source/image
metadata must not duplicate commands. UID-changing image extensions, build/pull/tag
on promote refuse; driver inserts --pull never in validated native run. Final
primary .Image must equal the verified immutable candidate ID. Candidate project
editing does not authorize a mutable image substitute.

Host initialize protocol for actual Anchorage: exact per-bundle `pull R`,
`image inspect --format {{.Id}} -- R`, `volume create V`, and
`run --rm --network none --mount type=volume,src=V,dst=/bundle,volume-nocopy IMAGE_ID`.
Bind inspected(R).Id to IMAGE_ID, retaining R's distinct OCI manifest/index digest;
never assert manifest digest equals image config ID. host-info additionally uses
its two exact docker info formats and context show as diagnostics, not routing.
The PATH driver binds all literal docker calls to the admitted endpoint. Owner
trust/preview covers pinned-image initializer effects including shared tool-volume
lock metadata writes; FR-067 removal protection remains absolute. This cooperative
routing does not sandbox the Docker owner or attest OCI contents against local code.

Output: omit preexisting host.env/smoke.env in execution input. Require fresh
host.env regular/no-follow/nlink 1/owner UID/max 64 KiB, seven unique producer-order
HOST_NAME/IP/OS/ARCH/ENGINE/ENGINE_ARCH/CONTEXT keys; values max 4096 UTF-8 bytes, no
controls. Parse producer POSIX single-quoted literals/quote escapes without eval;
reject extra shell syntax, duplicate keys and leftover temporary output. Record
size/hash/producer digest. No host smoke orchestrator runs.

Name host execution capture E, writable clone W and original workspace A. After
validating E/.devcontainer/host.env, publish exactly those validated bytes to
W/.devcontainer/host.env **before any candidate container hook/smoke**: preview the
candidate-only write, require target absence/no symlinks, use a private temporary
regular file (0644, nonsecret facts), fsync and atomic create-if-absent publication,
then remove the temporary name and reparse/hash W's file. No overwrite of candidate
work is implied. Journal producer operation/input digest, E output digest, W path/
digest and publication observation; this output becomes an explicit later-phase
input, separate from the pre-initialization fingerprint. Recheck before use.
Refresh requires the same receipt-bound W output; missing/changed output refuses
68 before verification and requires a fresh stage, never a host hook or republish.

For promote's original live bind, admit A/.devcontainer/host.env separately during
stage preview: the same literal-data validation and owner trust bind its exact
bytes/digest/provenance as an existing original output, not as E's fresh output.
Require it present/valid for this Anchorage greeting profile. Promote revalidates
that exact A output before primary stop and before container hooks; missing,
changed or invalid data refuses 68 before effects (73 after effects). Its admitted
facts are not a claim of fresh host probing. Never copy E/W output into A, run
host-info at promote or source unvalidated/stale-unbound shell bytes. Thus the
normal greeting path reads validated W data in candidate and validated A data in
primary without altering the original worktree. The stage trust manifest includes
the admitted A output digest. Its earlier producer is unknown: record null producer
fields and this operation's explicit admission instead of inventing a host-hook
receipt. Generated E/published W entries retain their known producer binding.

Bounds: metadata 5 s; capture/preflight 120 s; host-init 600 s; build/resources 1800 s;
container-hooks 600 s; health 120 s; smoke 600 s; stop 30 s; each removal 30 s; total stage 3600 s,
promote 1800 s. Each phase consumes remaining total. Child output max 1 MiB, metadata 4 MiB;
no raw output/env in receipt. TERM group, wait 2 s, KILL then inspect; killing a client
is not proof daemon effects stopped. Uncertain/partial effects are 73, no auto retry.

## Promotion fetch: add results without moving existing work

**Proposed authority amendment:** promotion may write Git objects and exclusively
new operation refs in the original repositories. Therefore it cannot promise all
original Git bytes remain unchanged. It never checkout/reset/merge/rebase, move an
existing branch/tag/ref, overwrite the original index/worktree, update FETCH_HEAD,
run a source hook or autonomously push. Original data changes are previewed exactly.

Before primary stop, freeze the candidate **ref/object export**, not its editable
worktree: capture HEAD plus all admitted heads/tags/notes results into a private
self-contained bare export, bounded and verified. Candidate writers can continue
on later versions; only captured results are transferred. Any change to the bound
execution version still invalidates runtime verification. Include every populated
submodule repository and map its export separately; this does not update original
gitlinks. Retain clone data for uncommitted/untracked work not transferable by Git.

Generate fixed explicit refspecs per exported ref, targeting fresh
refs/workharbor/stage/OP/REPO/{head,heads/NAME,tags/NAME,notes/NAME}. OP is 128-bit
random; REPO is a path digest. Preview exact original/export/ref/OID mappings and
objects to transfer. Reject any preexisting target ref 68, never force/update it.
Fetch uses an isolated generated Git directory/config and the original object
directory explicitly resolved, no source configuration/includes/rewrites/hooks/
helpers. Transport is the private local export only, no credential/network access:

```text
git -c core.hooksPath=/dev/null -c core.fsmonitor=false -c maintenance.auto=false
  fetch --atomic --no-write-fetch-head --no-tags --no-auto-maintenance
  --no-recurse-submodules -- EXPORT EXPLICIT_REFSPECS
```

Use no `+` refspec. Import objects first, then create each target ref with an
atomic create-only `update-ref --no-deref --stdin` transaction requiring absence;
**do not rely on fetch's absence precheck for exclusive ref creation**. Implement
fetch into an isolated private ref directory sharing only the admitted destination
object store, then publish original operation refs via that create-only transaction.
Publication's update-ref targets the physically resolved original Git directory;
its config may be parsed as data, but command overrides disable core.hooksPath,
core.fsmonitor, gc.auto and maintenance.auto. This command runs no transport,
credential helper, upload-pack or packObjectsHook; never invoke a source hook.
Its exact input is `start`, then `create REF OID` per operation ref, then `prepare`
and `commit`, LF-delimited; create requires the old ref absent. Reject symbolic
operation refs, including dangling ones, and keep --no-deref on the real publication
command so a concurrent symref cannot redirect creation into another owner ref.
No update/delete command or dereferencing fallback is admitted. A pre-import
collision is 68; a late transaction refusal after object import is partial 73.
Thus clone/push races cannot overwrite a just-created foreign ref. Multi-repository
publication is not globally atomic: journal each object's import/ref transaction,
report observed/remaining repositories on failure. Cancellation/uncertainty after
imports or any ref creation is 73, even if current branches/worktrees stayed intact.
Unreferenced imported objects may remain; do not GC/rollback or retry automatically.
All result refs are added before primary replacement; failure leaves primary running.

## Promote container hooks under the proposed phase contract

Promote uses the exact verified image and original primary home; never copies or
transplants candidate home. Fresh preview lists lifecycle hooks, trusted digest,
home effects, result-ref additions, original live workspace bind, pane loss and
cleanup consent. Hook-generated home files are normal project behavior, not a
migration guarantee. No host hook, rebuild or mutable-image resolution occurs.

The existing requirement binds every executed container hook to trusted inputs.
Under the proposed cooperative phase-binding amendment above, promote hashes the
actual original workspace dependency paths before/after official hooks against
the trusted execution manifest. setup.sh therefore executes at its normal path,
reads matching sibling assets/config and records original WORKSPACE while updating
the original home. No candidate-home transplant, alternate source mount,
intermediate primary or hook rewrite is needed. Drift before replacement refuses
68; detected drift after effects returns 73 and blocks cleanup/success. Fetched
refs do not activate changed working files.

Owner review concerns this explicitly conditional phase contract, not a claim that
mutable paths are immutable. A stronger enforced snapshot view is a different
runtime capability. Remaining implementation work is concrete: input/identity
checks, output/phase journals, exact official hook phases, drift/failure fixtures
and honest conditional verification reporting. Tests accompany source after
contract approval; native evidence later establishes compatibility.

## Allocation, durable intent and cleanup

State root proposed: `${XDG_STATE_HOME:-$HOME/.local/state}/workharbor/local-stage/v1`,
resolved once before constructing child env; require owner UID, 0700 directories,
0600 files, no symlink path components. Key daemon directories by sha256 of daemon
ID plus canonical endpoint. An OS advisory exclusive lock covers that daemon for
the whole operation; do not remove a lock file to steal a retained intent.
Dry-run creates neither persistent state nor receipt; temporary capture is removed.

Persist operation ID (random 128-bit lowercase hex), input manifest, trust/consent
record and full intended resources by exclusive create, fsync file and directory
**before** any mutation. Journal is monotonic sequenced records with previous-record
hash; flush/fsync intent before action and observation afterwards. Atomic receipt
replacement uses a fresh file, fsync, rename and parent fsync. An incomplete write,
unknown version or inconsistent hash chain blocks mutation, retaining resources.
Hashes detect accidental corruption, not tampering by the same local user.

New home name is `whr-stage-home-<32-lowercase-hex>` from 128 random bits, separate
from operation ID. No user/source name; no counter/retry after collision. Under
lock check absence of exact candidate name/role label, retained intent and volume;
write private allocation intent, recheck absence, then propose exact create:

```text
docker --host unix://SOCKET volume create --driver local
  --label workharbor.stage.operation=OP --label workharbor.stage.nonce=NONCE HOME_NAME
```

Docker volume create is idempotent, **not an atomic exclusive claim**. A successful
reply/name/prefix proves no ownership. Inspect and require matching operation,
nonce, driver, creation observation and no unexpected mounts; otherwise retain
and refuse (73 after attempted create). No adoption/deletion of a competing volume.
This closes cooperative WorkHarbor retries only; a same-Docker-user client can
race create or forge labels. Claiming stronger freshness requires a different
allocation primitive and is outside this profile. Before use/deletion require
private intent plus fresh metadata correlation; absence of intent is never repaired
from labels. Distinct primary-home identity is mandatory before candidate create.

Journal states: planned → allocating → candidate_resources → candidate_hooks →
verifying → staged_verified or staged_unverified → exporting_results →
importing_results → promoting → primary_resources →
primary_verified → candidate_removed → home_removed → complete.
Each action records intent/observed/uncertain independently. An uncertain transition
is retained and blocks fresh staging; rerun reports it, never reruns hooks/builds.
Only a receipt-bound verified candidate can enter promoting.

Before primary stop, recheck endpoint, exact primary/candidate/image IDs, source
manifest, original home, effective users/mounts/env, verification freshness and
**entire** candidate cleanup plan. Apply requires explicit --volumes --force and
fresh confirmation/--yes; missing either deletion option is 70. Dry-run with neither
flag returns preview 0 listing required apply consent; volumes alone is 70; both
preview successfully with zero effects. --yes synthesizes neither flag.

Keep every FR-067 guard including container-only refusal for staging/temp
`anchorage-home-*`; never rename/adopt current Anchorage candidates. Check all
running/stopped mounts before each removal: reject primary/shared/foreign/read-only
homes, bundledock-* and `/opt/bdock` descendants. Candidate removal precedes home
removal, only after new-primary verification. Reinspect between actions. Late
sharing or uncertainty yields 73; no unsafe rollback, deletion retry or stop/restart
of the old primary is automatic. Original primary home is never a cleanup target.

## Closed result, error and recovery proposal

All proposed objects below are closed: exactly listed required keys; explicitly
nullable fields admit null values, not omitted keys. Unknown fields refuse. IDs/digests use the formats
above; host paths max 4096 bytes, strings max 1024 unless specified. Public text is
escaped; no secret, raw env, script body, child transcript or remote operation_id.
Keep existing schema_version=1 envelope and error {code,message,remediation?}.
Future source PR adds schemas/command table together, not in this docs PR.

| Object | Exact proposed fields / types |
|---|---|
| stageResult/promoteResult | status enum(preview,staged_verified,staged_unverified,promoted,refused,partial); phase enum(journal states plus preflight); recovery_id nullable string; fingerprint nullable digest; primary nullable resource; candidate nullable resource; home nullable resource; verification nullable verification; effects array(effect), max 256; recovery array(step), max 32; repositories array(repository), max 33; result_refs array(resultRef), max 1024 |
| resource | kind enum(container,volume,image); name nullable string; id nullable string (container full64hex, image sha256, volume name); daemon_id string |
| repository | source_path string; clone_path string; head_oid string; object_format enum(sha1,sha256); dirty bool; uncommitted_retained bool |
| resultRef | repository_path string; export_ref string; target_ref string; oid string; state enum(planned,created,uncertain,remaining) |
| verification | policy enum(health,smoke,health_and_smoke); status enum(not_requested,missing,passed,failed,stale); binding_mode const(cooperative_quiescence); quiescence enum(caller_acknowledged,not_acknowledged); pre_input_digest digest; post_input_digest nullable digest; image_id digest; input_digest digest; observed_at UTC timestamp; output_digest nullable digest |
| repositoryBinding | source_path string; git_dir string; common_dir string (physically resolved original repository paths); object_format enum(sha1,sha256); original_head_oid oid; binding_digest sha256 digest |
| gitObjectsTarget | kind const(git_objects); repository repositoryBinding; export_path string; object_manifest_digest sha256 digest; object_count integer 0..1000000 |
| refCreate | name string (valid full operation ref, no control bytes); oid oid; expected_absent const(true) |
| gitRefsTarget | kind const(git_refs); repository repositoryBinding; creates array(refCreate), max 1024 |
| cloneOutputTarget | kind const(clone_output); clone_path string; relative_path const(.devcontainer/host.env); digest sha256 digest; producer_operation_id 32-lowercase-hex string |
| outputEntry | path string; origin enum(generated_E,published_W,admitted_A); size integer 0..65536; digest sha256 digest; producer_operation_id nullable 32-lowercase-hex string; producer_input_digest nullable sha256 digest; admission_operation_id 32-lowercase-hex string; admission_input_digest sha256 digest |
| effectTarget | exactly one of resource, gitObjectsTarget, gitRefsTarget or cloneOutputTarget, discriminated by kind; each variant is closed |
| effect | action enum(create_volume,host_initialize,publish_candidate_output,build,create_container,container_hooks,smoke,import_objects,create_result_refs,stop_primary,remove_primary,create_primary,remove_candidate,remove_home); target effectTarget; state enum(planned,attempted,observed,uncertain,remaining) |
| step | action enum(inspect_retained,restage_after_cleanup,run_stage_with_smoke,remove_candidate_with_guarded_rm,owner_reconcile); target nullable effectTarget; instruction string |

OID is exactly 40 lowercase hex for sha1 or 64 for sha256, matching its repository.
Git import effects require gitObjectsTarget; ref publication requires gitRefsTarget;
candidate output publication requires cloneOutputTarget; Docker effects require
resource. The object manifest is a private sorted OID/size inventory bound by its
digest, not an arbitrary payload. Full original repository binding and create OIDs
remain in the journal/receipt so Git effects have no Docker-only target gap.

Private receipt adds exactly version=1, operation_id, sequence, previous_hash,
state, endpoint_identity, original_source, input_manifest_digest, tool_manifest,
config_digest, image_metadata_digest, effective_config_digest, output_inventory,
origin_binding, candidate_git_version, execution_manifest_digest, phase_binding_mode,
clone_repositories, exported_refs, imported_objects, result_ref_transactions,
primary/candidate/home resource identities, trust_digest, consent flags,
verification and action inventory. Tool manifest entries are path/version/digest;
output_inventory is array(outputEntry), max 100. Only admitted_A may have null
producer fields; all entries retain the admitting operation/input binding.
Detailed byte manifests remain
private referenced artifacts, not arbitrary embedded JSON or public env dumps.
Recovery ID is daemon-key/operation-id, never a fabricated remote operation ID.

Success 0 requires operation completion or valid preview. If no healthcheck and
stage omits --smoke, retain the candidate as **staged_unverified**, return 73
partial_failure with explicit missing verification and recovery. This is not a
successful stage or a promotion ticket. Proposed explicit recovery: guarded local
rm of that receipt-bound candidate/home, then stage --smoke with fresh capture,
trust and consent. A rerun never adopts it or silently runs smoke. This proposal
adds no verify/resume flag or orphan-volume deletion API. Lost receipts/partial
allocation retain and report resources for owner reconciliation.

Before effects: missing trust/consent 70 needs_human; binding/capability/collision/
verification refusal 68 conflict; deadline 71 timeout; interrupt 72 interrupted.
After attempted effects with any retained/uncertain resource, 73 partial_failure
supersedes those categories and carries attempted/observed/remaining inventory.
Selection/usage 64, absent labelled primary 65 and internal 74 retain ADR0013 mapping.
Timeout diagnostics identify phase/local target, never invent remote operations.
No receipt is evidence of success; no receipt is also not evidence of no effects.

## Restriction review against the owner's priorities

| Constraint | Relevant issue / keep or drop |
|---|---|
| Read-only project / synthetic Git | Drop: defeats requested editing/full-history workflows; not required to protect host credentials. |
| Normal commits/rebases/owner pushes | Keep enabled: ordinary user activity. whr never autonomously pushes or supplies credentials. |
| Host config/hooks/credentials copied wholesale | Keep sanitation: avoids executing/relaying ambient host authorization; real Git objects/history are retained. |
| No overwrite on result fetch | Keep: existing owner work/refs must survive; create-only operation refs, no checkout/FETCH_HEAD writes. |
| Fresh trust on changed executed bytes | Keep D1: prevents stale-source approval; refresh and cooperative phase binding are explicit proposed amendments, never hashes-as-freeze claims. |
| Original source unchanged during candidate work | Keep: candidate is independent clone; promotion's explicit object/ref additions are separately previewed. |
| Zero promote container hooks | Drop: conflicts with owner priority; preserve trusted project hooks on original home. |
| No promote host hooks / immutable image | Keep settled semantics: no unexpected host execution or rebuilding an unverified image. |
| Arbitrary privilege/runArgs/features bans | Drop blanket security claims against Docker owner. Admit actual official-CLI semantics with explicit effects; report real unsupported resource/ownership/capability cases. |
| Version-only Node 26/Docker 27/API gates | Drop invented thresholds; retain pinned companion and actually needed/tested capabilities. |
| Bounded files/objects/deadlines | Keep resource/latency limits, never silently truncate history or discard dirty work; tune with measured intended-tree evidence. |
| Unix endpoint/owner/home/deletion guards | Keep existing native boundary and FR-067; labels remain usability, not authorization. |
| Hostile same-UID/Docker-user isolation | Drop claim: owner already has Docker exec. No new generic sandbox or external-tool hermeticity prerequisite. |

## Evidence and remaining gate

Source-only basis: official CLI v0.89.0
[options](https://github.com/devcontainers/cli/blob/v0.89.0/src/spec-node/devContainersSpecCLI.ts),
[initialize ordering](https://github.com/devcontainers/cli/blob/v0.89.0/src/spec-node/configContainer.ts),
[metadata](https://github.com/devcontainers/cli/blob/v0.89.0/src/spec-node/imageMetadata.ts)
and [unchanged-image/UID paths](https://github.com/devcontainers/cli/blob/v0.89.0/src/spec-node/containerFeatures.ts).
Read-only Anchorage graph at observed umbrella HEAD 28a9662f61ab6b8b2853bdc7453b78685ed63f64:
[config](https://github.com/wstein/anchorage-workspace/blob/28a9662f61ab6b8b2853bdc7453b78685ed63f64/.devcontainer/devcontainer.json),
[setup](https://github.com/wstein/anchorage-workspace/blob/28a9662f61ab6b8b2853bdc7453b78685ed63f64/.devcontainer/setup.sh),
[smoke](https://github.com/wstein/anchorage-workspace/blob/28a9662f61ab6b8b2853bdc7453b78685ed63f64/.devcontainer/smoke-container.sh).
No runtime proof follows. Tests must cover real clone/history/index/dirty submodules,
normal Git operations, changed-input refresh, detected drift/conditional reporting, exact official
phases, hook origin/order/path semantics, create-only multi-repo result refs,
FETCH_HEAD/index/worktree preservation, transfer cancellation and all FR-067 cases.
Owner approval concerns this concrete revised profile/amendments, not reapproval
of D1/D2/naming/deletion. Exact-profile acceptance of the cooperative phase-binding amendment, independent
review and required checks remain pending; native Linux/macOS evidence later establishes support.
Full T038/T039 dependencies/checkmarks and all live-operation gates are unchanged.
