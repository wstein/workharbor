# Feature 008 data model

Status: proposed; implements the entities of [spec.md](spec.md).

| Entity | Fields and invariant |
| --- | --- |
| Workspace | id, repository path, `workharbor.json` reference, devcontainer reference; never modified by sandbox operations (FR-001). |
| ExecutionInstance | `sandbox_id` (`sbx-` + 16 base32), optional name, workspace id, profile, provider id, state, retention, base commit, `shared_git_metadata`, git profile version and hash, created time, labels, handle (provider-native id). Immutable: id, workspace, profile, provider, base commit. |
| TrustProfile | `developer`, `cooperative_job`, `untrusted_job`; table below. |
| ProviderCapabilities | per profile: the eleven fields of [runtime-provider.md](contracts/runtime-provider.md), each `status`, `value`, `scope`. |
| SandboxRequest | workspace, profile, requirements (`field=value`), copy policy, acknowledgement flags, identity variable names (values are held by the caller, never logged), name. |
| AdmissionDecision | `allowed` with `Plan`, or `refused` with an ordered list of refusal codes; no side effect. |
| Plan | ordered mounts (source kind, target, mode), environment (names and sources, no secret values logged), labels, user, home kind. |
| OwnershipLabels | `io.workharbor.sandbox=1`, `.id`, `.workspace`, `.profile`, `.provider`, `.retention`, `.created` (RFC 3339 UTC), `.schema=1`, `.gitprofile` (version and hash), `.shared_git_metadata` (`writable` or `none`). Every object of an instance carries them. |
| CopyPolicy | base commit, include list (dirty tracked, untracked, ignored paths), secret-pattern overrides with confirmation, submodule choice; the resolved file list with hashes is part of the manifest. |
| ResultBundle | patch file (binary-safe, relative to base commit), manifest (partial flag, instance id, profile, base commit, base snapshot hash, result tree hash, export time, exit status, hashes, copy policy, shared-metadata flag), export destination. Never applied automatically. |
| GitProfileReport | location finding, host-only findings, preflight result (`read_failed`, `write_failed`, `rename_busy`, `origin_outside_mount`, ok), profile version. No values. |
| HostOnlyFinding | key, origin file (redacted home), remedy code, severity `report`. |
| CleanupReport | removed objects (ids), leftovers (ids and labels), results preserved, deadline outcome. |

Backend neutrality (FR-034): no entity carries a provider-kind enum; `provider` is an opaque id and everything that differs
is a capability value. A microVM provider is one more `ProviderCapabilities` source (`kernel_isolation=microvm`).

## Trust profile table

| | developer | cooperative_job | untrusted_job |
| --- | --- | --- | --- |
| Source | workspace bind mount (rw) | operator-supplied linked worktree plus common git dir at host absolute paths, acknowledged | private copy |
| Home | workspace home volume | scratch | scratch |
| Git global config | host directory rw | agent profile ro | agent profile ro |
| Results | in the repository | patch and manifest | patch and manifest |
| Host global config mount | yes (FR-019) | never | never |
| Intended use | humans; not for agents (FR-024) | trusted cooperative jobs | untrusted jobs |

## State transitions

| From | To | Trigger |
| --- | --- | --- |
| (none) | `preparing` | create admitted |
| `preparing` | `ready` | plan applied and preflight passed |
| `preparing` | `failed` | create or preflight failure (partial objects reported) |
| `ready` | `running` | exec started |
| `running` | `finished` | process ended (status recorded; not acceptance) |
| `running` | `failed` | cancellation, crash or deadline |
| `finished`, `failed` | (export) | export allowed, `partial=true` after failure (FR-007) |
| `ready`, `finished`, `failed` | `discarding` | discard accepted (FR-008) |
| `discarding` | `discarded` | all labelled objects removed |
| `discarding` | `cleanup_incomplete` | removal failure (leftovers listed; retry allowed) |
| `cleanup_incomplete` | `discarding` | retry |

Retention (`ephemeral`, `kept`) is independent of state; `kept` blocks `clean`, not `discard`. Any other transition is
`refused.invalid_state`.
