---
title: Canonical native role bindings
description: "Versioned external role data, host projection and effective native evidence."
weight: 7
toc: true
---

## 5.10 Canonical native role bindings (D54)

{{< status decided >}} D54 implements D52/D53's existing declarative role
boundary for [crewbook #9](https://github.com/wstein/crewbook/issues/9) and
[workharbor #242](https://github.com/wstein/workharbor/issues/242). It selects
a data contract, not another policy authority or native configuration source.
Project-policy source approval remains {{< status open >}} in
[#283](https://github.com/wstein/workharbor/issues/283). Production integration
and target compatibility remain {{< status unverified >}}.

### Canonical source and versions

The sole canonical role source is the selected package's externally pinned
`workharbor.json` and the text resources in its exact inventory. No local
`.claude/agents`, repository settings, provider sidecar or vendor TOML is a
second role source. Native files are generated projections, not canonical data.

Contract v1 remains byte-for-byte unchanged, including its unique adapter names
and one model/effort tuple per adapter. It does not represent multiple native
roles and must not be relabelled as doing so. Retain v1 text composition and
its recorded selections; do not reinterpret old records or upgrade on resume.
Native multi-role packages use explicit contract version 2, separately reviewed
pins and their own immutable revision directory. Unknown versions refuse.

Contract v2 retains v1's `identity`, `entrypoint`,
`required_project_inputs` and `files` fields and the same inventory encoding
and limits. It replaces `adapters` with `clients` and adds `roles`.
The complete allowed shapes are:

| Object | Allowed fields and meaning |
| --- | --- |
| Manifest | `contract_version: 2`, `identity`, `entrypoint`, `required_project_inputs`, `clients`, `roles`, `files` |
| Client | `name`: exact WorkHarbor adapter name; `version`: exact native CLI version |
| Role | `name`, `description`, `instructions`: one inventoried text path, `bindings`, `tools` |
| Role binding | `client`: a declared client name; `model`: explicit requested model selection; `effort`: explicit requested reasoning effort |
| Inventory entry | `path`, `sha256`, as in v1 |

There is no `provider` field, wildcard client, version range, executable
tool declaration or arbitrary native-settings bag. For Codex the adapter name
is `codex`, not a guessed vendor/provider alias. Other clients retain their
own existing adapter identity and binding policy. A client declaration establishes
requested compatibility, not measured support.

Client names are unique, with at most 32 clients. Role names are unique, with
1 to 64 roles, and follow the existing lowercase name grammar and 64-byte bound.
Descriptions are nonempty UTF-8 with at most 4096 bytes. Instruction paths obey
the existing safe-path rules and must identify regular inventoried UTF-8 text.
Every role has 1 to 32 unique client bindings; each references a declared client.
Version/model/effort retain v1's 128/128/32-byte bounds and reject control
characters. Bindings for different roles may request different model/effort
with the same client/version. There is no inherited or fallback binding.

All arrays with identity keys are sorted lexically by that key; inventory rules
and case-collision checks remain in force. `tools` is a required, sorted,
unique array of at most 64 host capability identifiers using the same name
grammar. An empty array requests no tools. Unknown fields, duplicate JSON
keys, null required arrays, duplicates, unsafe paths and over-limit content
refuse; nothing is truncated. Retain v1 manifest/file/total size limits.

The operator selects the root role in supervisor-owned configuration. The
package cannot select the running role, worktree, project policy or child
admission. Reviewer eligibility and author/reviewer separation belong to the
selected external workflow under the project's contribution rules; WorkHarbor
treats role names as opaque metadata. The host retains generic child identity,
admission, capability ceilings, binding evidence and human artifact approval;
workflow eligibility grants no host permission. The root entrypoint supplies
package-wide workflow text; the selected role adds its inventoried instructions. Both are
pinned and bounded. Role source identity is the manifest digest, role name and
instruction digest. Its derived `role_source_sha256` is SHA-256 of these UTF-8
bytes, each with a final LF:
`workharbor-role-v2\n<manifest_sha256>\n<role_name>\n<instructions_sha256>\n`.
This binds description, bindings and tool requests through the full manifest
digest without inventing another JSON canonicalization.

### Host projection and tool bounds

A role's `tools` requests capability names; it grants none. Workharbor resolves
them against its own known executable/tool registry and the immutable operator
ceiling. Unknown or disallowed requested capabilities refuse the selected role;
do not silently drop required requests or widen the ceiling. Host enforcement
covers root, children and resume. Prompts and native settings are not substitutes
for that enforcement. Forge merge/tag/release/deploy remain forbidden.

Only role name, description, inventoried instruction text, resolved model and
explicit effort are eligible for semantic projection into native role files.
Workharbor supplies any version-specific file/path registration itself, from
the validated read-only inventory and supervisor configuration. Project policy,
worktree mapping, approved tool ceilings, sandbox/approval settings and child
admission are separate supervisor-owned records, never package settings.

No native TOML key or registration mechanism is settled by this JSON contract.
The pinned binary/schema measurement must identify the actual allowed native
keys and loading mechanism. Generate only those measured keys; do not copy
package TOML, search repository/home configuration or guess metadata keys.
An incompatible client refuses that role even if the package decoder succeeds.

Model and effort recommendations belong to the selected external skill set.
The supervisor validates explicit operator-approved client/model/effort tuples;
it never derives model grade, admission, approval authority, concurrency or
capability privileges from a workflow name. D53 records the historically
approved external development bindings, not authorization for a new invocation.
Record requested and resolved identifiers separately. Resolve exact native IDs
and supported efforts through the pinned client and authorized measurement;
tool aliases, model examples and catalog listing alone are insufficient.
No silent inheritance, account rotation, automatic upgrade or fallback.

### Effective native evidence contract

Separate three products: validated package declarations, pinned compatibility
measurement evidence, and effective settings for this actual invocation.
Matching fixture or doctor metadata never enables support.

The host-facing `NativeBindingEvidence` version 1 carries the following
required field groups. All are bounded host data; neither package text nor
agent-written files can create a trusted record.

| Field group | Stable fields |
| --- | --- |
| Ownership | `evidence_version`, `workspace_id`, `run_id`, `role`, `native_session_id`, `connection_generation`; child records additionally identify their recorded parent and native child |
| Native identity | `client`, `cli_version`, `artifact_sha256`, `schema_sha256` |
| Role/package identity | full recorded skill selection, `role_source_sha256`, inventoried instruction path and digest |
| Binding | `requested_model`, `resolved_model`, `requested_effort`, `effective_effort` |
| Host context | recorded guest worktree mapping, project policy revision/content digest and approved proof reference, configuration digest, requested tools and enforced tool ceiling |
| Instruction sources | exact effective native policy/role/skill paths and digests, plus the source-isolation measurement reference |
| Measurement | immutable committed evidence reference and digest for native loading, configuration isolation, tool enforcement and authorized model-access measurements |

The approved project-policy proof reference is a separately validated host
input. Its source/approval mechanism is pending Werner, not defined by this
role schema. A nil/unavailable proof cannot be replaced by a package digest,
working-tree HEAD, empty project text or diagnostic metadata.

`internal/agent/codex` owns production evidence assembly from the verified
tool-store identity, same-binary schema and measured native protocol/settings.
It combines those observations with service/runtime-owned persisted selection,
policy/config/worktree records and child ownership. Native observations alone
cannot assert host policy compliance. If the pinned client cannot expose or
prove a required effective field, that field stays unavailable and the mandatory
selection refuses; requested settings cannot be reported as observed settings.

The service validates exact agreement and persists the complete binding record
before work executes. Retain the original package, role, native/config/policy
proof and measurement references across restart/resume, even after defaults
change. Revalidate their immutable content and effective invocation; missing
records, drift or incompatible binaries refuse. Every child obtains its own
owned evidence record before execution under the same or narrower ceiling.

Doctor consumes a read-only projection of these host records through the existing
`LaneEvidence` seam. Its current metadata seam is diagnostic-only and is not
the production producer or a capability gate. #34/#241 own committed target
measurements, not a second effective-settings producer. The adapter/service
capability gate remains authoritative; no support claim follows from the
schema, a catalogue or an offline matching tuple.

### Bounded implementation handoff

After this specification lands, existing #283 owns the v2 decoder/store tests
under `internal/skillset/`, preserving v1 and immutable retained revisions.
Crewbook #6/#9 own a v2 manifest producer and canonical role resources after
consumer/producer coordination. Inventory/provenance/validator outputs are
shared ownership and serialize in the existing authorized crewbook worktree;
no second worktree is inferred.

#242 owns host semantic role selection/projection and truthful drift diagnostics
after the v2 consumer and policy/evidence contracts are available. #35 owns
native observation/evidence assembly; #283 owns serial service/serve
persistence, trusted policy and runtime composition. Coordinate shared types
and generated files before edits. #34/#241 remain measurement-gated; #164's
finished source pin is not a duplicate task. This design batch writes no feature
code; dispatch admits implementation after the specification lands and the
named dependencies are satisfied. A prospective slice fills no source slot.

The fresh independent review of #242's `86f6ee1`/`d0c12c6` diagnostic delivery
continues separately. Full native #242/#283 and crewbook #9 criteria remain open.
A trusted policy reader cannot start until Werner's authority-source answer is
recorded and its specification lands. Measurement and actual loading, not extra
offline diagnostic work, gate native production support.
