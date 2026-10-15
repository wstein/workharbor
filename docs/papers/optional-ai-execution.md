# WorkHarbor as an optional execution home

Date: 2026-10-12 | Proposal updated: 2026-10-15
Status: Concept proposal; neither an accepted feature nor implementation evidence.

## Motivation and boundary

A future WorkHarbor owner could follow a development task from its approved plan
to its running workspace, intervene when execution blocks, and inspect the exact
result presented for review. An internal Phoenix LiveView board could make those
connections visible beside the resources the owner already operates.

This is a later optional extension. The [product baseline](../product.md) and
[constitution](../../.specify/memory/constitution.md) currently exclude AI product
features. [Proposed ADR 0006](../adr/0006-optional-ai-execution.md) owns the proposed
boundaries and approval gates. This paper explains the experience and questions
to investigate if the owner separately authorizes that direction. It does not
amend the constitution, supersede ADR 0005 or approve product implementation.

## One owner experience, distinct authorities

The owner would select a repository and feature, inspect its versioned spec, plan
and tasks, and see which task has the required scope approval and completed
prerequisites. The board could show pending, eligible, executing, blocked and
review-requested items, always distinguishing a requested transition from a
confirmed observation. These are illustrative view states, not a new dispatch
protocol or accepted state machine.

An eligible item could link to an attempt from one selected external orchestrator
(Symphony or Gas Town) and its WorkHarbor workspace,
with resource usage, connection health, intervention requests and redacted evidence.
When disconnected, it should show stale or unknown observations with timestamps.
An owner could request a pause or cancellation and see acknowledgement rather than
an immediate success assumption. Retry and handoff would preserve prior attempts,
commits and evidence; a finished task would not silently reopen because a process
restarted. WorkHarbor would own workspace access and resource operations through
its existing authorization boundaries. The selected orchestrator would remain the
sole scheduling/retry authority for that workstream/attempt. Native agent launch,
authentication and provider credentials stay with its external runner; human merge
and release policy remains unchanged.

