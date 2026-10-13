# Research: Provider-aware development orchestration

**Date**: 2026-10-13 | **Status**: Design research; no provider permission or runtime activation granted.

## 1. Reuse guards without imposing Symphony on manual work

**Decision**: Add a provider-neutral assessment and coordinator. Delegate manual guard actions through the existing
Feature 003 CLI. Use Feature 004 readiness only for its applicable orchestrated/external actions.
**Rationale**: `scripts/task_package.py` assess_readiness requires Symphony/workflow/config identities and authenticated
tracker observations. Calling it universally would violate FR-008. `scripts/single_task.py` already owns begin,
inspect, verify, complete, record-commit, resume, rework and status sequencing.
**Alternatives considered**: Change Feature 004 to make its fields optional (violates preserved scope and weakens
its contract); duplicate guard logic (creates divergent acceptance/recovery behavior).

## 2. Treat permission, capability and authorization separately

**Decision**: Three independent assessment axes, exact profile/evidence digests, action-specific capabilities and
explicit offline/live provenance. Candidate agent coverage makes no support claim.
**Rationale**: Existing `task_package.CapabilityEvidence` and host ports separate observations from package assertions.
[Provider context](../../docs/development/provider-policy.md) is dated 2026-10-13 research, not current execution
permission. This plan adds no provider-specific legal determination; actual assessments require renewed primary-source
review applicable to the exact service, authentication and mode.
**Alternatives considered**: Blanket agent allow/deny lists; treating API credentials or successful login as permission;
relabeling a local fixture pass as real adapter verification. All lose necessary distinctions.

## 3. Keep authority outside repository-controlled data

**Decision**: Trusted host code supplies owner authentication, policy/capability attestations, clock/revocation
observations and claim authority. Checked-in profiles supply inert proposals only. Local CLI defaults to unavailable
mutation authorities; fixtures are confined to synthetic repositories and cannot authorize real provider access.
**Rationale**: `task_automation.main` accepts injected host callbacks; `task_tracker.ApprovalAuthority` is already
host-pinned but task-specific. It cannot directly authenticate a different profile-grant schema. Use a separate
profile-authority port and preserve the existing task authority.
**Alternatives considered**: An --approve flag, issue label, JSON owner_ref or custom plugin path as authority; all
allow untrusted content to promote itself to authorization.

## 4. Preserve ownership and durable recovery

**Decision**: A new local ledger owns profile claims and transition intents; existing Feature 003 and Feature 004
stores retain their own attempt and handoff authority. Per-task coordinator lock precedes existing guard/handoff
locks; short SQLite transactions precede/follow protected calls, never span arbitrary subprocess execution.
**Rationale**: `single_task_state.open_store` provides locked durable local state. `task_handoff.Recovery` rejects a
changed checkout/host/process owner and has no transfer/release API. Cross-store work therefore needs intents and
reconciliation rather than a claimed atomic transaction.
**Alternatives considered**: Editing existing SQLite owners, resetting attempts, treating lock release or expired
leases as proof an executor stopped. These can duplicate or orphan implementation/publication.

## 5. Bound transfer support explicitly

**Decision**: Support tested same-owner route changes that preserve all existing store identities. A changed owner
requires a trusted fencing/cessation authority and compatible downstream transfer capability. If an existing
Feature 004 owner is fixed, that transfer blocks, even if the new ledger alone could transfer its claim.
**Rationale**: A profile generation cannot fence a process which bypasses the wrapper or transfer a downstream owner
without its API. Single-host POSIX locking is not distributed ownership. Fixtures test rejection of missing authority
and successful transfers only where every participating store permits them.
**Alternatives considered**: Promise universal cross-host transfer, steal a stale lock, or silently open another PR.

## 6. Keep external handoff honest

**Decision**: Local completion can retain an awaiting-publication result without tracker access. External publication
and label transitions delegate to verified host ports and existing recovery checks; missing capability blocks only
that affected step. No new provider transport is implemented.
**Rationale**: `task_tracker.ConditionalLabels` requires a conditional full-set replacement or serialization of all
writers. Ordinary read/PUT/read cannot establish that guarantee. Existing safe-label limitations remain blockers.
**Alternatives considered**: Add stock GitHub writes to appear operational, silently skip final labels, or call local
fixture outcomes live success.

## 7. Correlate guard creation when its response is lost (U1)

**Decision**: Retain the complete creation intent/pre-call attempt set, then use a pinned read-only guard-store reader
for lost-response lookup. Existing begin/rework choose random IDs internally; CLI status/resume require a known ID.
**Rationale**: single_task_state.open_store(require_existing=True) supplies locked validated reads; read_attempt and
read_events validate retained context/events. active_attempt returns one row, so uniqueness needs an explicitly
reviewed read-only schema-v1 enumeration of IDs in that same locked store. Exact context/baseline/predecessor and
exclusive-creator proof correlate one new record to the intent. No match or ambiguity remains blocked.
**Alternatives considered**: Retry creation, take the first active row, mutate the guard schema, or add a new guard
CLI option. These duplicate attempts, lose correlation or change protected Feature 003 implementation.

## 8. Use one action vocabulary and two eligibility flags (I1/A1)

**Decision**: The CLI/host action table defines exact tokens, operation/boundary mapping, capability requirements and
separate execution, observation and safety authority. The model and CLI use only profile_eligible/task_eligible.
**Rationale**: An entry boundary is not a begin grant alias; a profile-only preview cannot establish task eligibility.
Inspection of revoked work and future safe-stop require their own attributable authority, not an execution-grant
exception that silently re-enables implementation. Both execution flags stay false for observations/safety.
**Alternatives considered**: Free-form action aliases, a single eligible flag, or accepting revoked execution grants
for other actions. These conflate intent, capabilities and authorization.

## Evidence and resolved questions

Research used the accepted local source at main `d646bd565daff85b03519ac1873b534e9c0c2288`, especially
scripts/single_task.py, single_task_state.py, task_package.py, task_automation.py, task_tracker.py and task_handoff.py.
Independent read-only integration research confirmed the interfaces and ownership restrictions. No unresolved design
clarification remains. Provider-specific permission and host capabilities are runtime inputs requiring their own
review, not missing design decisions or permission inferred by this plan.
