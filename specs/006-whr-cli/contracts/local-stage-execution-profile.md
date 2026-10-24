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
Clone/result-transfer and conditional phase binding are **proposed
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

Clone root is `STATE_ROOT/DAEMON_KEY/OP/workspace/BASENAME`, where STATE_ROOT is
the private state root below, DAEMON_KEY is the daemon/endpoint digest, OP is the
operation ID and BASENAME is the validated original basename. Populated submodule
clones stay at their recorded relative paths inside that root. Require caller UID
ownership, 0700 administrative/clone-root directories, private metadata files 0600,
and captured working-file modes (including executable bits); receipt repositories
bind each physical clone_path, owner/mode and container mount mapping. Native
admission must establish that the expected container user can use this read-write
clone without changing the original source's ownership or permissions.

On macOS the physical clone root must be reachable through the admitted daemon's
already-configured file-sharing roots; do not assume every XDG_STATE_HOME or /tmp
path is shared. Validate actual platform/path reachability, or refuse unsupported
mount capability 68 before candidate effects. Never change Docker/OrbStack sharing
settings, strip /private prefixes or silently relocate the clone. Native acceptance
must exercise this exact state-root mount; source fixtures are not that evidence.

Report retained clone_path and working/object bytes after success or failure.
Container/home cleanup never removes this tree, even when apparently clean. The
explicit removal path is the owner's manual filesystem removal of that exact
receipt path after inspecting status, refs, transferred results and any remaining
uncommitted/untracked work; discarding such work requires their deliberate choice.
Do not follow a changed/symlinked path, silently delete dirty data, infer consent
from volume flags or invent a whr clone-removal command. Preserve the receipt/ref
mapping as the recovery record after the owner removes project data.

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
Ignore status alone is not a secret classification or blanket exclusion: bounded
noncredential ignored working data (including caches when explicitly inventoried)
may be admitted in preview and retained in the clone. List all omitted optional
data; never silently omit a required input. Credential/generated-output rules still
apply to .env/credential stores and host.env/smoke.env respectively.
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
to actual executable/config/build/smoke/dependency inputs invalidate verification.
**Result-only Git policy clarification:** original-source provenance, mutable
candidate result refs and executed Git inputs are different bindings. A normal
candidate commit/rebase or HEAD/ref move alone does not invalidate unchanged
image/mount/user/hook/smoke/dependency verification. This preserves the owner's
normal Git work plus result-fetch direction; it does not attest a later execution
version. If a Git identity/state actually supplies execution input (for example a
version derivation, conditional hook or dependency selection), that identity/state
remains in the executed-input manifest and a change refuses promotion, requiring
guarded cleanup/fresh stage. A check accepting any valid Git status is not evidence
for one frozen HEAD; record its actual predicate rather than invent broader proof.
Result-only refs/objects are distinct from code activation; no commit is checked
out into the original by promotion. The new primary keeps the original workspace
bind: fetched candidate commits are available for later owner integration, **not
automatically active working-tree code**. Preview states this distinction. Before promotion compare the current version
with the receipt and refuse stale verification before primary stop.

**Existing-candidate recovery:** this contract defines no rerun-verification,
re-entry or re-attestation interface for an existing candidate. Missing/failed
verification, changed trusted inputs or stale receipt bindings prevent promotion.
Retain candidate project data; known-safe recovery is guarded candidate/container-
home cleanup under unchanged FR-067, followed by a fresh stage with fresh capture,
trust and required smoke. Cleanup never deletes the retained writable clone or
uncommitted work. Lost receipts, uncertain phases and resource drift require owner
reconciliation; never adopt resources or retry uncertain effects.

A refresh interface is only a possible future owner proposal requiring separate
approval; no such flag, workflow or recovery promise is part of this contract.

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

Receipt separates original source binding, candidate Git/worktree result version
and executed-input manifest. Stage trust binds original provenance and all actual
executed inputs. Fresh promotion preview/confirmation binds the exact exported
result refs/OIDs and names the unchanged stage-trusted execution manifest; --yes
does not authorize changed source execution or a new verification version. Result
versions are captured for transfer, not used to rewrite stage evidence. Promote requires
original binding unchanged and actual original hook
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
DOCKER_CONFIG and fixed DOCKER_HOST; admit and record the caller's nonsecret
LANG/LC_ALL/TZ values (including unset) rather than force a locale/timezone; only explicit
nonsecret ANCHORAGE name/hostname/role/host-label/IP substitutions, with unset/empty
preserved. ContainerEnv/remoteEnv come from image plus trusted config, not host PATH.
Ordinary owner Git processes in the candidate are outside this managed env policy.