Public GitHub Issues would carry bounded work requests and collaboration. Optional
GitHub Projects views could present roadmap status; neither would replace the
internal execution board. GitHub documents Projects as configurable views over
issues and pull requests, including updates that affect the underlying items.
Any future synchronization therefore needs an explicit field owner and conflict
rule. A card movement cannot authorize execution. See
[GitHub's Projects documentation](https://docs.github.com/en/issues/planning-and-tracking-with-projects/learning-about-projects/about-projects).

## First increment: revision-bound Spec Kit explorer

Begin with a read-only feature list and detail view: select a repository and Git
revision, follow the feature's spec/plan/tasks links, inspect a task's prerequisites
and blockers, then follow its attempts, workspaces and acceptance/review evidence.
Display the current and reviewed artifact revisions and stale/disconnected
observations explicitly. An unavailable source or unrecognized parser format
cannot silently become an empty feature or completed task.

For example, a future view of `005-provider-aware-orchestration` could link its
approved artifacts and the separate T002/T003 attempts. As results arrive, it
would show implementation, checks, independent review, PR merge and live evidence
as separate observations. A checkbox alone cannot establish those results. This
is an illustrative future view, not evidence of a dashboard or live execution.

A task card uses `(repository_id, feature_id, task_id)` and includes the reviewed
commit, prerequisites, exact blockers, next permitted action, latest attempt and
workspace, acceptance evidence and PR. Retry must distinguish retained
handoff-only work from separately owner-authorized rework. Manual and orchestrated
attempts are equally visible; permission, verified adapter capability, profile
opt-in and task approval remain distinct decisions.

The team rated the proposed sequence for this project's current stage:

| Suggestion | Value |
| --- | ---: |
| Show spec, plan and tasks from an identified commit; Git stays authoritative | 10/10 |
| Use repository + feature + task IDs, with separate attempts and workspaces | 10/10 |
| Show approval, implementation, checks, review, merge and live evidence separately | 10/10 |
| Explain blockers and the next permitted action on each task card | 10/10 |
| Offer a concise approval packet with scope, checks and changes since approval | 9/10 |
| Add guarded command buttons after the read-only view is proven | 8/10 later |
| Let dragging a card approve or dispatch work | 2/10 |

Ratings are design judgments, not measured performance or implementation evidence.
The approval packet would bind paths, literal acceptance commands, source and
artifact revisions, prerequisite evidence and the semantic changes since prior
approval. It reduces repeated administration without hiding an authorization
change. A progress-only comparison preserves approval only when the existing
recorded guard proof permits it; the UI invents no normalization rule.

Current [Spec Kit workflow documentation](https://github.github.com/spec-kit/reference/workflows.html)
describes human checkpoints. Any later command integration must pin and verify its
actual installed workflow capabilities instead of assuming the pinned version
supports every current upstream command. The view must not execute commands
parsed from repository Markdown or issues.
[LiveView security guidance](https://phoenix-live-view.hexdocs.pm/security-model.html)
requires authorization within LiveView callbacks as well as route controls.
Owner/project checks therefore apply to reads, subscriptions and each later action;
public issue visibility cannot expose private prompts or execution logs.

No implementation follows from this paper. The read-only product increment still
needs the scope/governance and spec/plan approvals in ADR 0006. The next separately
approved design would freeze the read-model/parser contract, missing/stale-data
behavior and owner-visible acceptance cases before adding guarded controls.

## Stable identity and a frozen execution request

Suppose repository A has `001-workspace-lifecycle/T001`, while repository B has
`003-editor/T001`. They are distinct tasks. The board would key each by stable
repository, feature and task identities, retaining path/rename history where
needed. Human labels such as `T001`, issue numbers and branch names are display
references. A task can link several attempts and an explicitly scoped tracker
reference without becoming a different task. Attempt IDs identify individual
runs and their outcomes; they do not identify approved scope.

Before dispatch, a future adapter would resolve an immutable execution contract:
approved artifact commits and digests, task identity, dependency evidence,
allowlisted changes, acceptance argv, execution mode, workflow/runtime pins and
external owner-approval references. The contract revision would travel with the
attempt, workspace and resulting evidence. Repository artifacts describe the
work; a durable external owner record establishes approval of the named revision.
An agent cannot approve its own request by editing repository or board fields.

A semantic artifact or contract change would stop eligibility until renewed
approval. A retry would reuse the same contract unless the owner approves a
replacement; completed implementation would resume only missing handoff steps
when matching evidence exists. The current
[operator rules](../development/symphony.md#retry-and-rework-states) already explain
that distinction for development tooling. The board should expose it clearly,
without changing the current workflow or live task approvals.

## Reuse with a narrow integration seam

The pinned [Symphony specification](https://github.com/openai/symphony/blob/be10a1b79df723d6d7612b5651c8522704dafb2e/SPEC.md)
assigns dispatch, retries and reconciliation to its orchestrator, with per-issue
workspaces and an optional status surface. WorkHarbor's proposed role is to connect
those observations to authorized resources, owner interventions and retained
evidence. It should not implement a competing queue. A resource denial or lost
host would become an explicit scheduler input through a future adapter; delivery,
idempotency and acknowledgement semantics still need design and failure testing.

The pinned [Spec Kit README](https://github.com/github/spec-kit/blob/0443760542e7e5269724744a2ac55c4bd386d095/README.md)
describes structured processes, templates and versioned outcomes spanning
specification, planning, tasks, implementation and convergence. Those artifacts
would remain repository authority. The board would link them and surface approval
status, rather than create a second editable specification or treat a generated
task list as owner consent.

The [proposed adapter design](../design/orchestrator-adapters.md) extends this seam
to a choice of Symphony or Gas Town, with explicit task/attempt/workspace/path
ownership, retained-data rules and failure questions. Gas Town's role/mailbox/Beads
and Git-worktree model needs a different mapping from Symphony's issue/workflow
model; neither is a verified WorkHarbor plug-in. Selecting an orchestrator cannot
authorize dual dispatch or its automated merge behavior.

Initially, separate processes would permit independent updates and restarts of
WorkHarbor and the selected orchestrator. A narrow authenticated, versioned adapter
is the proposed seam; no stable upstream product-integration API is assumed.
The pinned [Elixir README](https://github.com/openai/symphony/blob/be10a1b79df723d6d7612b5651c8522704dafb2e/elixir/README.md)
labels the reference implementation a prototype and documents GitHub Issues and
Linear adapters plus a LiveView/JSON observability service. Its existence does
not establish authorization, durable intervention handling or integration readiness
for WorkHarbor.

Running WorkHarbor and Symphony in one BEAM could be investigated later, but
needs proof that all process registrations, supervisors, configuration and secret references are
instanceable and that dependencies and restart behavior coexist safely. For
example, the pinned [orchestrator source](https://github.com/openai/symphony/blob/be10a1b79df723d6d7612b5651c8522704dafb2e/elixir/lib/symphony_elixir/orchestrator.ex)
accepts a process-name option while retaining a default named task supervisor and
calling shared configuration. That is partial evidence, not proof of an embeddable
plugin. A fork or vendored copy of either upstream core is not the proposed path.

GitHub Issues is the first tracker option. Linear would remain an optional external
SaaS service for owners who already choose it. Its
[official API documentation](https://linear.app/developers/graphql) describes an
authenticated hosted GraphQL endpoint. The proposal assumes neither a self-hosted
Linear server nor a supported Elixir SDK; client selection and credential scope
would need verification during later design.

## Questions and evidence before investment

The owner must first approve scope and the required constitutional change or
exception, then a feature spec and plan. Baseline work keeps its current priority
and scope; this paper adds no task to features 001–006 or the live execution queue.

The first feasibility work should establish the adapter contract, retained evidence
model and failure recovery across independently restarted services. Measurements
should cover idle and active resource cost on the target host; this paper claims
no performance advantage from either topology. An experiment should show that
duplicate events cannot duplicate execution, stale approvals block dispatch,
lost scheduler state cannot erase evidence, and intervention races have bounded,
visible outcomes. A disposable tracker/workspace walkthrough should demonstrate
normal handoff and interrupted handoff before any readiness claim.

Every server-side read, subscription and command needs owner/project authorization.
Filtered views provide convenience, not ACLs. Per-user/project credentials,
least-privilege provider tools, workspace isolation and redaction need separate
design and acceptance gates before multiple users or projects share execution
infrastructure. Current single-owner scope and container-isolation limits remain
binding. Approval verification must survive forged issue text and agent-authored
board state. Until these questions have evidence, this remains a documented option.
