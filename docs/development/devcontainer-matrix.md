# Dev Container provider capability matrix

Feature006 T009 remains unaccepted. This matrix describes the reviewed initial
subset and its source implementation; real Linux/Docker acceptance is still pending.
The [provider contract](../../specs/006-whr-cli/contracts/devcontainer-provider.md)
is authoritative. The official Dev Container CLI 0.89.0 supplies provisioning
semantics, with locked JSONC/parser/tool integrity recorded in the contract and pins.

| Input or behavior | Initial subset |
| --- | --- |
| Configuration | Dedicated HTTPS/explicit-SSH Git origin; exact 40/64-hex root commit and complete sorted project gitlink set; separate owner approval |
| Submodules | Explicit absolute HTTPS/SSH origins and exact approved commits; no recursive/nested or relative-origin resolution |
| Dev Container input | Strict JSONC, comments/trailing commas; fatal UTF-8/scalar checks and recursive decoded duplicate-key refusal |
| Image | Digest-qualified image; raw selected-platform registry config verified before any pull; closed raw Config keys as enumerated in the provider contract, unknown Config keys refuse (ancillary root fields preserved) |
| Dockerfile | Pinned maintained grammar; static digest external FROM or prior stage; literal named/final target; official no-Features wrapper |
| Execution users | Both explicit numeric UID:GID pairs, each ID 0..4294967294; containerUser equals remoteUser or 0:0 |
| Ownership | Rootful unmapped Linux Docker and initial chown capability; preserve ordinary source modes; immutable original data UID/GID |
| Commands | onCreate, updateContent, postCreate, postStart as string/argv/object; resource setup and trusted hook phases separated |
| Workspace data | One persistent project bind at /workspace; stop/start/replacement/failed build must retain edited binary and untracked files |
| Initial symlinks | Relative, existing complete chains contained within original projects subtree; absolute/escaping/cyclic/dangling/indeterminate links refuse |
| Environment | Supported containerEnv/remoteEnv string maps; no Dev Container `${...}` substitutions or ambient host credentials |
| Fixed policies | userEnvProbe none, updateRemoteUserUID false, waitFor postStartCommand, shutdownAction none |
| Unsupported inputs | Host initializeCommand/postAttach, Features, Compose, customizations/extensions/dotfiles, raw runArgs/mounts/ports/security/privilege/device/hostRequirements and unknown fields |
| Unsupported image/build capabilities | Raw image/base Volumes or OnBuild; custom frontends, platform/dynamic FROM, Dockerfile VOLUME/ONBUILD and any reserved frontend/provider ARG declaration |
| Runtime identity | Provider-generated deterministic name and eight complete labels plus exact container/image IDs and sole mount; foreign/partial/duplicate matches refuse |
| Completion | Normal full official CLI completion plus exact inspection; CID/IID/events/markers/client exit alone never establish completion |
| Recovery scope | Atomic ORIGINAL seed publication/reuse only; Feature006 T010 owns lifecycle dispatch/reconciliation and durable phase receipts |

The historical [workspace example](../../examples/workspace/README.md) remains a
design example with a tag/named user. The independently authored
[admitted image](../../examples/workspace/admitted-image/README.md) and
[admitted Dockerfile](../../examples/workspace/admitted-dockerfile/README.md)
examples use Docker Official buildpack-deps noble-scm at immutable index digest
4163ac2c58660c533f3cf3d178551287ddd0ac18269ef84f3a382d0698c89254.
Official metadata was captured before selection; actual raw manifests/config,
platform, tool integrity and numeric nonroot behavior must be independently checked
by the provider and real tests. No tag fallback is permitted.

Explicit real acceptance entrypoints TestDockerAcceptanceImageFreshCache and
TestDockerAcceptanceDockerfileFreshCache are implemented behind docker_integration.
They remain unrun and unaccepted; they fail on absent
tools/configuration and never skip. Image and Dockerfile runs use sequential fresh
helper-owned Linux guests; each must start without cached workspace images. Required
results include no hooks/provider mutation on refusal, raw hostile metadata, final
metadata overwrite/reopen, nonroot Git/chmod and exact retained hashes through
stop/start/replacement/failed build/approved configuration changes. Linux seed
SIGKILL/competing-publisher tests are additional evidence, not power-loss proof.
Native unit fixtures or partial slice review never substitute for these results.

The operator's 2026-10-15 limit is MAX3GB PER TEST GUEST, conservatively at most
3,000,000,000 bytes. The planned guest remains 2,147,483,648 bytes; no automatic
increase is authorized. Host-available memory guard proposals are separate from
that per-guest ceiling and from measured containment. Real proof.json/outcome.json
exports are declared by each test-owned artifact-manifest.json; no project files,
private credentials, raw hook output or ambient host configuration is exported.
