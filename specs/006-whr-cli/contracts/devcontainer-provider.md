# T009 trusted configuration and Docker provider — consolidated V3.3

Status: source consolidation of the independently Sol DESIGN-CLEAR V3.3, including
immutable initial-seed binding, before failure-first implementation. Author owns
T009 only; retained independent reviewer must confirm faithful consolidation before
behavior. T008 final accepted prerequisite is
`7b041e4c28023b11aa9c8497ef27d219857efcf9`; its exercised runtime source is
`64541c1fa8b1f0d9f353272a0ea8ee6888ea5f5c`. No provider, Docker, browser, capacity
or crash acceptance is claimed by this document. T009 remains unchecked.

## Scope and accepted upstream interfaces

Retain Feature001 T005/T016/T006 acceptance and the existing Feature006 ordering.
This work supplies native trust/config/provider/storage primitives, not lifecycle
transport, dispatcher, current-runtime promotion, retries or reconciliation (T010).
Original Feature001 T016's browser trust evidence remains pilot T012. T013 terminal
artifact delivery is separately deferred; no terminal component is installed here.

Accepted T007 draft v1 already carries `approval_ref` and the exact normalized
request. Create/rebuild use request config with wire `execution_config=null`;
start carries its frozen target config. No envelope field or wire version changes.
Go `protocol.Context{Peer,Snapshot}` / Elixir HostProtocol.Context remain trusted
caller-created expectations, not decoded authentication. `Validated.Disposition`
`new_eligible` alone never grants effects. Preserve pending/completed lookup and
reconciliation dispositions without hooks, provider mutations or new audit.

Accepted T008 authenticates owner/host assignment through identity.State and
Hosts.Peer/current_peer?. It exchanges hello/heartbeat only and currently advertises
empty capabilities. T009 neither invents a lifecycle channel nor advertises Docker
from a configuration flag. T010 must construct operation snapshots/local durable
claims and authenticated delivery; never manufacture them from incoming body IDs.
Native T009 admission compares the already validated command, scoped operation,
reservation, exact revision approval and capability plan before any effects.
The config package stays independent of protocol (protocol already imports config).

## Exact revisions and owner approval

Foundation's unchanged descriptor is literal `repository_identity`, full lowercase
40/64-hex config `commit`, and the complete unique scalar-sorted submodule
`[{path,commit}]` set. No branch/alias/URL-only trust; no silent omissions. Requested
config is immutable operation identity. Runtime effective_config is populated only
on its candidate and equals the operation snapshot; queueing a new revision never
changes an older runtime. Trust is separate context excluded from request identity.

ConfigTrust.approve(authenticated Accounts.Principal, descriptor) stores immutable
approval ID, owner, literal repository, exact config/submodule descriptors,
canonical bytes/hash and approved_at. Native schema/FKs enforce ownership and
uniqueness; exact repeat returns existing approval without duplicate mutation.
Narrow Accounts actions config_approve (owner-write) and config_read (read) are
registered explicitly, never aliased to status. Revoked/expired/wrong-owner and
dashboard mutation refuse. New commits require new explicit approval; unattended
approval is not added. Native host lookup rechecks current Hosts.Peer assignment,
then exact approval ID/owner/descriptor. No unauthenticated approval-reference API.
Agent approval expectation arrives separately through the trusted native context;
its ID/owner/descriptor must match T007 command and immutable operation input.
T010 owns its authenticated delivery, not a new grant/signature scheme here.

Git resolution uses a fresh private stage, exact argv, isolated configuration and
no ambient global/system hooks, filters, aliases or URL rewrites. Resolve exact
commit objects; read manifest/.gitmodules/tree gitlinks before explicit submodule
initialization. No recursive clone or submodule --remote. HTTPS/SSH/scp literal
origins only initially; nested submodules and relative submodule origins explicitly
unsupported. Credentials use separately authorized helpers, never URLs/logs/copied
Git config/workspace environment. Reject traversal, symlink/Git-entry/containment
and overlapping path errors in addition to the accepted lexical path predicate.
Requested and resolved complete path/commit sets must match. Retained project trees
are never reset, cleaned or fetched over by configuration resolution.

