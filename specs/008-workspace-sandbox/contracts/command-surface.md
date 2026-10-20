# Command surface (PROPOSED syntax only)

Status: option A approved by the owner 2026-10-20 (OD-4); nothing is registered or implemented (registration is task T016, after the ADR 0014 amendment). `whr workspace` remains the anchor. Registering a
new group needs an amendment of ADR 0014 (its group list is frozen), a `contracts/cli/commands.json` change with freeze
tests and an owner-reviewed spec amendment. ADR 0014 lists the groups in its decision section 1; alias rule 5 governs short forms only, so a
new group is an amendment of that ADR (OD-4, approved in principle). FR-028.

## Alternatives considered (research input: IDEAS_NOTES section 5 of the Anchorage branch `docs/ideas-codex-delivery-plan`)

| Option | Shape | Assessment |
| --- | --- | --- |
| A | New group `whr sandbox create\|ls\|show\|exec\|keep\|discard\|clean\|export` | Recommended |
| B | `whr sandbox primary\|up\|ls\|rm` | The primary environment is the workspace, not an instance; `primary` invites operations on it, which FR-001 forbids |
| C | Flags on `whr workspace up` (`--runtime=sbx`, `--ephemeral`, `--microvm`, `--runtime=microvm`) | `--microvm` and `--runtime` read as preferences and invite silent fallback (FR-014); overloads workspace verbs whose rm semantics protect volumes (FR-067) |
| D | Nested `whr workspace sandbox ...` | Longer, no clearer ownership split; no short forms possible anyway |

**Recommendation: A.** Reasons: (1) an instance has its own identity, labels, retention and result lifecycle, distinct from
the durable workspace; (2) workspace commands and their frozen contracts stay unchanged; (3) the request is expressed as
`--profile developer|cooperative|untrusted` plus `--require <field>=<value>` (for example `--require kernel_isolation=microvm`),
which maps one to one to `Admit` and cannot be read as a preference; (4) no short forms are needed (alias rule 3). Replacing
workspace commands is deferred.

Backend neutrality (FR-034): there is no microVM command family or `--microvm` flag. The backend is selected by `--provider
<id>` (default: the configured provider) and constrained by `--require`; a different backend is a different provider id with
the same verbs.

## Proposed verbs

| Verb | Purpose | Notes |
| --- | --- | --- |
| `create` | Admit, prepare, preflight; prints id; for `cooperative_job` the operator-supplied worktree path | `--workspace`, `--profile`, `--require`, `--name`, `--base <rev>`, `--include <path>` (copy policy), `--acknowledge-shared-git` (cooperative), `--git-author-*`/`--git-committer-*` (identity only) |
| `ls`, `show` | Read-only listing and detail | `--json`; `show` exposes the provider handle so an external runner can attach with its own tools |
| `exec <id> -- <argv...>` | Run an opaque argv in the instance | passes the argv as data; exit-status passthrough needs a separate ADR 0013 amendment (OD-1); not registered in this increment |
| `keep <id>` | Set retention `kept` | does not merge or publish |
| `export <id> [--to <dir>]` | Write patch and manifest | no remote, no apply |
| `discard <id>` | Remove an instance | refuses unexported results (FR-008) |
| `clean` | Remove finished or failed ephemeral instances with no unexported results | never `kept` |

## Exit codes (ADR 0013, no new codes)

0 success; 64 bad flags, missing `--profile`, malformed `--require`; 65 unknown instance or workspace; 68 any
`refused.*` (including foreign object and wrong state); 70 unattended confirmation missing; 71 deadline; 72 interrupt or
cancellation; 73 incomplete cleanup (leftovers listed); 74 internal. Library mapping: Feature 008 T004.

## Feature 006 FR-030 boundary

`whr` creates and describes instances, sets the identity variables (identity is not a credential), runs an operator-given argv
and exports results. An external runner or the operator installs, authenticates and launches an agent, supplies any provider
credential by its own means and decides acceptance. `whr` has no `run --agent`, no agent-specific flag, no agent detection and
no credential relay; credential mediation is a separate specification. A credential an external runner places inside an
untrusted instance is exposed to that instance (residual risk). Whether generic `exec` is outside Feature 006 FR-030 is recorded as an
owner decision OD-1, 2026-10-20: outside Feature 006 FR-030 ([plan.md](../plan.md#owner-decisions)). Which profile an agent uses is the operator's choice: `whr`
does not detect agents and enforces only profile-based capabilities (FR-024); `developer` is for humans.