W=phase workspace, C=config, O=full replacement override, D=fixed-endpoint driver,
U=private user data, L=explicit ordered identity/role labels. Stage C/O derive from
the captured admitted config. For **every promote invocation** set C=O=P, where P
is a complete private receipt-derived `promote/devcontainer.json`: image is the
verified immutable sha256 image ID, workspaceMount binds the original source,
home/mounts/users/env/overrideCommand are the admitted primary values. P contains
no build/dockerFile/dockerComposeFile/service, Features/additional Features,
initializeCommand or UID-extension request; updateRemoteUserUID=false and
--update-remote-user-uid-default never apply **on promote only**. Stage retains the
configured/official UID update behavior, including its image extension when needed;
record the resulting user IDs/image for verification. Never pass the original build config as C.
Hash/store P in the receipt; recheck its bytes before each phase. No omission in
a delta override is treated as deletion of a source property. P retains each
trusted source lifecycle contribution once, accounting for contributions already
present in inspected image metadata, with official origin/order/object parallelism;
never flatten merged hooks back into source fields or deduplicate distinct origins
merely because command strings match.

```text
devcontainer read-configuration --workspace-folder W --config C --override-config O
  --docker-path D --mount-workspace-git-root false --mount-git-worktree-common-dir false
  --log-format json --log-level info L --include-merged-configuration true
devcontainer up --workspace-folder W --config C --override-config O --docker-path D
  --mount-workspace-git-root false --mount-git-worktree-common-dir false
  --log-format json --log-level info L --user-data-folder U
  --skip-post-create true --skip-post-attach true --omit-syntax-directive true
  --include-configuration true --include-merged-configuration true
devcontainer run-user-commands --workspace-folder W --config C --override-config O
  --docker-path D --mount-workspace-git-root false --mount-git-worktree-common-dir false
  --log-format json --log-level info L --user-data-folder U --container-id FULL_ID
  --skip-post-attach true
docker --host unix://SOCKET exec --interactive --user REMOTE_USER
  --workdir CONTAINER_WORKSPACE FULL_ID bash -s -- CONTAINER_WORKSPACE
```

Only promote up adds `--update-remote-user-uid-default never`. User-env probing
follows trusted configuration/official defaults, including login-shell behavior;
GPU negotiation follows admitted configuration and actual official-CLI capability.
Record effective results; no blanket none restriction or host-env inheritance.

Container hooks run through the official phase in stage **and promote**; no hook-free
promotion default. Preserve containerUser/remoteUser, home, env/mount precedence,
overrideCommand and all supported lifecycle kinds; postAttach remains suppressed
only because this operation does not attach, and preview says so. Source/image
metadata must not duplicate commands. UID-changing image extensions, build/pull/tag
refuse only on promote; driver inserts --pull never in validated native run. Final
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
Later use requires the same receipt-bound W output; missing/changed output refuses
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
primary without whr's host-facts publication writing the original worktree. Trusted
container hooks retain their separately previewed read-write access. The stage trust manifest includes
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
original Git bytes remain unchanged. **whr's own Git transfer operations** never
checkout/reset/merge/rebase, move an existing branch/tag/ref, overwrite the original
index/worktree, update FETCH_HEAD, run a source hook or autonomously push. This is
not a guarantee that owner-trusted project container hooks leave the worktree
untouched; their ordinary read-write/runtime effects are previewed separately.

Before primary stop, freeze the candidate **ref/object export**, not its editable
worktree: compare candidate HEAD/heads/tags/notes with the exact captured original
ref baseline recorded at clone construction. Export **only new or moved refs**
(including HEAD only when its OID changed), with their required object closure,
into a private self-contained bare export, bounded and verified. Unchanged refs
and deletions create no operation refs or owner-ref changes. Candidate writers can continue
on later versions; only captured results are transferred. Any change to the bound
execution version still invalidates runtime verification; a result-only ref move
does not. Original source correspondence/identities stay bound to stage. Include every populated
submodule repository and map its export separately; this does not update original
gitlinks. Retain clone data for uncommitted/untracked work not transferable by Git.