## Closed source and effective schemas

Manifest is strict duplicate-free JSON with exactly schema_version=1,
devcontainer_path:string, projects_path:string and bundles:[]; unsupported fields,
nulls/types and nonempty bundles refuse. Validate actual Git entries/containment.
Devcontainer-relative parent references may resolve only inside approved config
staging; lexical validation alone cannot establish containment.

Devcontainer JSONC uses explicitly pinned official jsonc-parser. Reject syntax and
duplicate decoded keys at every depth before semantic conversion. Render validated
AST number nodes from their original source slices, without JavaScript numeric
conversion; native UseNumber retains exact lexemes and integer gates reject
fractional/exponent/overflow spellings rather than coercing them. This also applies
to strict raw registry JSON before descriptor size/schema checks. The accepted
integer-only wirejson profile is not a Devcontainer parser. Initial source values
containing Devcontainer `${...}` substitutions are unsupported.

| Raw source field | Admitted type/value |
| --- | --- |
| name | string |
| image OR build | exactly one; image digest-qualified string; build closed object below |
| build | dockerfile:string; optional context:string, args:string map, target:string |
| containerUser, remoteUser | required explicit canonical numeric UID:GID (semantic gate below) |
| containerEnv, remoteEnv | string map |
| overrideCommand | boolean |
| userEnvProbe | none |
| updateRemoteUserUID | false |
| waitFor | postStartCommand |
| shutdownAction | none |
| onCreateCommand, updateContentCommand, postCreateCommand, postStartCommand | string, string argv array, or object of string/string-array commands |

The independently reviewed ownership clarification admits both source users only
as explicit canonical decimal UID:GID pairs (no leading zeros except zero, EACH
ID is in 0..4294967294; sentinel 4294967295 and larger values refuse). containerUser must equal remoteUser or be exactly 0:0. Source-last
metadata precedence and exact run/exec users must be independently verified, making
image Config.User, metadata defaults and Dockerfile USER irrelevant to workspace
ownership. No inferred passwd entry, named root, named vscode or bare numeric UID;
absent/named/ambiguous identities refuse before seed/provider effects. Preserve the
historical workspace example; add separately identified admitted fixtures.

Read-only daemon information must establish rootful Docker without userns-remap;
unknown/rootless/remapped operation refuses before host/container UID equivalence
is used. Exact runtime inspection and real user-namespace evidence corroborate this
at acceptance. Host initial-chown capability must be established before seeding
(root with CAP_CHOWN on Linux when needed, or exact own UID/GID). Initial data files,
directories and symlinks receive remoteUser's UID/GID once; ordinary modes/executable
bits remain, with setid/sticky bits stripped. Host-private parent/marker remain
host-owned0700/0600. The immutable initial marker includes the initial data UID/GID.
A changed remoteUser UID/GID refuses before effects; retained data is never
recursively chmodded/chowned/reset and no wildcard Git safe.directory is added.
Real numeric nonroot Git, chmod and byte-retention evidence is mandatory.

All other source fields refuse: initializeCommand (host execution),
postAttachCommand, Features including nested/local/transitive inputs, Compose,
customizations/extensions/dotfiles, runArgs, workspaceMount/workspaceFolder,
extra mounts, privilege/capability/security/device options, hostRequirements and
port publication/forwarding. Never silently approximate or claim full compatibility.

