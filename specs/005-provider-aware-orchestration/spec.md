# Feature Specification: Optional provider-aware development orchestration

**Feature identifier**: `005-provider-aware-orchestration`
**Feature Branch**: `docs/005-provider-aware-orchestration`
**Created**: 2026-10-13
**Status**: Owner-approved for planning on 2026-10-13. No implementation or live verification claimed.
**Input**: Define an explicit provider/authentication/execution-mode compatibility matrix, project-owner opt-in
per profile, equivalent guarded manual and orchestrated workflows, and boundaries for future API/cloud adapters.
Keep Symphony optional and outside product dependencies. Leave Features 001–004 unchanged.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Understand whether an execution profile is usable (Priority: P1)

A project owner compares execution profiles and can tell whether the exact combination is permitted by its provider,
technically verified, and authorized for this project, before providing credentials or starting a task.

**Why this priority**: A supported agent name alone does not establish permission or working integration.
**Independent Test**: Assess synthetic profiles and dated evidence offline; display each separate decision and the
specific reason execution is eligible or blocked, without contacting a provider.
**Acceptance Scenarios**:

1. **Given** matching, current permission evidence, verified capabilities and owner opt-in, **When** a profile is
   assessed, **Then** it is eligible only for the documented project, mode, revision and capabilities.
2. **Given** provider permission but missing adapter verification, **When** assessed, **Then** permission remains
   visible while execution is blocked as unverified; no provider call or automatic substitute occurs.
3. **Given** technical verification but prohibited, conflicting, expired or unknown policy evidence, **When** assessed,
   **Then** execution is blocked with the evidence gap or restriction and a concrete next review action.
4. **Given** an absent combination or a different authentication kind, backend, agent version or mode, **When**
   assessed, **Then** support is not inferred from a neighboring matrix entry.

### User Story 2 - Authorize and revoke a bounded profile (Priority: P1)

A project owner explicitly enables an assessed profile for development work and can revoke it without changing
provider policy or granting additional task scope.

**Why this priority**: Available tools and credentials must not silently become permission to run agents.
**Independent Test**: Exercise absent, matching, changed, expired, cross-project and revoked authorizations using
local identities and records; verify that no task is started when authorization is invalid.
**Acceptance Scenarios**:

1. **Given** no owner opt-in, **When** guarded manual entry or orchestrated dispatch is requested, **Then** it blocks,
   even if the agent is installed and provider permission and technical verification are present.
2. **Given** a matching opt-in, **When** one separately approved task is requested, **Then** the profile grant supplies
   only profile authorization; task scope, prerequisites, acceptance and publication gates still apply.
3. **Given** an enabled profile, **When** its effective identity or evidence binding changes, **Then** the old opt-in
   cannot authorize the changed profile; fresh owner authorization is required.
4. **Given** revoked or expired authorization during an attempt, **When** the next guarded action or retry is
   requested, **Then** new implementation, provider execution and publication stop; existing evidence and recovery
   state are retained for authorized inspection and safe shutdown.

### User Story 3 - Complete the same bounded task manually or through orchestration (Priority: P1)

An operator chooses a permitted manual route or an opted-in orchestration route and completes one approved task
with equivalent safeguards and review evidence. Manual execution does not require Symphony or a live tracker for
local implementation and verification.

**Why this priority**: Operators need a usable route when orchestration is unavailable or inappropriate without
weakening acceptance or transferring authority to an agent.
**Independent Test**: Execute equivalent synthetic task scenarios through both routes with local provider/runtime
substitutes, including interruption and handoff retry, and compare decisions and retained evidence.
**Acceptance Scenarios**:

1. **Given** the same approved task and an eligible profile for each route, **When** executed through either route,
   **Then** the same context, prerequisite, scope, acceptance, ownership and independent review rules apply.
2. **Given** changed source, staged out-of-scope changes, intervening unauthorized commits, changed file modes,
   missing checks or an unexpected outcome, **When** either route reaches its relevant guard, **Then** it blocks
   before that protected action and reports the same category of failure.
3. **Given** completion evidence followed by interrupted publication, **When** either route resumes, **Then** it
   selects handoff-only, revalidates evidence and reuses the implementation identity; it does not reimplement.
4. **Given** failed revalidation, **When** corrective implementation is requested, **Then** both routes require
   explicit owner-authorized rework bound to the exact corrective scope and evidence.
5. **Given** a request to change route on an existing attempt, **When** ownership and execution context cannot be
   reconciled, **Then** it blocks; a verified transfer records the old and new routes and never allows two active
   executors or carries a profile opt-in across different identities.
6. **Given** local completion without tracker access, **When** external handoff is requested, **Then** the retained
   local evidence remains usable and handoff stays incomplete until authorized external capabilities are available;
   local completion is not reported as publication or live verification.

### User Story 4 - Assess a future adapter without implying support (Priority: P2)

