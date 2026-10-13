# 0006-optional-ai-execution: Optional execution board after the baseline

Date: 2026-10-12 | Proposal updated: 2026-10-13
Status: Proposed; no product implementation authorization
Amended by: [ADR 0009](0009-in-container-ai-agents.md) (in-container agents, wave one)

## Context

The [constitution](../../.specify/memory/constitution.md) excludes AI orchestration
from the product. [ADR 0005](0005-development.md) keeps Symphony separate as
development tooling. Both remain authoritative. This proposal records a possible
later extension, with rationale in the [concept paper](../papers/optional-ai-execution.md).
It creates no accepted feature, implementation task or change to current approvals.

## Proposed direction

After the workspace baseline is demonstrated, consider an optional internal
Phoenix LiveView execution board connecting repository-owned Spec Kit artifacts,
an independently operated Symphony scheduler and WorkHarbor-managed resources.
Ordinary workspace lifecycle and terminal access must remain usable without this
extension, an AI subscription or a tracker account.

The board would expose execution progress, workspace/resource links, blocked
interventions and review evidence. Public GitHub Issues and optional GitHub Projects
would remain separate collaboration surfaces. GitHub Issues is the first proposed
tracker; Linear would be an optional external SaaS integration, never a required
self-hosted component. No Linear server or Elixir SDK capability is assumed.

| Responsibility | Proposed owner |
| --- | --- |
| Requirements, plan and task definitions | Versioned repository artifacts, maintained through Spec Kit |
| Scope and spec/plan approval | Human owner, in durable external review evidence |
| Dispatch eligibility, concurrency, attempts and retry scheduling | Symphony alone |
| Workspace/provider lifecycle, resources and access authorization | WorkHarbor |
| Internal execution view, intervention requests and durable evidence links | WorkHarbor |
| Public issue/PR discussion and optional roadmap views | GitHub, with explicit mappings |

An execution item would use a stable `(repository_id, feature_id, task_id)` key.
`T001` is local to a feature, never a global key. Tracker identity, workspace
identity, attempt identity and immutable execution-contract revision would be
separate references. A retry must retain earlier evidence and must not create new
scope authority.

The immutable execution contract would bind approved repository/artifact revisions,
one task, prerequisites, allowed changes, acceptance commands, runtime/workflow
pins and approval references. Approval must be verified against an external owner
record and those exact inputs. Board movement, a label or an agent-authored field
cannot grant approval. Semantic contract changes require renewed owner approval;
the existing [bounded execution rules](../development/symphony.md#spec-kit-and-bounded-implementation-issues)
continue to govern current development.

## Read-only Spec Kit view first

The first proposed increment is an authenticated, owner/project-scoped read-only
view of `spec.md`, `plan.md` and `tasks.md` at an identified Git commit. Git stays
authoritative; the dashboard stores a derived view and evidence references, never
a second editable specification. Pin the parser/schema capability and retain the
artifact paths, commit and content identities. Unknown layouts or unavailable
objects produce an explicit unavailable/stale view, not inferred task state.

Use the repository + feature + task key above, with separate attempts and
workspaces. Show artifact approval, implementation progress, acceptance checks,
independent review, PR/merge and live-verification evidence separately. A checked
task is a reported progress marker; it establishes neither merge nor live success.
Every observed result identifies its source revision, evidence and observation time.

Each task card explains unmet prerequisites, blockers and the next permitted
action. A later approval packet would show exact scope, allowed paths, commands,
reviewed revision and changes since the previous approval. Semantic changes need
renewed approval; progress-only changes retain it only when the existing recorded
comparison proves that preservation. Missing evidence stays unknown.

Prove the read-only view before proposing guarded command buttons. Such controls
would use canonical trusted-host operations and freshly revalidate authorization,
source, scope and recovery at invocation. Markdown, issue text and arbitrary
workflow strings are never executable authority. Dragging a card cannot approve,
dispatch or mutate scope. Manual and orchestrated attempts share the same task
view while retaining distinct route and provider-compatibility observations.

This sequence adds no approved product scope, live dispatch or Spec Kit command
execution. Product reads/subscriptions themselves require the governance approval
below; "read-only first" does not bypass that gate.

## Integration boundary

The initial proposed topology uses two separate OS processes/BEAM instances for
WorkHarbor and Symphony, with independently pinned updates and restarts. An
authenticated, versioned adapter would exchange requests and observations; its
protocol and failure behavior remain design work. WorkHarbor would not duplicate
Symphony's queue or retry engine. Resource admission can reject or suspend a
request without becoming a second scheduler.

Sharing one BEAM is a later option only after proving instanceable process names,
configuration/secret isolation, supervision and dependency compatibility. The
pinned upstream is not assumed to be a ready plugin. Prefer a narrow adapter and
upstream contributions; this proposal recommends neither a fork nor vendoring
Symphony or Spec Kit core.

## Approval and evidence gates

Before any product implementation, the owner must explicitly approve a product
scope change and constitution amendment or scoped exception under its governance,
then review a superseding decision, feature spec and plan. Merging this proposed
ADR would only preserve the proposal. No approval defaults from board state.

That later design must prove authorization for every board action, server-side
owner/project access, isolated per-user/project credentials, secret redaction and
execution isolation. A filtered board view is not an ACL. Multiple users remain
outside the current baseline. Acceptance must cover approval forgery, changed
contracts, duplicate dispatch, restart/retry recovery, disconnected schedulers,
intervention races and cross-project access before claiming integration readiness.

## Consequences

The baseline and features 001–004 retain their existing scope and approval state;
current issue execution is unaffected. The proposal adds documentation only.
Adapter feasibility, resource costs and same-BEAM operation are unmeasured. Reject
or revise this direction if its operational or security costs outweigh the value
of connecting execution evidence to workspaces.