A pinned maintained Moby/BuildKit Dockerfile AST parser gates capabilities only;
the official CLI retains provision semantics. Static digest-qualified external FROM
and prior-stage aliases are supported; dynamic FROM/unsupported platform selection,
custom syntax frontends, VOLUME, ONBUILD and malformed/ambiguous source refuse before
build. Reject source build.args and ARG declarations in BUILDKIT_ and
_DEV_CONTAINERS_ namespaces. Scrub inherited frontend/Buildx/Docker configuration;
use fixed provider endpoint/tool paths. Independently validate every external base's
raw metadata, with Config.Volumes and Config.OnBuild absent/empty. No extra or
anonymous-volume support. Raw Docker Config keys are closed to User, Env,
Entrypoint, Cmd, Volumes, OnBuild, Labels, WorkingDir, ExposedPorts, StopSignal,
ArgsEscaped, Healthcheck, Shell and StopTimeout, with maintained Docker types,
exact case and strict duplicate checks. Any other Config key diagnoses/refuses
before effects; apply the same gate to local post-pull/build image Config.
Ancillary root image-document fields are preserved in the raw Document and do not
extend Config/metadata capabilities. No bespoke shell/Dockerfile interpreter.

Raw image devcontainer.metadata is independently strict-decoded before CLI merge;
malformed/duplicate/unknown content never becomes an accepted CLI empty fallback.
Admit only the above supported singular container hooks/users/env/override/wait/
probe/UID/shutdown fields with their types and policies. Reject id/Features,
entrypoint/init/privileged/capAdd/securityOpt/mounts/customizations/ports and unknowns.

The generated override is the FULL validated source object, not a delta:
--override-config replaces the document. Preserve admitted hooks/env/build fields;
--config remains the immutable original path for relative build anchors. Add only
provider-owned workspaceFolder=/workspace, exact recorded bind workspaceMount,
userEnvProbe=none, updateRemoteUserUID=false, waitFor=postStartCommand,
shutdownAction=none and runArgs=[--name,workharbor-runtime-<reserved-runtime-UUID>].
Raw source runArgs is always forbidden. Conflicting policy inputs refuse.

Validate effective output separately: exact configFilePath provenance and fixed
mount/name/policy; supported plural onCreateCommands/updateContentCommands/
postCreateCommands/postStartCommands arrays; postAttachCommands absent/empty.
Generated unsupported fields may only be absent/empty/false (init, privileged,
capAdd, securityOpt, entrypoints, mounts, customizations, ports/forwardPorts).
Compare command/env merges and preserved source to independently validated inputs;
CLI output is a candidate, never authority. Nonempty fixed metadata ensures the
CLI final stage overwrites authored metadata labels; hostile-label/reopen evidence
is mandatory rather than an assumed label guarantee.

## Tools and pre-effect validation

Exact candidate pins and integrity records are in
[tool-pins.json](../../../apps/host_agent/internal/providers/docker/tool-pins.json).
The host companion package lock pins CLI0.89.0 and jsonc-parser3.3.1. The CLI npm
artifact is bundled with no runtime npm dependencies. Dockerfile parser is
Moby/BuildKit v0.34.0 (Apache-2.0), selected maintained release with Go1.26.8 minimum,
compatible with pinned Go1.27.2; its dependency graph is locked in go.mod/go.sum.
The reviewed direct low-level dependency golang.org/x/sys v0.49.0 matches that
BuildKit requirement and supplies maintained Linux Renameat2/RENAME_NOREPLACE and
Darwin RenameatxNp/RENAME_EXCL wrappers. No overwrite or emulated fallback; other
platforms refuse. Extracted official companion runtime file SHA256 values are
embedded from internal/devcontainer/tool-files.json after integrity-locked,
script-disabled npm installation; the original JSONC wrapper is embedded product
code, so a repository cannot select/replace its parser program. Shared native
application gates install this same lock with npm ci --ignore-scripts first.

Buildx v0.38.0 is pinned for raw registry manifests. Its formatted .Image output
normalizes JSON and drops Docker Config.OnBuild, so it never authorizes raw config
capabilities. The independently reviewed amendment pins maintained crane v0.22.1
(Apache-2.0), including official Linux arm64/amd64 archive and extracted binary
SHA256 values, for exactly `crane config <selected-leaf@sha256>`; no bespoke registry
client, layers, save or pull. Verify raw root/selected-leaf manifest SHA256, then
raw config SHA256 AND its declared positive byte size before strict duplicate/type/
metadata parsing. Each raw manifest/config has an explicit 1MiB host input limit;
reject oversized advertised config size before spawning crane, and enforce streamed
stdout size/deadline with owned-child kill/reap and discarded bounded stderr.

