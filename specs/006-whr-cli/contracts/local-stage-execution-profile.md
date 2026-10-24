# Proposed local stage/promote execution profile

Status: **engineering proposal, S0 incomplete**, 2026-10-24. This is the
reviewable technical prerequisite for Package B, not product registration,
implementation, tool installation, a runtime experiment or owner approval.
The [interaction contract](local-stage-promote.md), [LS-1](../spec.md#ls-1-owner-direction-2026-10-24),
[home decision](candidate-home-cleanup-decision.md) and
[cleanup task proposal](candidate-home-cleanup-task-proposal.md) retain their
separate authority. Owner direction to implement Package B does not establish
approval of this exact execution profile. S0 remains gated on independent review
and exact-profile owner approval; the choices below are proposed, not accepted.
No task entry, dependency, checkbox, FR-067 protection or D1/D2 decision changes.

## Proposed workspace semantics and admitted capabilities

**Profile choice proposed for explicit owner approval:** stage uses a private,
bounded mirror of the whole admitted workspace and populated submodules, retaining
its basename and layout. `${localWorkspaceFolder}` resolves to that mirror;
`${localWorkspaceFolderBasename}` retains the original basename. The mirror is
mounted **read-only** at the original container workspace target. Candidate source
editing is prohibited; candidate home and private temporary storage remain writable.
Edits to the original after capture are absent; no candidate file is copied back.
Promotion restores the original source bind and original primary home and executes
zero host or container lifecycle hooks. Mirror/synthetic-Git semantics and this
initial hook-free promotion are explicit proposed owner profile choices; neither
is transparent configuration reuse or a committed-files-only restriction.

The original physical source path and primary ID remain identity inputs. Snapshot
labels never select a primary. Source correspondence, exact selector admission,
local Unix socket admission and one fixed candidate per daemon follow the
interaction contract. Linux and macOS/OrbStack remain required future native
acceptance targets; neither has been verified by this document.

| Surface | Proposed admission / refusal before effects |
|---|---|
| Configuration | One local JSONC devcontainer document; preserve supported fields in a full override. Reject duplicate keys, unknown executable/resource fields and unresolved substitutions. Editor customizations are inert retained data. |
| Image/build | Local Dockerfile/context inside capture, or image reference. Record Dockerfile/context bytes and effective image ID. Stage may build/pull after trust; network, apt and mutable base images are explicitly trusted external dependencies, not reproducibility claims. |
| Build expansion | No Compose, Features/additional Features, external context, SSH/secret mount, remote ADD, arbitrary frontend or build context outside the mirror. Unsupported syntax refuses 68; do not silently omit it. |
| Users | Preserve distinct containerUser and remoteUser, effective image defaults, numeric identities and home target; home only `/home/captain` or `/home/vscode`. UID rewriting disabled in both phases; a profile needing UID extension refuses. |
| Mounts | One read-only mirror workspace bind during stage (original live bind on promote), one home, digest-bound BundleDock volumes read-only with volume-nocopy. Preserve target/options and reject duplicate/conflicting targets, escaping host binds, anonymous volumes and socket/credential mounts. |
| Runtime fields | Admit validated name/hostname runArgs only; preserve effective containerEnv, remoteEnv, overrideCommand, init and working directory. Privilege, devices, capabilities, security options, extra network and arbitrary runArgs refuse. |
| Substitutions | Admit local workspace/basename, devcontainerId, explicit localEnv entries and inspected containerEnv references. Resolve once with unset/empty/default distinguished; unknown references refuse. |
| Hooks | Admit the inventoried Anchorage hooks below in their declared array/string/object forms and official ordering; unknown dependency patterns refuse. Object initialization remains parallel. No host hook during promotion, as already decided; this profile additionally suppresses every container lifecycle hook during promote. Host initialization is limited to the closed protocol below. |
| Verification | Native declared healthcheck passing, or explicitly requested existing smoke over stdin. Smoke requires captured transitive Make/scripts/assets and expected interpreter/user. Running alone never qualifies. |

This matrix covers the actual Anchorage shape instead of inheriting the narrower
remote provider matrix. Read-only observation on 2026-10-24 found Dockerfile build,
vscode, seven parallel initialize entries, sibling BundleDock script execution,
home/devcontainerId substitution, name/hostname runArgs, container/remote env,
setup.sh and smoke's `make devcontainer-test`. The observed containing checkout
HEAD was `28a9662f61ab6b8b2853bdc7453b78685ed63f64`; this is provenance, not proof
its working files equal that commit or a claim this entire configuration is admitted.

## Capture, fingerprint and phases

Proposed fixed limits: 100,000 entries, 2 GiB total regular bytes, 64 MiB per file,
32 populated submodules, nesting depth 8, pathname 4096 bytes, configuration 256 KiB,
smoke 256 KiB. Count directory and metadata entries; exceeding any bound refuses
68 before execution. These bounds need owner review against the intended tree;
there is no automatic fallback to a smaller capture or network fetch.

1. Resolve physical source and every submodule root; enumerate with no-follow
   directory handles. Include tracked actual working bytes, deletions, executable
   modes and nonignored untracked files. Record gitlinks and actual submodule HEADs
   separately; dirty submodules are not replaced with their pinned commit bytes.
2. Exclude `.git`, credential stores, sockets/devices/FIFOs, known private paths
   (`.aws`, `.ssh`, `.gnupg`, `.docker`, `.env` and `.env.*`) and ignored paths by
   default. A tracked private path refuses rather than silently disappearing.
   Explicit admitted ignored inputs require a reviewed exact path inventory;
   ignore status alone never classifies an executable input as safe.
3. Omit preexisting host.env/smoke.env; record omissions. Only host.env and its
   PID-suffix temporary sibling may be written into W by host initialization;
   smoke.env remains absent (this profile never invokes its orchestrator). Further
   output paths need a profile revision, not permissive ignored-tree copying.
   Tests may write only bounded candidate-home/private temporary data outside
   input regions; Python bytecode/cache writes into source are disabled explicitly.
4. Read regular files through verified handles; compare identity/size/mode before
   and after, then re-enumerate and compare source content manifests. Directory or
   symlink replacement, missing submodules or inconsistent passes refuses 68.
   Admit only relative symlinks resolving inside the admitted tree; hash link text
   and referent records. Reject links to excluded/absent paths and all hard links.
5. Generate synthetic standalone Git repositories, never copy `.git` wholesale.
   Materialize only admitted HEAD-tree files into a safe baseline tree, reconstruct
   the admitted index's staged entries, then overlay actual working bytes without
   checkout. Use the fixed safe-Git protocol below. Original commit/gitlink IDs
   remain provenance; snapshot IDs differ. Full Git-history semantics are excluded.
6. Canonical input manifest is UTF-8 JSON, sorted object keys and path records by
   unsigned UTF-8 bytes, no insignificant whitespace, LF terminator. Paths use
   slash-separated relative components; non-UTF-8 names refuse. Each record has
   exactly path, kind, mode (permission bits), size and sha256 (or link target).
   Include resolved config, submodule identities, safe Git inventory, substitutions,
   output omissions, profile version and tool identities. Normalize only assigned
   private roots to typed `$CAPTURE`, `$USERDATA` and `$DRIVER` placeholders before
   hashing resolved documents; record the exact substitution map privately. Preserve
   original physical source path/identity. Opaque operation/home IDs are allocation
   intent, not source inputs. Repeated unchanged preview/apply captures must match;
   no arbitrary path/string replacement or normalization of executable file bytes.
   Digest is
   `sha256:<64-lowercase-hex>` over these bytes, with no host secrets or file bodies
   in the public result. Private capture retains admitted executable bytes.
7. Preview and owner trust bind this manifest. Apply captures again and requires
   equality; confirmation never authorizes a different digest. Execute from the
   private capture exclusively. A sequential capture proves the selected coherent
   manifest, not a single-instant filesystem snapshot. Advisory locks do not stop
   editors; private state is not isolation against a hostile same-user process.

Keep a frozen base capture and execution mirror W. The full stage override sets
`workspaceMount=type=bind,source=W,target=CONTAINER_WORKSPACE,readonly` before up.
Official 0.89.0 awaits all host initialize branches before resource creation; they
write only declared outputs in W, while Docker mounts W read-only for the candidate.
After up, compare input regions with the base and validate outputs before invoking
any container hook or smoke. Recheck before each later phase; reject unexpected
writes (73 after executed effects). Candidate home/tmp remain writable. No stale
host.env/smoke.env is sourced. Trusted host initialization/CLI processes cooperate
with this write inventory; neither file modes nor the read-only container bind
sandbox same-UID host processes, which are outside this profile's threat boundary.

Safe-Git protocol, bottom-up per admitted repository (all subprocesses bounded):

- Parse original HEAD/index/gitlink identities without running repository config.
  A generated empty Git directory reads the resolved original object directory
  via explicit GIT_OBJECT_DIRECTORY and the original index via GIT_INDEX_FILE;
  use fixed `ls-tree -rz`, `ls-files --stage -z`, `cat-file --batch` protocols.
  No checkout/status/fetch/submodule update, hook, filter or network. Refuse source
  alternates, replace refs, unmerged/intent-to-add/sparse/split indexes, non-SHA1
  objects and missing original trees. Source attributes are captured data only.
- Use an empty environment plus recorded PATH, private HOME and TMPDIR; set
  GIT_CONFIG_NOSYSTEM=1, GIT_CONFIG_GLOBAL=/dev/null, GIT_CONFIG_COUNT=0,
  GIT_NO_REPLACE_OBJECTS=1, GIT_OPTIONAL_LOCKS=0 and GIT_TERMINAL_PROMPT=0. Fixed
  `-c core.fsmonitor=false -c core.hooksPath=/dev/null` applies to every call.
  Never inherit GIT_DIR/WORK_TREE/INDEX_FILE/OBJECT_DIRECTORY/SSH or credentials;
  only the protocol's explicitly assigned private paths/object reader are allowed.
- Create each synthetic `.git` with `git init --template= --object-format=sha1`;
  only generated core/submodule layout config is allowed. Feed raw captured blobs
  to `hash-object -w --stdin --no-filters`, build baseline trees with `mktree -z`,
  and create deterministic parentless `commit-tree` commits (fixed synthetic
  author/committer `WorkHarbor Snapshot <snapshot@workharbor.invalid>`, timestamp
  `1970-01-01T00:00:00Z`, message `snapshot <original-commit-id>`; no git identity
  config is written). Including the original ID distinguishes different commits.
  Original history/config is excluded.
- For each populated submodule map three original states separately: parent
  HEAD-tree gitlink, parent stage-0 gitlink and child checked-out HEAD. Synthesize
  each referenced committed tree bottom-up (max3 distinct snapshots per repo),
  substituting corresponding nested synthetic IDs; missing/unrepresentable states
  refuse. Parent baseline uses mapped HEAD gitlinks, parent index uses mapped
  stage-0 gitlinks, child HEAD uses mapped actual HEAD. Build each working index
  with `update-index -z --index-info`, then overlay captured working bytes/deletions/
  untracked paths without add/checkout. Preserve staged, unstaged, untracked and
  dirty-submodule categories, not original status hashes: reject a mapping that
  collapses an original difference. Record original IDs/status provenance separately.
  Submodule links resolve inside W; read-only Git metadata prevents candidate writes.
  Describe/revision/upstream/history equivalence is unsupported, never fake clean.

Anchorage dependency admission is the captured `.devcontainer` script/config/assets
inventory (initialize, host-info, setup, greeting-info, prompt TOML/logo, pins-hash,
smoke-container, test-integration/test-pins/test-setup and their fixture-invoked
up/smoke/Git helpers), root Makefile/.gitignore/.gitmodules, and the captured
BundleDock bundle-volume script plus referenced publication receipts/docs. Scripts
must match this reviewed dependency/effect inventory; dynamic source selection,
external scripts or additional executable roots refuse 68 before hooks. Inputs
may be modified working bytes when the owner trusts their digest; this is explicit
trust in the inventoried cooperative programs, not arbitrary shell-closure proof.
Container image/bundle tools and generated candidate-home startup code are trusted
external/runtime inputs, separately recorded; no host credential stores are copied.

Output validation after up, before hooks/smoke: require one new no-follow regular
host.env, owner UID, nlink1, max64KiB, seven unique keys exactly HOST_NAME/IP/OS/ARCH/
ENGINE/ENGINE_ARCH/CONTEXT in producer order. Each value is max4096 UTF-8 bytes,
no control characters; parse only POSIX single-quoted literals with the producer's
literal quote-escape concatenation, never eval/source for validation. Reject extra
shell tokens, keys, duplicate records, remaining PID temporary files or smoke.env.
Record raw output size/hash and producer/input digest. Invalid/absent output after
up is 73, and no container hook/smoke runs. Candidate home/tmp outputs are disposable
runtime data, bounded by phase deadlines, not a claimed filesystem quota/sandbox.

## Tool boundary, endpoint, environment and budgets

Use the already selected official Dev Container CLI **0.89.0**, trusted installed
Node **26.11.1** (repository pin), Git **>=2.39.5**, Docker CLI/Engine **>=27.0**
with negotiated API **>=1.46**, POSIX sh and Bash **>=3.2**. These are proposed
admission thresholds, not measured compatibility. Require local Unix transport,
inspect/mount/health fields and `run --pull never`; missing capability refuses 68.
Resolve absolute executables, record actual version/path/content digest, recheck
before launch; no npx/download/install fallback. Host utilities are the installed
uname, hostname, awk, sed, dirname, mv and platform host-info probes (scutil,
sw_vers, route/ipconfig or ip when present), inventoried by path/digest. Missing
optional probes follow captured script fallbacks; missing required tools refuse.
No new broad dependency or native isolation runner is proposed.

Pin the admitted physical Unix socket and daemon ID throughout; all Docker
processes use `--host unix://<socket>` and empty private DOCKER_CONFIG. Reinspect
daemon identity before each mutation. No context selection, credential helper,
authenticated remote provider context or ambient DOCKER_HOST is inherited.
A private docker driver used by official CLI must dispatch through this same
boundary; hooks' literal `docker` also require that driver first in fixed PATH.
This is cooperative trusted-tool routing, not a same-user security sandbox.

Construct env from empty: PATH of recorded tool directories, HOME/private user
and cache directories, TMPDIR, DOCKER_CONFIG, fixed DOCKER_HOST, LANG=C,
LC_ALL=C and TZ=UTC. Set GIT_CONFIG_NOSYSTEM=1 and GIT_CONFIG_GLOBAL=/dev/null.
Set only admitted ANCHORAGE_CONTAINER_NAME/HOSTNAME/ROLE substitutions and explicit
nonsecret ANCHORAGE_HOST_LABEL/ANCHORAGE_HOST_IP values, distinguishing unset/empty. No tokens,
SSH agent, arbitrary localEnv, host user Docker config or ambient XDG state.
Container env is separate and comes from the inspected image plus admitted config;
resolve remoteEnv after image inspection, never by importing host PATH.

Proposed argv vectors below are specifications for fake/native conformance, not
commands run here. `D` is absolute private Docker driver, `W` execution mirror,
`C` captured config, `O` full private override, `U` private user-data directory.
For promote, W is the original source directory, C remains receipt-captured and O
is the image-only config with explicit original workspace mount; no live config
is reevaluated or hook executed. All private argv paths are materialized from the
receipt's normalized map and checked before launch.
`L` is an explicit ordered list of exact `--id-label key=value` pairs binding
original source identity, operation and stage role; do not use mirror paths as
original identity. Configurations set userEnvProbe=none explicitly. Stage
containerEnv/remoteEnv also set PYTHONDONTWRITEBYTECODE=1 and GIT_OPTIONAL_LOCKS=0;
record these visible additions in effective configuration and trust inputs.

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

Smoke stdin is captured smoke-container.sh bytes, not a shell-expanded pathname.
Run onCreate/updateContent/postCreate/postStart through the official hook phase;
postAttach is suppressed because there is no attach, recorded in preview/receipt.
These container hook commands apply to stage only. Promote invokes only the
image-only `up` vector; it never invokes run-user-commands or smoke.
`up --skip-post-create` still executes initializeCommand. No flag is claimed to
suppress that host hook: promotion's full replacement config must omit it.

Bounds: metadata call 5 s, capture/preflight 120 s, total host initialization 600 s,
build/resource phase 1800 s, container hooks 600 s, health wait 120 s, smoke 600 s,
stop 30 s, each remove 30 s; whole stage 3600 s, promote 1800 s. A phase consumes
the remaining total deadline, never resets it. Output per child 1 MiB combined,
JSON metadata 4 MiB, no raw child output persisted. Signal process group TERM,
allow 2 s, then KILL and inspect. A killed client does not prove Docker/build
cancellation; uncertain daemon effects require 73 and no automatic retry.

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
verifying → staged_verified or staged_unverified → promoting → primary_resources →
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

## Promotion image and executable-byte obligations

Promotion override is image-only with `image=sha256:<verified candidate image ID>`;
no build, Features, additional Features, initializeCommand or UID extension. The
private driver denies build/buildx/pull/tag during promote and inserts the fixed
`--pull never` option in validated native run argv (reject a conflicting option). A missing local image refuses before primary stop. Inspect resulting
`.Image` independently and require exact equality; a CLI success message is not proof.

Keep ordered image metadata and source contribution separate in the receipt.
Official metadata merging can append baked hooks to source hooks. Stage retains
source contribution once and image contribution once, official origin/order and
object parallelism; do not flatten an effective hook list back into source config.
Preserve effective env precedence and mount-target resolution, distinct
containerUser/remoteUser and overrideCommand. Promotion's full image-only config
omits source lifecycle hooks and initializeCommand; skip-post-create and
skip-post-attach must suppress baked metadata lifecycle hooks as well. Refuse
unbound/conflicting metadata before effects. Always create a fresh primary ID;
never reuse the candidate or let workspace labels attach to the wrong container.

**Proposed hook-free promotion choice:** no run-user-commands, dotfiles, smoke or
other exec phase. Verify final primary running, exact image, complete expected
mount/user/env configuration and passing healthcheck when declared. Without a
healthcheck, require the prior receipt-bound successful candidate smoke plus
fresh source/input/image/config rechecks. No implicit workspace smoke runs at
promotion. These checks establish image/configuration replacement, not application
health on the preserved home. Existing primary-home/workspace bytes remain live
data; candidate setup is not a home migration, and previously configured startup
files may remain incompatible with a changed image. Preview this limitation.
This is a bounded profile choice for approval, not unqualified Dev Container
compatibility or a new D1/D2 question. Native conformance must prove official CLI
suppresses all metadata hooks; flag spelling alone is insufficient evidence.

Host initialization uses only the captured exact initialize.sh/BundleDock pair
and host-info.sh for the observed Anchorage shape. Each declared bundle supplies
an owner-trusted digest reference R, exact volume V and bundle name B. The private
PATH driver admits these literal hook calls, in order per parallel bundle branch:

```text
docker pull R
docker image inspect --format {{.Id}} -- R
docker volume create V
docker run --rm --network none --mount type=volume,src=V,dst=/bundle,volume-nocopy IMAGE_ID
```

After pulling digest reference R, inspect R and bind its returned `.Id` to the
initializer's exact IMAGE_ID; retain both R (OCI manifest/index digest provenance)
and `.Id` (Docker image configuration identity). These digests need not be equal; `V` must equal that
bundle's declared read-only destination volume. No arbitrary image, host bind,
primary home, unknown option or extra command is admitted. The driver prefixes
the fixed endpoint and uses empty config; label correlation does not manufacture
ownership of shared tool volumes. host-info additionally admits only docker info
with exact formats `{{.OperatingSystem}} {{.ServerVersion}}` and `{{.Architecture}}`,
and `docker context show`; that diagnostic
context value grants no endpoint selection. Record per-branch effects before
forwarding commands; any uncertainty is 73. Bundle pull/initializer invocations are
explicit previewed trusted effects, including shared-volume lock metadata writes; original primary home remains untouched.

Read-only source at BundleDock `6886cd12bc9a50f7709e0e52cf9b6dda6cb9db95`
([initializer](https://github.com/wstein/bundledock/blob/6886cd12bc9a50f7709e0e52cf9b6dda6cb9db95/internal/toolvolume/volume.go))
creates .initializing, compares ready-volume manifests/checksums and verifies the
tree instead of rewriting payload; a nonready nonempty volume refuses. New empty
volumes are initialized and verified. This is source evidence, not proof that a
published OCI image contains that source or that existing volumes are valid.
Policy explicitly trusts each owner-pinned OCI digest and its declared initializer
behavior as an external dependency; do not claim runtime attestation, zero writes,
hermeticity or rollback. FR-067's tool-volume removal protection remains absolute.
Retain these tool volumes on failure; never turn stage cleanup into tool deletion.
Host tools and `/etc/os-release` are also explicit runtime dependencies.

Promotion publishes no generated output: it neither copies mirror host.env or
smoke.env back nor executes a host/container hook that sources them. Existing
original workspace outputs remain unchanged, without a freshness/application
health claim. Newly staged generated outputs prove only candidate verification.
If a future profile needs output migration or container hooks on the preserved
home, require separate path/output/trusted-byte semantics before implementing it.

## Closed result, error and recovery proposal

All proposed objects below are closed: exactly listed required keys; explicitly
nullable fields admit null values, not omitted keys. Unknown fields refuse. IDs/digests use the formats
above; host paths max4096 bytes, strings max1024 unless specified. Public text is
escaped; no secret, raw env, script body, child transcript or remote operation_id.
Keep existing schema_version=1 envelope and error {code,message,remediation?}.
Future source PR adds schemas/command table together, not in this docs PR.

| Object | Exact proposed fields / types |
|---|---|
| stageResult/promoteResult | status enum(preview,staged_verified,staged_unverified,promoted,refused,partial); phase enum(journal states plus preflight); recovery_id nullable string; fingerprint nullable digest; primary nullable resource; candidate nullable resource; home nullable resource; verification nullable verification; effects array(effect), max256; recovery array(step), max32 |
| resource | kind enum(container,volume,image); name nullable string; id nullable string (container full64hex, image sha256, volume name); daemon_id string |
| verification | policy enum(health,smoke,health_and_smoke); status enum(not_requested,missing,passed,failed,stale); image_id digest; input_digest digest; observed_at UTC timestamp; output_digest nullable digest |
| effect | action enum(create_volume,host_initialize,build,create_container,container_hooks,smoke,stop_primary,remove_primary,create_primary,remove_candidate,remove_home); target resource; state enum(planned,attempted,observed,uncertain,remaining) |
| step | action enum(inspect_retained,restage_after_cleanup,run_stage_with_smoke,remove_candidate_with_guarded_rm,owner_reconcile); target nullable resource; instruction string |

Private receipt adds exactly version=1, operation_id, sequence, previous_hash,
state, endpoint_identity, original_source, input_manifest_digest, tool_manifest,
config_digest, image_metadata_digest, effective_config_digest, output_inventory,
primary/candidate/home resource identities, trust_digest, consent flags,
verification and action inventory. Tool manifest entries are path/version/digest;
output entries path/producer_phase/size/digest. Detailed byte manifests remain
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
Selection/usage 64, absent labelled primary65 and internal74 retain ADR0013 mapping.
Timeout diagnostics identify phase/local target, never invent remote operations.
No receipt is evidence of success; no receipt is also not evidence of no effects.

## Evidence and remaining gate

Pinned primary source: [CLI options](https://github.com/devcontainers/cli/blob/v0.89.0/src/spec-node/devContainersSpecCLI.ts),
[host initialization](https://github.com/devcontainers/cli/blob/v0.89.0/src/spec-node/configContainer.ts),
[metadata merging](https://github.com/devcontainers/cli/blob/v0.89.0/src/spec-node/imageMetadata.ts),
[unchanged-image and UID paths](https://github.com/devcontainers/cli/blob/v0.89.0/src/spec-node/containerFeatures.ts).
Read-only Anchorage references: [config](https://github.com/wstein/anchorage-workspace/blob/28a9662f61ab6b8b2853bdc7453b78685ed63f64/.devcontainer/devcontainer.json),
[initialize](https://github.com/wstein/anchorage-workspace/blob/28a9662f61ab6b8b2853bdc7453b78685ed63f64/.devcontainer/initialize.sh),
[host-info](https://github.com/wstein/anchorage-workspace/blob/28a9662f61ab6b8b2853bdc7453b78685ed63f64/.devcontainer/host-info.sh),
[setup](https://github.com/wstein/anchorage-workspace/blob/28a9662f61ab6b8b2853bdc7453b78685ed63f64/.devcontainer/setup.sh),
[smoke](https://github.com/wstein/anchorage-workspace/blob/28a9662f61ab6b8b2853bdc7453b78685ed63f64/.devcontainer/smoke-container.sh).
Source inspection establishes feasibility constraints only; no runtime passed.

Remaining owner decisions: approve this exact profile's read-only mirror,
synthetic Git semantics and hook-free promotion limits. Naming/deletion scope
follows Package B direction; no old proposal merge is substituted for profile
approval. Engineering evidence still required: independent contract review, safe
Git/status fixtures, output parser and official CLI fake conformance; native
Linux/macOS runs later establish compatibility, never this source-only proposal.
Red-first fake tools must cover source swaps/dirty submodules/ignored secrets,
output freshness, exact argv/env, hook origins/order, no promotion host hook,
UID/build/pull/tag refusal, image equality, allocation races, crash at every intent,
unverified outcomes, all deletion consent combinations and every unchanged FR-067
regression. Owner-authorized Linux/macOS runs must exercise the actual admitted
Anchorage shape before compatibility claims. Full T038/T039 dependencies remain
as written; no task graph is silently amended to schedule around this gate.
