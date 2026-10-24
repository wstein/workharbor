# Local bundle reference drift: bounded #143 delivery

This read-only Package D slice implements the FR-069 report for the Anchorage
configuration format below. It does not complete T039 or its full-T038 prerequisite.
The existing task descriptions, checkboxes and dependency graph remain unchanged.
Other pin formats need their own reviewed protocol and tests before support.

## Invocation and observations

`whr workspace drift --local --source <directory> <exact-name|full-64-hex-id>`
(`ws` remains the workspace group alias) requires all three inputs. There is no
bare `drift` or `status` alias. Missing inputs, extra arguments or unsupported
flags return 64 before I/O. Without `--local`, the command refuses 64.

Use the existing FR-060 local Unix Docker endpoint selection and one shared
five-second deadline for endpoint discovery, fixed projected container list and
inspect calls, and local pin reads. Never inspect container environments, run
hooks, exec a version command, inspect/pull images, recreate containers or mutate
resources. Preserve the existing minimal Docker subprocess environment and empty
Docker configuration; never consult credential helpers or remote contexts.

The selector must identify exactly one running recognized **primary** container.
A missing, ambiguous, foreign, staging, temporary or stopped target is
`cannot_determine`. The explicit source directory must resolve to the same physical
path as that primary's `devcontainer.local_folder` label. A missing label, unreadable
path or different directory is `cannot_determine`; do not infer source from cwd or
read a Docker-provided path as the chosen source. The source root and its config
files use `workspacecfg`'s existing bounded no-follow filesystem reader.

## Initially supported pin protocol

Read only `.devcontainer/devcontainer.json` (at most 64 KiB), with the existing
strict UTF-8 JSONC scanner: comments and trailing commas accepted, duplicate keys,
invalid encoding, BOM/NUL, excessive depth/nodes and unsafe files rejected.
The presence of `.devcontainer/bundle-pins.json`, including an unreadable or unsafe
entry, refuses determination rather than silently choosing between pin sources.
No bundle-pins or arbitrary lockfile parser is defined by this slice.

`initializeCommand` must be an object whose bundle members have this exact array
shape (the string hook path is data and is never executed):

```json
["sh", "${localWorkspaceFolder}/.devcontainer/initialize.sh",
 "quay.io/bundledock/toolbox:VERSION@sha256:FULL_LOWERCASE_DIGEST",
 "bundledock-toolbox-FULL_LOWERCASE_DIGEST", "toolbox"]
```

The member key and final argument are the same bundle name, matching
`[a-z][a-z0-9-]{0,31}`. References have the fixed `quay.io/bundledock/<bundle>`
repository prefix, optional OCI tag (`[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}`) and mandatory
lowercase 64-hex SHA256 digest. URLs, userinfo, queries, substitutions and opaque
references are unsupported and never echoed. The expected volume is exactly
`bundledock-<bundle>-<digest>` and its target is `/opt/bdock/<bundle>`.
The actual Anchorage non-bundle member
`"host": ["sh", "${localWorkspaceFolder}/.devcontainer/host-info.sh"]` is ignored
inertly. Other command shapes or inconsistent bundle declarations make the pin
source unsupported. At least one bundle is required.

Each pin must have exactly one matching string in `mounts`, with `type=volume`,
matching `source` and `target`, and bare `readonly`; bare `volume-nocopy` is optional.
No duplicate or additional options are supported on those bundle mounts. Every
mount under `/opt/bdock` or with a `bundledock-` source must correspond to a pin;
conflicting, missing or opaque declarations are undetermined. Non-bundle mounts
remain inert. Mount objects are not part of this initial protocol.

## Comparison, output and exits

For each declared bundle, exactly one observed mount at its expected target must
be a volume, explicitly read-only in Docker metadata, and named
`bundledock-<same-bundle>-<64-lowercase-hex-digest>`. Equal expected and observed
names give `in_sync`; unequal known digests give `drift`. Missing, duplicate,
writable, bind, wrong-bundle, incomplete or opaque mounts give
`cannot_determine` and a null mounted identity. Additional undeclared bundle
mounts add `cannot_determine` rows; unsafe bundle names become the fixed name
`unknown`. Missing or unsupported pins make the aggregate undetermined without
inventing per-bundle pins.

**These are reference-alignment facts only.** A digest in a volume name does not
attest the bytes of that volume, image provenance, installed tool versions or
whether any initialization command actually ran. An equal name cannot prove
payload integrity. The owner decides whether and when to recreate.

The report fields are frozen by `bundleDriftResult` in
[the CLI schema](../../../contracts/cli/output.schema.json). Container ID and
per-bundle identities are nullable when unknown; no raw hooks, environment,
configuration, paths or unvalidated identities are output. Rows sort by bundle
name, then reason; mount order does not affect JSON. Human output escapes
untrusted text per FR-022 and states the payload-attestation limitation.

Aggregate precedence is `cannot_determine` over `drift` over `in_sync`. Exit 0
means `in_sync`; exit 68 represents either drift or undetermined evidence, with
the distinct `status` field identifying which. This chooses the already permitted
68 alternative of FR-069/SC-025 without changing ADR 0013's exit table.
`--json` writes one normal envelope, retaining the report in `error.Result` on
68; human reports go to stdout and the standard diagnostic goes to stderr.
Cancellation remains 72 and deadline expiration 71. Usage errors and interrupted
reads can omit a report; every drift-command 0/68 envelope requires one.

## Verification boundary

Offline tests use a captured Anchorage configuration, pure comparisons, fake
Docker CLI records and a fixture Unix listener. They prove fixed read vectors,
minimal environment, report/schema behavior and zero mutation calls. They are
not real-host Docker, payload integrity, installation or browser evidence. The
owner's separate real-host/format/build acceptance gates remain open. Full T038
and task/feature completion remain independent of this bounded delivery.
