---
title: Skill-set consumer contract
description: "Stage 1 external text package encoding and integration interfaces."
weight: 15
---

The [D52 contract](../design/skill-sets.md) is implemented in stages under
[#283](https://github.com/wstein/workharbor/issues/283). This page describes the
platform consumer interface. Crewbook #6 agrees the encoding; independent
implementation review is still required. Runtime mounting and live client
loading remain {{< status unverified >}}. The raw published crewbook import is
not a compatible default; configuring a default waits for its reviewed package.

## Manifest and external lock

The package contains `workharbor.json`, separately from any package-specific
layout metadata such as `crewbook.json`. All objects reject unknown and duplicate
JSON keys. Text must be UTF-8. The manifest has these required fields:

| Field | JSON type | Meaning |
| --- | --- | --- |
| `contract_version` | integer | Exactly `1` |
| `identity` | string | Lowercase identifier, up to 64 characters, starting with a letter; subsequent characters may include digits, `_` and `-` |
| `entrypoint` | string | One inventoried public text file; this stage explicitly loads its text |
| `required_project_inputs` | array of strings | Unique identifiers with the same syntax as identity, at most 32; each must be confirmed by the trusted project-input provider |
| `adapters` | array of objects | Between 1 and 32 explicit client bindings |
| `files` | array of objects | Complete sorted inventory, excluding this manifest |

Each adapter object has four required nonempty strings: `name`, `version`,
`model`, `effort`. Names follow the identity syntax and are unique. Version and
model are at most 128 bytes; effort is at most 32 bytes. Version matching is an
exact string comparison, without ranges or wildcards. The client binding
provider must validate its supported version, model and effort and return an
exact declared tuple. There is no inherited model or effort. An absent provider
refuses a package; manifest data does not establish client support.

For the existing production adapter `name` is `claude-code` (its `Name()` value).
`version` denotes the exact installed native CLI version, not the adapter
protocol's integer `agent.ContractVersion`. The present Claude configuration has
an optional explicit model but no effort binding or version validator. There is
therefore no approved production tuple in stage 1; synthetic test bindings are
not production compatibility claims. Stage 2 must validate and apply the tuple
without changing the existing instruction/permission suppression controls.

Each file object contains string `path` and string `sha256`. Paths are relative
POSIX paths, at most 240 bytes, containing only ASCII letters, digits, `.`, `_`,
`-` and `/`. Absolute paths, backslashes, empty/`.`/`..` components, repeated
separators, trailing dots and `.git` components are forbidden. ASCII-only paths
avoid Unicode normalisation aliases; case-colliding files or directories are
refused. Paths sort in ascending byte order. Digests are 64 lowercase hex digits.

The inventory includes every distributed file and dot-directory resource except
`workharbor.json`. Its digest is SHA-256 of these UTF-8 lines, in path order:

```text
<64 lowercase hex file SHA-256><two spaces><relative path><LF>
```

The last line also ends in LF. Hash file bytes exactly, without newline or JSON
normalisation. Hash `workharbor.json` separately; this avoids a self-hash cycle.
Its own layout/resource metadata, licence and provenance files are inventoried
when distributed. An unexpected file, empty directory, executable, symbolic or
hard link, socket, device, binary content, missing resource or mismatched digest
refuses installation/loading.

The trusted **external** operator pin has fields `identity`, `source`, `commit`,
`manifest_sha256`, `inventory_sha256` (strings) and `contract_version` (integer).
Commit is exactly 40 lowercase hexadecimal characters. Source is a nonempty,
bounded descriptive repository identity, not a fetch command. Expected digests
come from the reviewed operator lock, never package self-reporting. Installation
does not run an installer or acquire upstream content.

Limits are 256 KiB for the manifest, 1,024 inventoried files, 1 MiB per resource,
16 MiB total and 16,384 filesystem entries. Validation reads bounded regular
files, checks ownership and filesystem identity, and refuses group/other write,
special permission bits and executable resource bits. Installation copies into
a temporary directory in the dedicated store, validates the copy, then renames
it to `<store>/<inventory_sha256>`. An existing revision is validated, never
overwritten. Every start/resume revalidates content. Store roots are canonical
absolute paths outside repositories, workspace roots, tools and forbidden
credential/vendor roots. Installed resources have mode `0400`.

## Supervisor selection

The supervisor JSON configuration has a `skill_set` object:

```json
{"skill_set":{"selection":"none"}}
```

`selection` is `default`, `package` or `none`. Omission means `default`, requiring
an explicit compatible `crewbook` pin in `default`; an absent default refuses
new runs rather than silently using none. `package` requires an alternative pin
in `package`. Default selection rejects an alternative package field; none also
rejects that field but may retain a configured default for future selections.
A package selection additionally requires `store`. Default status grants no
permission. There is no automatic download, branch tracking or repository-local
fallback.

Constructing the service without its `SkillSet` provider also refuses a fresh
run: omission cannot be used as an implicit no-package mode. Tests and callers
that create fresh runs explicitly select `none` or a pinned package. The composed
supervisor supplies its configuration. Existing runs retain the explicit
`legacy` marker from the additive migration; they resume without attaching any
current package. Changing them requires a new run and session.

## Service and runtime interface

`service.Config.SkillSet` supplies operator selection. `SkillForbidden` supplies
the workspace, tool and credential roots that cannot overlap the store.
`ProjectInstructions` returns reviewed text, its revision and a map confirming
declared input identifiers; it must never confirm an unreviewed repository file.
Without that provider the standing supervisor-configured agent instructions are
used, with revision `agent:<id>`; no required package input is assumed present.
Project digest changes on resume refuse launch, even if today's default changed.

`SkillBinding(context.Context, []skillset.Binding)` validates and returns the
effective client tuple. `PrepareSkills(context.Context, domain.Run, SkillMount)`
must enforce the runtime's existing mount refusal, idle/environment ownership
and rebuild locks. A `SkillMount` contains `Source` (the selected validated
directory), `Target` (`/skills/<inventory_sha256>`) and `ReadOnly` (`true`). A
missing runtime hook refuses a package before an agent launches. Stage 2 supplies
this hook and adapter support; stage 1 grants no additional runtime mount root.

The service composes a labelled platform policy section, reviewed project text,
selected external entrypoint/root and task/turn instruction into `StartSpec` on
start and resume. For crewbook its invocation context names `CREWBOOK_ROOT` as
the mounted root. This is text context, not automatic Markdown interpolation,
native command registration or an environment override. Resource and child
invocation handling remains stage 2's responsibility. Existing Claude suppression
flags, supervisor settings, approvals and credentials continue to apply.

The resolved selection, source commit, manifest/inventory digests, entrypoint,
effective binding and separate project/composed instruction digests are saved
with the starting run and audited before launching. Resume restores that pin,
not today's default, and verifies the recorded project and binding. The original
provenance survives launch failure. Missing old content must be restored at its
same digest. Uninstall and operator CLI delivery remain follow-up integration;
this stage supplies `skillset.Store.Install` and `Load` as platform interfaces.

For `none`, stage 1 records the selection and project/composed digests, with no
package tuple. Production native client version/model/effort provenance for this
path, and the reviewed repository `AGENTS.md` reader behind `ProjectInstructions`,
remain stage 2 integration work. The default standing-instruction provider does
not claim to pin repository files the existing client reads independently.
