# Proposed external orchestrator adapters

Date: 2026-10-15
Status: Proposed wave-two design; outside the Feature 006 installable pilot.
No adapter, supported plug-in, protocol implementation or activation is claimed.

## Purpose and approval boundary

WorkHarbor could prepare authenticated, persistent devcontainer workspaces for
one selected external orchestrator: Gas Town or OpenAI Symphony. WorkHarbor would
own environments and resource access; the selected orchestrator would own task
execution. A narrow authenticated, versioned adapter would connect those roles.
Neither upstream would be vendored, embedded or required by the product.

[ADR 0006](../adr/0006-optional-ai-execution.md) owns this proposed direction and
its governance gates. Implementation needs a separately approved feature spec,
plan and constitutional amendment or scoped exception. This document grants no
dispatch, credential provisioning, merge, release or deployment authority and
does not amend approved execution under Features 001–005.

Feature [006](../../specs/006-whr-cli/spec.md) retains its wave-one boundary:
`whr` lists, attaches to, inspects, sends input to and stops existing in-container
agent sessions (FR-029); it does not install, configure, authenticate, launch or
wrap native agent tools, or relay their provider credentials (FR-030). Lifecycle
and access work without AI, an orchestrator or a tracker. An owner could manually
run Gas Town inside a prepared workspace as an ordinary workload, subject to
workspace trust and repository policy; that is not a certified integration or
permission for its automated merge behavior.

## Upstream models to map