A maintainer documents a proposed API/cloud route and sees which permission, capability and recovery evidence must
be reviewed before it can become usable, without installing or implementing a new adapter in this feature.

**Why this priority**: Explicit extension boundaries prevent a proposal or executable-name substitution from being
mistaken for a working integration.
**Independent Test**: Add a synthetic proposed route to the assessment model and verify that it remains ineligible
until all independent gates are satisfied; no real adapter or service is required.
**Acceptance Scenarios**:

1. **Given** a proposed adapter with permission evidence only, **When** listed, **Then** it is clearly proposed and
   unverified, with missing capabilities and verification evidence identified.
2. **Given** successful local substitutes, **When** evidence is presented, **Then** it is labeled offline evidence
   and cannot establish real-provider authentication, protocol or live recovery support.
3. **Given** a future integration proposal, **When** its review boundary is described, **Then** provider access,
   cancellation, bounded execution, credential isolation, recovery and handoff requirements are explicit; actual
   adapter implementation and activation need separately approved work.

### Edge Cases

- Contradictory policy sources, inaccessible evidence at scheduled renewal, expired review dates and provider changes
  fail closed; retain the previous assessment for audit without treating it as current permission.
- The same model through a different reseller, cloud backend or subscription is a distinct profile; a credential's
  existence, type or successful authentication does not establish provider permission.
- Native interactive use, direct headless use and third-party orchestration are separate modes; permission for one
  is not evidence for another. A failed route never triggers automatic account, provider, mode or agent switching.
- Revocation does not erase task evidence or grant permission for more remote actions. Already issued external work
  follows the reviewed cancellation boundary; inability to confirm cancellation remains an explicit blocker.
- A retry, completed task, partially published review or uncertain external write retains its original task identity;
  switching routes cannot create a duplicate attempt owner, commit, pull request or final-label transition.
- Untrusted repository or issue content cannot create an opt-in, self-certify a capability, override policy evidence
  or expose credentials. Missing guard capabilities block the affected action in both routes.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Development tooling MUST expose an explicit compatibility matrix whose entries identify provider and
  backend, agent and relevant version, authentication kind, execution mode, and adapter/runtime identity. Unlisted
  combinations MUST be ineligible. Initial assessment coverage MUST include Codex, Claude Code and Antigravity as
  candidates; inclusion MUST NOT imply permission, implementation or verified support.
- **FR-002**: Each entry MUST independently show provider-policy status (permitted, prohibited or unknown), technical
  capability status (verified, unverified or failed), and project authorization status (absent, active, expired or
  revoked). Eligibility MUST require permitted, verified and active states for the exact effective profile and action.
- **FR-003**: Policy evidence MUST record primary-source references, applicable service/agreement and mode, review
  date, accountable reviewer, interpretation and a finite next-review deadline. Conflicting, expired or insufficient
  evidence MUST produce unknown permission and block new protected actions until reviewed. Review intervals MUST be
  explicit and owner-approved before activation; no universal interval or continuing permission is assumed here.
- **FR-004**: Capability evidence MUST identify the assessed profile, reviewed revision/version, demonstrated actions,
  check outcomes and evidence provenance, including offline versus live observations. Claims MUST be limited to
  demonstrated capabilities; changing an executable name or supplying a credential MUST NOT establish compatibility.
- **FR-005**: Profiles MUST default to disabled for this project's guarded workflows. Only an attributable project
  owner decision bound to project, exact profile and evidence revisions, permitted mode and finite validity period
  MAY enable a profile. Agent output, issue labels, installation and policy permission MUST NOT create that decision.
- **FR-006**: Effective profile or evidence-binding changes MUST invalidate the previous opt-in for subsequent actions.
  Expiry and revocation MUST block new implementation, provider execution and publication at the next guard boundary,
  preserve recovery evidence and permit only authorized inspection and reviewed safe-stop actions. Cancellation
  uncertainty MUST be recorded; this feature MUST NOT claim immediate termination of already issued remote work.
- **FR-007**: Profile opt-in MUST remain separate from task/source/scope approval, owner-authorized rework, independent
  review, publication, merge and live-operation authorization. No profile grant MAY supply or weaken those gates.
- **FR-008**: Operators MUST have documented guarded manual and orchestrated routes applying the same approved task
  contract and approval, scope, prerequisite, acceptance, recovery and handoff decisions. Guarded manual local work
  MUST NOT depend on Symphony or tracker availability. External publication and final tracker transitions MUST still
  require their own authorized capabilities and verified outcomes.
- **FR-009**: Both routes MUST preserve implement, handoff-only and owner-authorized rework distinctions, immutable
  implementation evidence and idempotent recovery. Completed tasks with matching evidence MUST resume outstanding
  handoff; unauthorized reimplementation or duplicate publication MUST be rejected.