Registry tools receive a fresh host-owned0700 private HOME/config/XDG staging area,
both HOME/.docker/config.json and DOCKER_CONFIG/config.json containing only `{}`
with0600 permissions, and empty private XDG_CONFIG_HOME/XDG_RUNTIME_DIR. Environment
is restricted to tool PATH, LANG and those private paths; ambient registry auth,
cloud/proxy credentials and credential-helper configuration never pass through.
No insecure endpoint/source flags or tags/indexes substitute for the selected leaf.
Unavailable/ambiguous/over-budget raw input refuses before provider effects.
Node26.11.1 is the repository runtime pin. These selections are not measured
provider compatibility; exact installed version and real Docker evidence must
pass before acceptance. Owner decision H50:b: the host agent does not hash the Node
or Docker CLI binaries. Each is verified by exact installed version plus guest
package verification (package signature/checksum at guest install, recorded in the
runtime preparation evidence). The procedure and evidence fields are in
[guest package verification](../../../docs/development/guest-package-verification.md);
it is a required runtime prerequisite, written but not yet exercised, not an
established guarantee. The Node
`linux_*_sha256` values in tool-pins.json are release-archive digests, not binary
hashes. Docker Engine29.8.2 is the initial intended provider pin;
its verified guest package/install remains a separate runtime prerequisite.
No latest/npx download fallback. Original code retains EUPL-1.2 SPDX; use packages
independently and preserve dependency license/provenance.

Preflight order: resolve/validate immutable Git and gitlinks; raw schemas and
capabilities; exact owner approval/operation match; read-only local OR registry raw
image/base config + digest/platform; metadata/volume/ONBUILD checks; official
read-configuration merged candidate under typed mutation-refusing Docker adapter;
effective/preservation/provenance checks; final exact input/approval recheck.
ONLY THEN seed storage, pull exact digests, build/create/start. Fresh-cache registry
metadata is allowed; unavailable/ambiguous metadata refuses without pull. The
CLI's registry-inspect failure can fall back to docker pull: the adapter MUST
block it in preflight. No cache-only requirement or separate image trust grant.

The reviewed native run identity amendment uses a fresh exclusively reserved
host-private operation directory and generated CIDfile path, left absent for Docker
itself. Raw source CIDfile/runArgs remain forbidden. Docker CLI's CIDfile creation
is not an exclusive/fsync guarantee; only the live same fenced allocation request,
fresh current authorization, full64hex CID, and independent unique full name/all
labels/image/sole mount inspection can bind the first exact target. Never replace
a different target or adopt from names/events/labels after ambiguous loss. CIDfile
is allocation identity only, never completion or a durable receipt.

The native typed run uses the independently verified FINAL local config ImageID
and fixed provider `--pull=never`; missing images/layers fail unavailable without
implicit pull or tag fallback. Explicit admitted digest pulls occur only after all
approval/capability gates, followed by fresh raw/local metadata+ID checks. Official
up retains workflow authority; no native create/start workflow substitute is added.
Typed run/events/exact-ID exec streams stay within the phase's owned host-client
lifetime; disconnect cancels/reaps that client without inferring daemon quiescence.

## Runtime identity and closed provider driver

Opaque provider handles stay in adapter scope. Runtime name derives only from
accepted reserved UUID. Complete labels bind provider namespace, owner, host,
workspace, runtime ID/generation and allocation operation; current start/stop
operation has a separate fence. Independent discovery validates unique complete
ID/name/labels/image/project mount/storage; zero=absent, duplicates or any mismatch
=conflict/unknown, no first-match adoption/deletion. Name collision is inspected
and refused; never overwrite or delete foreign resources.

Because CLI up selects the first label match, the closed typed Docker executable
adapter validates internal discovery filters/results and every start/exec/delete/run
target against context before forwarding. run includes exact fixed name/labels/mount.
Unknown/global argv and arbitrary executors refuse. Per-phase allowlists: preflight
reads only; resource phase scoped provision; hook phase exact existing-runtime exec,
no allocation/start/build. No Docker socket/browser/control-plane exposure.