Primary documentation inspected on 2026-10-15 describes Gas Town's Mayor and
Polecat roles, Beads work tracking, mailboxes, Git worktrees and Refinery merge
queue. Its runtime choices and team workflow belong to that external system.
See the [Gas Town README](https://github.com/gastownhall/gastown#readme).

Symphony's specification assigns issue polling, dispatch, retry and per-issue
workspace management to its orchestrator. Its Elixir implementation launches
Codex app-server sessions using a repository workflow. See the
[Symphony specification](https://github.com/openai/symphony/blob/main/SPEC.md) and
[Elixir README](https://github.com/openai/symphony/blob/main/elixir/README.md).
These mutable upstream links describe design inputs, not tested WorkHarbor
capabilities. The separately pinned [development workflow](../development/symphony.md)
builds WorkHarbor, remains prepared rather than activated, and is unchanged.

Both upstreams manage task identities and filesystem workspaces. An adapter must
explicitly map those concepts to WorkHarbor resources; matching names or paths
does not establish ownership, compatible lifecycle semantics or permission.

## Proposed responsibility split

| Responsibility | Authority |
| --- | --- |
| Scope, spec/plan, execution approval, merge and release decisions | Human owner under repository policy; no automatic grant |
| Task queue, eligibility, concurrency, dispatch, retries, worker launch and completion/handoff workflow | One selected external orchestrator per workstream/attempt |
| Native agent installation, configuration, authentication and provider credentials | External runner/operator, outside `whr`, control plane and host agent |
| Workspace identity, devcontainer environment, persistent storage, lifecycle, resource admission and access | WorkHarbor through its authorized operations |
| Mapping external tasks/attempts/paths to owned resources; bounded request and observation exchange | Separately reviewed adapter, with no independent scheduler |
| Approval, checks, review and merge evidence | Their authoritative records, linked without equating completion to approval |

Selecting Gas Town does not authorize its Refinery to merge WorkHarbor changes.
The external completion/merge workflow must honor the repository's independent
review and human merge/release rules. WorkHarbor resource refusal may block an
attempt; it never delegates retry or scheduling authority to a second system.
Gas Town and Symphony must not both dispatch or retry the same workstream/attempt.

## Identity, paths and retained state

The future design should bind owner and repository identity to feature and task
identity, then keep external task/tracker references, orchestrator identity,
attempt ID, WorkHarbor workspace ID, external issue-directory/Git-worktree ID,
runtime/container ID and generation, retained storage ID and intent revision
distinct. Canonical lifecycle operation/idempotency keys identify effects, not
task approval or attempt identity. Evidence should retain the approved source,
execution-contract revision and runtime/workflow pins alongside those mappings.

WorkHarbor would admit a workspace and its persistent storage. The orchestrator
would manage only explicitly assigned issue directories or Git worktrees inside
that owned workspace. The adapter design must specify the path mapping and which
party may create, resume, clean or remove each path; no host path or foreign
workspace can be inferred from issue text. WorkHarbor owns runtime removal and
confirmed storage deletion; the orchestrator may clean only its assigned temporary
issue directory or worktree under the reviewed retention policy. Closing an issue
must not delete WorkHarbor project data, retained attempts or review evidence by
default. Runtime deletion and confirmed data deletion remain separate operations.

For example, workspace W can retain storage S and worktree X while container C1
at generation 1 is replaced by C2 at generation 2. Attempt A and its evidence
remain associated with X and S; the rebuild operation K identifies that lifecycle
effect, not a new task or attempt. The future mapping must distinguish a worktree's
identity from its container-visible path and preserve the evidence/storage outside
any issue-cleanup path. This is a conceptual example, not an accepted ID format.

## Bounded capabilities and failures to design

An initial adapter proposal should enumerate only necessary capabilities: discover
compatible versions and capabilities; request admitted workspace lifecycle/access
through canonical WorkHarbor operations; report revision-bound observations and
evidence links; and request an orchestrator pause/cancel or handoff. It must not
offer arbitrary shell strings, generic native-tool launch or implicit tracker,
merge and credential authority. These are candidate capabilities, not endpoints
or a completed wire contract.

Each request would need authenticated owner/repository/workspace scope and fresh
authorization. Unsupported capabilities, version mismatch, changed approvals or
identity mismatch should refuse before side effects, with a bounded reason and
next review action. Disconnection or missing evidence yields unknown/stale state
with observation time; it cannot be treated as completion or permission to retry.

Repeated requests should reconcile the original canonical operation and result;
changed payloads under the same key should conflict. Late events from an old
attempt, runtime generation or intent revision must not overwrite current state.
Cancellation is a request until acknowledged and observed; stopping a workspace
and stopping an agent attempt are separate effects. Restart and route handoff
must reconcile retained evidence and prove the previous executor has relinquished
authority before a replacement dispatches. An uncertain transfer remains blocked,
with no second active dispatcher or automatic reopening of completed work.

## Credentials and execution transport

Workspace agents and adapters receive no host-agent credentials, Docker socket
or raw host shell. Native-tool and provider credentials remain the external
runner/operator's responsibility; an adapter cannot relay them through `whr` or
turn the host agent into a native-tool wrapper. Tracker and product credentials
also need separate scopes and redacted observations. Any future credential
brokering, TLS peer model or execution transport requires its own reviewed threat
model and acceptance evidence; the existing host protocol does not prove that
an orchestrator can securely launch tools in a managed container.

## Questions before implementation approval

- Which pinned orchestrator/runtime versions and exact capabilities can be
  verified, permitted by provider policy and explicitly opted into by the owner?
- How are owner/task/attempt/path mappings authenticated, fenced on restart and
  transferred without dual dispatch or retry authority?
- Can duplicate delivery, changed-key payloads, stale generations, lost replies,
  version mismatch, revoked approval and unavailable hosts refuse or recover
  without duplicate effects or false success?
- How will cancel-versus-completion races and interrupted handoff retain evidence,
  distinguish acknowledgement from observation and require review when unknown?
- Can a replacement container reuse retained storage and worktrees while keeping
  runtime, task, attempt and operation identities distinct? Can issue cleanup and
  orchestrator restart preserve uncommitted work and evidence under the assigned
  cleanup/retention ownership?
- Can a disposable real integration demonstrate isolation, redaction and human
  merge gates without exposing host control or native provider credentials?

Offline mappings or upstream documentation alone cannot answer these questions.
No verified Gas Town profile is added to Feature 005's compatibility matrix, and
no current Symphony/Spec Kit pin or execution approval is changed here.