Candidate export treats the writable clone as an **untrusted repository**, using
the same generated-empty-Git-directory reader as original capture, not Git run in
the candidate's own administration directory. Require the receipt-bound physical
admin/object/index paths; reject symlinks in administration paths, changed Git-dir
pointers, objects/info/alternates and replacement refs. Read candidate config only
as inert data when needed for an admitted value; never load it into the reader.
No candidate includes, alternateRefsCommand, SSH/filter/fsmonitor/maintenance/
packObjectsHook or credential helper executes; no candidate upload-pack runs.
Resolve textual HEAD only to a validated in-repository ref, rejecting cycles and
other symbolic export refs; never follow a ref-file symlink. Verify every exported
object ID/content, bound expanded object bytes/count and reject inconsistent reads
before destination imports. Only the verified self-contained export may serve as
the subsequent local fetch source; it retains no candidate alternates or hardlinks.

Generate fixed explicit refspecs per exported ref, targeting fresh
refs/workharbor/stage/OP/REPO/{head,heads/NAME,tags/NAME,notes/NAME}. OP is 128-bit
random; REPO is a path digest. Preview exact original/export/ref/OID mappings and
objects to transfer. In the human preview, label each exported OID as matching
that repository's recorded stage-verification HEAD or as an unverified result,
without presenting fetched results as verified code.
Reject any preexisting target ref 68, never force/update it.
Count the complete planned result refs across root/submodules before any Git
import or other promote effect: more than 1024 refuses 68, never truncate or start
a partial transfer. Zero changed refs is a valid empty transfer. Persist the
baseline comparison and exact planned mappings so retries cannot export later work.
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
Operation refs are retained without automatic expiry/GC. After inspecting and
integrating results, the owner may remove only the explicitly listed operation
refs using their own Git tooling, e.g. `git update-ref --no-deref -d REF EXPECTED_OID`
per receipt mapping. Changed OIDs refuse deletion; never delete owner branches/tags,
prune a namespace blindly or treat volume cleanup as Git-ref cleanup. No automatic
ref cleanup or new whr command is implied.

## Promote container hooks under the proposed phase contract

Promote uses the exact verified image and original primary home; never copies or
transplants candidate home. Fresh preview lists lifecycle hooks, trusted digest,
home effects, result-ref additions, original live workspace bind, pane loss and
cleanup consent. Hook-generated home files are normal project behavior, not a
migration guarantee. No host hook, rebuild or mutable-image resolution occurs.
Preview explicitly states that trusted container hooks have read-write access to
the original workspace and their admitted mounts, plus the container network, as
at normal primary creation. They may write ordinary files outside the admitted
execution-input set; input rechecks do not inventory or prevent all such writes.
Their source trust and disclosed effects remain separate from whr's non-overwriting
Git transfer guarantee. No unexpected write is silently described as zero effects.

The existing requirement binds every executed container hook to trusted inputs.
Under the proposed cooperative phase-binding amendment above, promote hashes the
actual original workspace dependency paths before/after official hooks against
the immutable **container-hook manifest actually executed at stage**, including
argv, script/dependency bytes and origin order. No later candidate inspection can
replace that record with unexecuted hooks. setup.sh therefore executes at its normal path,
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

Before entering exporting_results, complete fresh source/fingerprint trust checks,
all mutation/deletion consent (--volumes plus --force and confirmation/--yes), the
entire FR-067 cleanup preflight, result-ref count/collision checks and exact
resource/verification rechecks. This ordering precedes export/import/object/ref
effects, not merely primary stop. Missing consent is 70 and inadmissible cleanup
is 68 with zero promote effects. Recheck again before import and primary stop;
late drift after an applied effect follows partial-73 rules, never unsafe cleanup.

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
| stageResult/promoteResult | status enum(preview,staged_verified,staged_unverified,promoted,refused,partial); unverified_reason nullable unverifiedReason; phase enum(journal states plus preflight); recovery_id nullable string; fingerprint nullable digest; primary nullable resource; candidate nullable resource; home nullable resource; verification nullable verification; effects array(effect), max 256; recovery array(step), max 32; repositories array(repository), max 33; result_refs array(resultRef), max 1024 |
| unverifiedReason | enum(missing_smoke,failed_smoke,failed_health,stale_candidate_inputs); no free-form value |
| resource | kind enum(container,volume,image); name nullable string; id nullable string (container full64hex, image sha256, volume name); daemon_id string |
| repository | source_path string; clone_path string; clone_owner_uid nonnegative integer; clone_root_mode const(0700); container_workspace_path string; head_oid string; object_format enum(sha1,sha256); dirty bool; uncommitted_retained bool |
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
unverified_reason is a required result/receipt key: non-null unverifiedReason
exactly when result status/receipt state is staged_unverified, null otherwise.
Known verification failures persist their precise reason for inspection/recovery;
no reason value authorizes existing-candidate verification or state re-entry.
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
verification, unverified_reason and action inventory. Tool manifest entries are path/version/digest;
output_inventory is array(outputEntry), max 100. Only admitted_A may have null
producer fields; all entries retain the admitting operation/input binding.
Detailed byte manifests remain
private referenced artifacts, not arbitrary embedded JSON or public env dumps.
Recovery ID is daemon-key/operation-id, never a fabricated remote operation ID.