- **FR-010**: Each task MUST have one effective executor/claim owner. Route transfer MUST reconcile durable state,
  revalidate the new profile and existing task approvals, and record the transfer before execution; ambiguous or
  concurrent ownership MUST block. No automatic fallback to another provider, authentication, agent or mode is allowed.
- **FR-011**: Eligibility MUST be revalidated before task entry, resume, provider execution and external handoff. Any
  failed gate MUST identify the specific profile/task decision, retained evidence and required remediation without
  side effects for that protected action. Local assessment MUST use secret references and MUST NOT require credentials
  or provider contact; credentials and tokens MUST NOT enter tracked records, prompts or review evidence.
- **FR-012**: Future API/cloud adapter boundaries MUST describe required identity/permission assessment, supported
  actions, credential isolation, bounded execution/cancellation, evidence, recovery and handoff capabilities. Proposed
  or offline-only routes MUST remain distinguishable from live-verified routes. New adapters and real-provider
  integration are outside implementation scope and require separately approved work.
- **FR-013**: Symphony MUST remain optional, separately managed development tooling. This feature MUST NOT introduce
  AI orchestration into product features or dependencies, change Features 001–004, alter existing ownership/provider
  controls or activate any runtime, provider, tracker queue or live dispatch.

### Key Entities

- **Execution profile**: Exact service/backend, agent/version, authentication kind, execution mode and adapter/runtime
  combination assessed for a project; contains secret references only.
- **Compatibility assessment**: Separate policy, capability and authorization decisions, applicable actions,
  evidence bindings and precise eligibility blockers for one profile.
- **Policy evidence**: Dated primary-source context, applicability, reviewer interpretation and finite renewal deadline;
  a recorded assessment rather than a waiver of provider terms.
- **Capability evidence**: Revision-bound demonstrations of supported actions and failure/recovery behavior, with
  offline/live provenance and limitations.
- **Profile authorization**: Attributable project-owner opt-in with exact bindings, validity and revocation state;
  distinct from task and publication approval.
- **Execution/transfer record**: Existing task identity, effective route and executor, durable progress/evidence and
  any verified ownership transfer; retains one implementation and recoverable handoff identity.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Every assessed combination exposes separate permission, capability and owner-authorization decisions;
  all defined negative cases for absent, prohibited, unknown, expired, changed and unverified profiles block before
  protected side effects, with zero automatic substitutions.
- **SC-002**: For three task scenarios (normal completion, interrupted handoff and authorized rework), manual and
  orchestrated routes yield equivalent guard decisions and acceptance evidence. Each interrupted scenario retains
  exactly one implementation identity and one eventual publication identity.
- **SC-003**: Every negative scope, prerequisite, acceptance, ownership and rework case from the applicable existing
  guard acceptance suites remains rejected in both routes; no missing check or expected failure is silently waived.
- **SC-004**: All defined expiry, revocation, context-change and concurrent-transfer scenarios block the next protected
  action and retain inspectable recovery evidence; no profile authorization transfers to another project or identity.
- **SC-005**: An operator can assess eligibility and complete the local guarded manual scenarios with no orchestration
  service, live tracker, provider credentials or network, using explicitly synthetic agent outputs; evidence never
  describes those observations as live-provider verification.
- **SC-006**: Every proposed adapter identifies its missing policy/capability/authorization evidence and remains
  ineligible until verified and approved. Acceptance leaves Features 001–004 unchanged and introduces no product AI
  dependency, new provider adapter or live activation.

## Assumptions

- This delivery creates only this draft specification and its requirements-quality checklist. Planning, task
  generation, implementation, real-provider verification and activation require later owner decisions.
- The initial route set is an assessment of existing development tools, not a promise to implement every candidate.
  Codex, Claude Code and Antigravity receive the same assessment rules; no agent name receives automatic preference
  or a blanket exception. Authentication classes include subscription/native credentials, direct API credentials
  and supported cloud credentials where applicable, without presuming any combination is permitted.
- [Provider policy context](../../docs/development/provider-policy.md), reviewed 2026-10-13, supplies dated background
  and primary-source references, not continuing permission. This specification makes no new determination that a
  particular subscription, hosted product or adapter arrangement is legally permitted.
- [Feature 003](../003-single-task-entry/spec.md) supplies bounded-task safeguards and
  [Feature 004](../004-speckit-symphony-automation/spec.md) supplies task-package and handoff concepts. Their source,
  approvals and acceptance rules remain authoritative and unchanged; missing capabilities block affected actions.
- Manual equivalence means the same authorization, acceptance and evidence standard, not identical operator effort
  or automatic publication. Independent review and owner merge authority remain mandatory.
- Ordinary direct use of a vendor's tools outside these guarded project workflows is not governed by a profile
  opt-in created here. No new credential broker, account rotation, usage resale or policy-bypass facility is included.