## Official resource/hook phases and T010 boundary

Fixed resource command is official up with retained workspace, immutable --config,
full --override-config, repeated exact --id-label, --skip-post-create,
--skip-post-attach, --default-user-env-probe none,
--update-remote-user-uid-default never, --mount-workspace-git-root false,
--mount-git-worktree-common-dir false, --omit-syntax-directive true and typed
--docker-path. The omit flag enforces the reviewed bundled-frontend policy on the
official generated build, with independent generated Dockerfile/build-argv checks;
raw source directives still refuse. It is not passed to run-user-commands. No source flags or
remove-existing-container. skip-post-create suppresses all container lifecycle hooks
and dotfiles, NOT host initializeCommand, which the raw matrix rejects.

After positively complete inspected resource binding, T010-fenced official
run-user-commands uses exact verified --container-id plus matching labels/config/
full override/workspace, skip-post-attach/env-probe none and the same fixed driver.
Supported hooks may read/modify retained mutable data; approved command/config/build
inputs remain immutable. No claim to immutable content for scripts reached in data.

T010V1.6 owns durable phase intent/fence before execution and receipt before ack.
Resource receipt requires FULL normal official up completion, closed final success
and exit0 after finishBackgroundTasks/dispose AND exact unique final inspect.
Printed JSON, attached Docker child exit, cancellation, markers/events/names/absence
are insufficient. Hook receipt records actual normal full exact-ID command exit;
CLI timestamp markers are written BEFORE hooks and prove no successful completion.
No overlapping takeover while fenced runner owns work. Missing receipt after all
workers are lost remains bounded unknown; never blind whole-up rerun or inferred
daemon quiescence. Positive hook-unknown recovery requires resources-complete,
hook CLI group reap, acknowledged exact candidate stop + stopped inspect, then
explicitly authorized candidate removal. Trusted new-key/new-generation rebuild may
rerun; no automatic resend. No generic retry engine/native run replacement/service.
T009 primitives alone do not establish T010 orchestration/recovery acceptance.

## Durable project storage and atomic initial seed

Storage outlives runtimes and has mandatory owner/host/workspace/storage identity.
Native first provider_handle observation is fenced against exact reservation,
operation/current intent/scope; once set it is immutable. No data deletion here.
The original create operation supplies immutable INITIAL seed descriptor. Never
compare seed marker to a later rebuild execution descriptor or desired projection.
New approved config changes staging/candidate only, preserving existing user data.
Native Foundation.storage_binding(owner,host,operation,intent,storage) returns the
scoped immutable original create descriptor/reservation and existing provider handle.
Foundation.observe_storage additionally requires executing/current intent, stores
only a first handle, and repeats it without replacing it or duplicating audit. These
are authenticated-caller primitives, not new transport/dispatch capabilities.

Store.Seed receives this authoritative binding, ExistingHandle outside the immutable
marker, initial UID/GID, a trusted immutable ORIGINAL resolver source and a native
current-operation/approval Check. Check runs before filesystem effects, inside the
exact-storage lock, before publication and before returning a completion handle.
IDs alone grant no effect. A known handle with absent final is storage_unavailable;
never recopy or repair missing retained data. An unexpected handle/path refuses.
Only never-observed storage may copy an absent final, from an exact ORIGINAL
source descriptor; valid existing final reuse does not depend on a later rebuild
source descriptor. T010 must supply actual authenticated delivery/durable fencing.


A host-private final storage PARENT contains data/ (only mounted subtree) plus
private sibling seed manifest. Under exact-storage serialization build a same-FS
uniquely owned temp parent; complete initial copy and exact scope/storage/original
seed manifest; fsync data/files/directories/manifest; atomically publish WHOLE parent
NO-REPLACE; fsync destination parent before completed receipt/handle observation.
No separately published data subtree, overwrite fallback or unowned adoption.

