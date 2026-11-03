# RuntimeProvider contract (proposed)

Status: proposed design baseline for Feature 008 (FR-012 to FR-018); not implemented. Go package
`apps/whr/internal/runtime` (ADR 0010 layout). Stable refusal codes: [refusal-codes.json](refusal-codes.json).

## Capability report

Every provider returns one report per environment profile it supports. Each field has a value, a `scope` (what the claim
covers). Status per field: `enforced`, `not_enforced`, `unknown`; `unknown` never
satisfies a requirement (FR-015). Reports state only what the provider's tests prove (FR-018).

| Field | Values | Request that must be refused when not `enforced` | Standard container provider (Stage 2) reports |
| --- | --- | --- | --- |
| `kernel_isolation` | `shared_kernel`, `microvm` | `microvm` -> `refused.kernel_isolation_unavailable`; never a fallback | `shared_kernel` (the container shares the Docker VM kernel with all other containers of that VM, which is itself separate from the macOS Mac host kernel) |
| `filesystem_access` | `private_copy`, `bind_rw`, `bind_ro`, `linked_worktree`, `mountless` | an unsupported mode -> `refused.profile_unsupported` | `private_copy`, `bind_rw`, `bind_ro`, `linked_worktree` |
| `git_access` | `private_clone`, `linked_worktree`, `primary_rw` | untrusted with worktree or alternates -> `refused.shared_git_metadata_untrusted` | private clone, linked worktree (cooperative only), primary rw (developer only) |
| `git_config_profile` | `host_global_dir_rw` (developer), `agent_policy_ro` (jobs) | `refused.git_config_profile_unavailable`, `refused.host_git_config_for_agent`, `refused.git_env_override`, `refused.git_config_single_file_mount` | both; verified by preflight and canary tests only |
| `home_state` | `persistent_volume`, `scratch`, `none` | `refused.profile_unsupported` | `persistent_volume` (developer), `scratch` (jobs) |
| `tool_mounts` | `ro_bdock`, `none` (current identifiers; a value for provider-declared writable roots is added with the first such provider, T019) | a tool root writable outside `developer`, or a BundleDock root writable anywhere -> `refused.tool_mount_writable` | `ro_bdock` (read-only BundleDock roots); protects tool files only, not other host resources |
| `egress_enforcement` | `none`, `cooperative_proxy`, `enforced_allowlist`, `enforced_deny` | a requirement above what is `enforced` -> `refused.egress_enforcement_unavailable` | `none` (network is unrestricted and the sandbox is not offline, FR-036; proxy variables configure cooperative clients and enforce nothing) |
| `credential_mediation` | `none`, `sentinel_mediated` | `refused.credential_mediation_unavailable` | `none` |
| `host_tools` | `none`, `gateway` | `refused.host_tools_unavailable`; host Docker socket always `refused.host_socket_mount` | `none` |
| `ide_access` | `attach`, `none` | informational, never a refusal | `unknown` until verified on a host (separate evidence) |
| `cancellation_cleanup` | `graceful_then_force`, `force_only`, `none`; `leftover_report` bool | a job profile with less than `force_only` -> `refused.cleanup_capability_insufficient` | `graceful_then_force` with leftover report |

No field is a claim of containment, exfiltration prevention, performance or portability.

## Go interface (illustrative; names are not frozen until T001 is reviewed)

```go
type Provider interface {
    ID() string
    Capabilities(ctx context.Context, p TrustProfile) (Capabilities, error)
    Create(ctx context.Context, plan Plan) (Instance, error)  // only after Admit allowed
    Exec(ctx context.Context, id string, argv []string, env map[string]string) (ExecResult, error)
    Export(ctx context.Context, id string, dest string) (ResultBundle, error)
    Cancel(ctx context.Context, id string) error
    Remove(ctx context.Context, id string, o RemoveOptions) (CleanupReport, error)
    List(ctx context.Context, sel LabelSelector) ([]InstanceInfo, error)
}
func Admit(req SandboxRequest, caps Capabilities) AdmissionDecision // pure, no provider call
```

`argv` is opaque data (FR-029). `env` is filtered by profile: only the four identity variables of FR-023 are accepted for jobs and every other git-control variable is scrubbed; for `developer` a `GIT_CONFIG_*` variable is refused. `Create`, `Exec`, `Remove` and `Cancel` re-verify ownership labels before acting.

## Mount and environment plan

`Plan` is the testable artifact: ordered mounts (source kind, target, mode), environment, labels, user, home. Tests assert on
the plan before any provider acts (FR-027). Forbidden in every plan: host Docker socket, a BundleDock root (see
[Tool root providers](#tool-root-providers)) writable in any profile, any other tool root writable outside `developer` (FR-016), primary home volume in a job, primary `.git` in an untrusted job. Secret-pattern paths excluded from copies:
`.env*`, `*.pem`, `*.key`, `id_*`, `.git-credentials`, `.netrc`, `.npmrc`, `.pypirc`, `.aws/`, `.ssh/`, `.gnupg/`.

## Tool root providers

A tool root (FR-016) is a directory a tool provider supplies; requirements and tests refer to the tool root and its access
mode, and only this section names provider paths. Every provider declares per root: target path, access mode (`ro` or
`rw`) and, for `rw`, that it is writable by design. Admission refuses `rw` outside `developer`
(`refused.tool_mount_writable`).

| Provider | Roots | Access | Status |
| --- | --- | --- | --- |
| BundleDock volumes | `/opt/bdock/<bundle>` backed by `bundledock-*` volumes | always `ro`; no profile may write, remove or replace the volumes | current (Stage 2 reports `ro_bdock`) |
| `mise` data directory | a directory of installed tools, path decided by the ADR | `ro`, or `rw` in `developer` only if declared | candidate; open until spike [#319](https://github.com/wstein/workharbor/issues/319) and its ADR |

Configuration that selects tools from the checked-out repository (for example `mise.toml`) is untrusted input unless the repository is on the owner's trusted list kept outside the repository (FR-037); no provider reads, applies or trusts it on its own.

## Provider conformance suite (outline)

One table-driven suite in `apps/whr/internal/runtime/conformance/`, run against every provider (FR-018, FR-034); a new provider
adds a constructor, not a case:

1. Capability report is complete (all eleven fields, scope non-empty) and stable across calls.
2. A requirement above a declared capability is refused with the expected code, with zero side effects (provider call count).
3. `unknown` never satisfies a requirement; `microvm` is satisfied only by a provider declaring it, never by substitution.
4. `Create` applies the labels of the data model to every object; `List` returns only labelled objects; foreign objects are refused.
5. The `Plan` has no forbidden mount (Docker socket, tool root writable outside `developer`, BundleDock root writable, primary home or `.git` in a job) per profile as the threat table of the plan states (FR-027).
6. `Exec` passes `argv` as data (no shell, no interpretation); environment contains no credentials; git-control variables are scrubbed (jobs) or refused (developer).
7. `Cancel` and `Remove` are bounded, idempotent, preserve results, allow `Export` from a failed instance and report leftovers.
8. A provider that reports more than its tests prove fails the suite (declared `enforced` fields need a passing probe).

## Lifecycle of a request

`Admit` -> labels and plan -> `Create` -> git profile preflight -> `ready`. Any refusal or preflight failure happens before
`ready`; a failed create removes only what it created and reports leftovers.