Success 0 requires operation completion or valid preview. If no healthcheck and
stage omits --smoke, retain the candidate as **staged_unverified**, return 73
partial_failure with explicit missing verification and recovery. This is not a
successful stage or a promotion ticket. Explicit recovery is guarded local
candidate/container-home removal followed by fresh stage --smoke and fresh trust/
consent. Preserve the retained clone and all uncommitted/untracked work; the owner
can inspect/integrate it with their own Git tools. A rerun never adopts an existing
candidate or silently runs smoke. No resume/reverification/orphan-deletion API is
defined. Lost receipts/partial allocation retain and report resources for owner
reconciliation.

Before effects: missing trust/consent 70 needs_human; binding/capability/collision/
verification refusal 68 conflict; deadline 71 timeout; interrupt 72 interrupted.
After attempted effects with any retained/uncertain resource, 73 partial_failure
supersedes those categories and carries attempted/observed/remaining inventory.
Selection/usage 64, absent labelled primary 65 and internal 74 retain ADR0013 mapping.
Timeout diagnostics identify phase/local target, never invent remote operations.
No receipt is evidence of success; no receipt is also not evidence of no effects.

## Restriction review against the owner's priorities

| Restriction | Threat | Prevents | Keep/Drop/Settled |
|---|---|---|---|
| Read-only candidate project | None justifying a permanent ban | Ordinary project editing | Drop; owner requires writable clone. |
| Synthetic Git IDs/history | No credential threat requires altered history | Normal commits/rebases/history | Drop; preserve real full-history objects. |
| Ban on owner Git pushes/configuration | None; owner action is outside managed Git | Normal user Git work | Drop; owner may configure/push. |
| Autonomous whr remote push | Unrequested remote mutation/auth use | Publishing owner code without instruction | Keep; result fetch is local only. |
| Copy host config/hooks/helpers wholesale | Ambient command execution/credential relay | Host authorization entering candidate/managed export | Keep sanitation; objects/history remain. |
| Ignored private/credential working files | Secret copying across host/container boundary | Implicit transfer of .env/credential stores | Keep explicit exclusion; required excluded inputs refuse. |
| Blanket exclusion of nonprivate ignored data | No threat follows from Git ignore status alone | Useful bounded caches/data in clone | Drop blanket ban; inventory admitted/omitted data. |
| Existing generated host.env/smoke.env as executable input | Unvalidated shell syntax/stale provenance | Sourcing unbound generated bytes | Keep explicit validation/publication/admission rules. |
| Candidate admin symlinks/alternates/executable config on export | Container-to-host command/path redirection | Host commands and foreign object-store reads | Keep config-free bounded reader; normal user Git remains possible. |
| whr checkout/reset/merge/existing-ref/FETCH_HEAD writes | Owner work overwritten by transfer | Activating candidate code or moving owner refs | Keep create-only result refs; trusted hook effects are separate. |
| Fresh digest trust on changed executed inputs | Stale source approval | Untrusted replacement hook/test bytes | Settled D1; fresh stage for changed actual inputs, not result-only ref moves. |
| Reusing verification after changed hooks/environment | Verification of setup that never ran | Rebinding a completed stage to new hooks | Keep; guarded cleanup and fresh stage must execute changed hook inputs. |
| Original source binding must match | Wrong workspace or stale execution version | Promotion against unrelated/changed original | Settled interaction; results do not change original checkout. |
| Zero promote container hooks | None justifying the owner-rejected ban | Normal project lifecycle | Drop; preview/run trusted project hooks. |
| Promote host hooks | Unexpected additional host execution | Reinitializing host resources during promote | Settled no-host-hook direction. |
| Promote image build/pull/tag/UID extension | Replacing verified image with different bytes | Implicit rebuild or UID-derived new image | Settled immutable-image requirement; keep promote-only UID never. |
| Stage UID-update prohibition | No general threat; different host UID can need it | Official UID mapping/writable clone ownership | Drop; honor config/official default and verify resulting image/user. |
| Forced user-env-probe none | No general threat in owner-trusted container shell | Configured/login-shell hook environment | Drop; honor configured/official behavior and record it. |
| Forced GPU availability none | No identified threat | Admitted GPU capability | Drop; use actual CLI/provider capability, no silent approximation. |
| postAttach suppression | None; this operation has no attach event | Running attach-only lifecycle code at the wrong event | Keep proposed event semantics, not a security or settled decision. |
| Cooperative no-input-writer phase condition | Unobserved concurrent executable-byte changes | Claiming exact inputs from hashes alone | Keep proposed conditional-binding assumption; not enforced isolation. |
| Fixed C/UTC host locale/timezone | No identified threat | Caller locale-dependent host facts | Drop forced values; record admitted nonsecret locale/timezone. Container locale stays configured. |
| Credential-free initial remote URLs | Embedded host authentication leakage | URL credentials copied into clone | Keep initial sanitation; owner may later configure their own auth. |
| One fixed candidate per daemon | Collision/ambiguous lifecycle ownership | Multiple candidates bypassing fixed selector | Settled FR-068. |
| Adoption/retry of foreign or uncertain attempts | Wrong-resource mutation/duplicate effects | Name-based ownership inference | Settled no-adoption/retry and durable operation boundaries. |
| File/object/count/depth bounds | Unbounded storage/resource consumption | Unbounded capture without truncating history | Keep proposed explicit caps; over-limit refuses before effects. |
| Phase/total deadlines and output caps | Unbounded execution/output resource use | Indefinite operations/raw-output retention | Keep proposed bounds with honest partial outcomes. |
| Local Unix endpoint and owner scope | Endpoint drift/foreign resource mutation | Implicit remote auth/routing | Settled local admission boundary. |
| Protected/shared/foreign home refusal | Data loss | Removing primary/tool/shared data | Settled FR-067 in full, including Anchorage prefix. |
| Explicit --volumes --force and confirmation | Unconsented home deletion | Cleanup inferred from --yes/trust alone | Settled deletion-consent direction. |
| Private state ownership/modes/no symlinks | State path substitution/accidental disclosure | Misbinding local intent/receipt/resources | Keep local integrity boundary; no hostile same-UID claim. |
| Retain clone until explicit owner removal | Loss of uncommitted/untransferred work | Cleanup silently deleting project results | Keep data-preservation policy, not an extra operation gate. |
| CLI 0.89 and existing Git baseline | Unsupported command/contract behavior | Claiming untested tool semantics | Keep actual pinned/baseline contract; no invented Node/Docker thresholds. |
| Fixed minimal managed host environment | Ambient auth/endpoint/config inheritance | Undisclosed host execution inputs | Keep explicit allowlist; container env follows config. |
| Blanket privilege/runArgs/Features ban | No general threat against Docker owner | Admitted official Dev Container semantics | Drop; report actual capability/ownership limits. |
| Hostile same-UID/Docker-user isolation/hermetic tools | No such product boundary | Owner Docker exec or trusted external dependencies | Drop claim; do not invent a new sandbox prerequisite. |

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
candidate export with malicious config/includes/alternateRefsCommand/packObjectsHook,
external alternates, symlinked admin paths and symbolic/replace refs (zero host
commands or foreign-object reads; bounded verified export only),
normal Git result-only commits/ref moves without false invalidation, Git identities
that participate in execution refusing on change, changed-input refusal, retained dirty clone/fresh-stage recovery,
detected drift/conditional reporting, exact official
phases, hook origin/order/path semantics, create-only multi-repo result refs,
FETCH_HEAD/index/worktree preservation, transfer cancellation and all FR-067 cases.
Owner approval concerns this concrete revised profile/amendments, not reapproval
of D1/D2/naming/deletion. Exact-profile acceptance of the cooperative phase-binding amendment, independent
review and required checks remain pending; native Linux/macOS evidence later establishes support.
Full T038/T039 dependencies/checkmarks and all live-operation gates are unchanged.