Existing final with valid complete exact owned original binding: reuse WITHOUT
recopy/reset/clean, allowing user edits. Absent final: recover exact owned temp or
discard/reseed that temp only. Invalid/partial/mismatched/unowned final: preserve and
refuse, never delete/repair/recopy. Rename collision validates final under same fence.
After-rename/before-receipt crash reuses exact complete final after parent fsync,
never seeds twice. Temp cleanup only exact owned staged candidates. Source filesystem
APIs/filenames are implementation detail; crash tests do not claim power-loss proof.
Initial copied symlinks must be relative and their complete existing target chains
must resolve inside canonical ORIGINAL ProjectsPath. Absolute, escaping, cyclic,
dangling or otherwise indeterminate initial links refuse invalid_seed before
publication. Standard filesystem resolution checks actual chains, not only lexical
Clean. Preserve original bytes for legitimate ../binary and contained chains.
This explicit initial-input subset never traverses or rewrites valid retained data.

Runtime stop/start/remove never delete storage. T010 owns lifecycle/data-delete and
multi-runtime cleanup authorization; no broad label-based removal. Host systemd
stays on Linux host; no AI orchestration/native tool/bootstrap enters images.

## Failure-first acceptance and runtime gate

Write meaningful negative tests before behavior: approval scope/revisions, JSONC
syntax/types/duplicates, paths/gitlinks/symlinks, raw/inherited/effective unsupported
inputs, frontend/volumes/ONBUILD, pin/tool refusal, source preservation, typed argv,
internal wrong target/duplicate/name collisions, seed races and retention.
Both native suites and explicit manifest IDs must retain all inherited acceptance.

Real Docker fresh-cache IMAGE AND Dockerfile success; refusal with zero hooks and
zero provider mutation; raw/merged hostile metadata, nonempty final metadata and
hostile authored label/reopen; exact binary/untracked/edited hashes across primitive
stop/start/replacement/failed build/changed-approved-config; seed crashes before/
during copy, before manifest, before rename and after rename/before receipt;
competing publisher/no-replace, valid edited final and invalid/unowned final.
Phase/receipt loss tests use actual normal exits/inspect and truthful unknown.
No mocks, skipped tests or absent tools satisfy real acceptance.

Fixture P3 is independently PREPARATION-DESIGN-CLEAR ONLY. Sole helper owns guest
mutation. Create isolated/network-isolated native-arm64 Ubuntu24.04 at2CPU/2GiB/
16GiB; fresh whole-guest CPU/memory/disk enforcement and dockerd/BuildKit/Node/Go
ancestry must be proven before workload, otherwise defer+clean. Two fresh guests
sequentially; bounded evidence then unconditional exact owned-ID deletion on every
outcome unless explicit user retention instruction. Preserve outer Engine/BuildKit/
context. Actual runtime GO requires exact candidate/archive/test manifest and helper
guards; source GO does NOT grant guest activation. Final five-workspace/eight-
attachment capacity remains T024. Full canonical make check is reserved combined
head; run appropriate native/pristine app/source/manifest gates and retain raw logs.

## Official references and unresolved implementation evidence

