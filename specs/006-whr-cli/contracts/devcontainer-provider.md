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
duplicate decoded keys at every depth before semantic conversion. The accepted
integer-only wirejson profile is not a Devcontainer parser. Initial source values
containing Devcontainer `${...}` substitutions are unsupported.

| Raw source field | Admitted type/value |
| --- | --- |
| name | string |
| image OR build | exactly one; image digest-qualified string; build closed object below |
| build | dockerfile:string; optional context:string, args:string map, target:string |
| containerUser, remoteUser | string |
| containerEnv, remoteEnv | string map |
| overrideCommand | boolean |
| userEnvProbe | none |
| updateRemoteUserUID | false |
| waitFor | postStartCommand |
| shutdownAction | none |
| onCreateCommand, updateContentCommand, postCreateCommand, postStartCommand | string, string argv array, or object of string/string-array commands |

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
anonymous-volume support. No bespoke shell/Dockerfile instruction interpreter.

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
Buildx v0.38.0 is pinned for raw registry metadata without a bespoke registry client.
Node26.11.1 is the repository runtime pin. These selections are not measured
provider compatibility; exact installed version/hash and real Docker evidence must
pass before acceptance. Docker Engine29.8.2 is the initial intended provider pin;
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
--mount-git-worktree-common-dir false and typed --docker-path. No source flags or
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