- [CLI0.89.0 release](https://github.com/devcontainers/cli/releases/tag/v0.89.0).
- Pinned CLI sources: devContainersSpecCLI.ts, configContainer.ts,
  injectHeadless.ts, singleContainer.ts, imageMetadata.ts, containerFeatures.ts,
  utils.ts in the official release; inspected semantics, never copied implementation.
- [Buildx registry inspect](https://docs.docker.com/reference/cli/docker/buildx/imagetools/inspect/).
- [Docker reserved frontend arguments](https://docs.docker.com/reference/dockerfile/#buildkit-built-in-build-args).
- [OrbStack isolation](https://docs.orbstack.dev/machines/isolated) and
  [caps](https://docs.orbstack.dev/machines/#resource-limits).

Exact pins/install/build, native behavior and actual guest proof remain unverified
until recorded. No wire/lifecycle/browser/terminal completion is implied by native
trust/provider primitives or by this consolidation review.

## Native implementation alignment and reviewed transport refinements

Provider.Prepare accepts only native Admission produced from authenticated T007
context, exact approval and authoritative ORIGINAL storage binding plus current
fence checker. It resolves/seals source, admits all raw registry inputs, checks
rootful unmapped daemon/candidate union and explicit users, validates full official
read-configuration, then Inspect ALWAYS Seed (including existing-final parent
fsync/current Check) before returning a handle. A newly absent final requires the
resolved descriptor equal ORIGINAL; known handle plus absent final refuses.

Prepared.Resources and Hooks are one-attempt native primitives, not a dispatcher.
Explicit typed digest pulls happen only after every preparation gate, then official
up performs resource semantics. Hooks require positively completed resources and
exact-ID run-user-commands. Full normal Node exit, private adapter/client reap,
closed result, independent effective-input comparison and fresh exact running
runtime inspection precede PhaseEvidence. T010 must durably record that evidence
before acknowledgment; it owns all interrupted provisioning/lifecycle recovery.
Close cancels and reaps owned official CLI/client groups before private staging
cleanup; cancellation never proves daemon completion or ends container hooks.

The generated build uses one private absent provider-only IID file, added only to
reconstructed native buildx argv. Full normal build exit plus exact owned regular
IID bytes, inspection of THAT image ID, nonempty expected final metadata and tag
same-ID agreement precede immutable proof registration. Missing, stale, partial,
foreign, mismatched, failed or canceled evidence never registers/adopts/reuses.
The final built image supplies command expectations when overrideCommand is false;
never substitute the original base image's command. Official startup script bytes
are measured against pinned CLI0.89.0 and retained only as SHA256 expectation.

The private Unix streaming adapter registers a random 32-byte/64-hex token before
one-use invoke. Registration alone starts no work. Duplicate/canceled/unregistered
invokes refuse; cancellation closes only that exact private request connection and
owned input, cancels its native client context and waits boundedly for completion
or reports unknown. A positive completion acknowledgment requires the exact forward
handler/client to finish, never daemon/resource quiescence. Closing registration,
aborting owned input/output and reaping handlers is part of phase cleanup. No
browser/CP wire capability, broad socket executor or source-selected endpoint exists.

Pinned crane uses plaintext automatically for loopback/.localhost/RFC1918 literal
registry authorities even without an insecure flag. These and link-local/unspecified
literal authorities refuse before metadata subprocesses/provider effects. Named
registry authorities use the pinned tools' HTTPS entrypoint; do not claim a stronger
transport policy than measured pinned-tool behavior. Every raw manifest/config
still requires the exact approved digest/size chain and strict admission. Optional
CAFile/CASHA256 is trusted native host setup only: exact physical privately owned
regular bounded PEM CA bytes/hash/validity verified before each subprocess; only
SSL_CERT_FILE is appended to the existing private registry-read environment. No
source/wire/env passthrough, credential helper, TLS private key, insecure flag or OS
trust mutation. A changed/missing/invalid bundle refuses. Actual pinned-tool TLS
compatibility is a required real test, not established by native fixtures.

The maintained pure Dockerfile grammar imports remain under config capability
validation. The architecture checker permits only exact whole-line unaliased
parser import in dockerfile_plan.go and parser/instructions/linter in trusted.go.
All Docker clients/sockets, other imports/files and control-plane provider access
remain rejected, with preserved inherited and new independent negative harness IDs.
This checker alignment requires full canonical verification on the exact pristine
candidate; earlier docs-only canonical evidence cannot cover it.

Pinned CLI resource stdout displays canonical numeric UID0 as `root` and other
UIDs as their decimal string. Validate exactly that derived display value; it
provides no named-user resolution or ownership authority. Source/effective/run/exec
users and immutable initial ownership remain the exact admitted numeric UID:GID.
Normal resource/hook stdout uses the same strict companion/UseNumber path as
preflight, so exact configFilePath.$mid=1 provenance retains its numeric type and
noncanonical fractions/exponents never become accepted integers.
