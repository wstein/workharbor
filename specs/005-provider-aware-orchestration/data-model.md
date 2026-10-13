# Data model: Provider profiles and guarded execution

Status: proposed schema v1; no records, grants or implementation are created by this document.

## Common rules

Strict JSON: UTF-8, unique keys, no unknown fields, no floats/non-finite values. UTC integer timestamps; reject a
non-increasing review/validity interval, missing clock authority or observed clock rollback. Limit each input to 1 MiB,
100 profiles, 100 evidence references per record and 1,000 retained events per task before explicit archival; reject
excess without truncation. IDs use bounded ASCII identifiers; repository identity is host-pinned, not inferred from
issue text. Relative artifact paths must stay within approved roots; reject escaping, symlinked or nonregular inputs.

Identity digests use SHA-256 over canonical JSON (sorted keys, compact separators, UTF-8, no NaN). Include schema,
all effective identity fields and evidence digests; display names and read-only assessment timestamps are outside
execution binding. No checkbox/evidence normalization can change profile authority. All identities are immutable;
updates create new revisions. Credentials never appear; a secret reference is an opaque host-owned identifier and
cannot name an arbitrary file, environment variable or command from untrusted input.

## ExecutionProfile

Fields: schema_version, project_id, profile_id, provider, backend, agent, agent_version, authentication_kind,
execution_mode, adapter_revision, runtime_revision, secret_ref (nullable), policy_digest, capability_digest.
Modes: native-interactive, direct-headless, orchestrated. These are separate from task modes implement, handoff-only
and owner-authorized rework. Each profile is an exact tuple; wildcard or version-range support is rejected.
Derived profile_digest binds every field. The tuple's existence expresses a candidate, not eligibility.

## PolicyEvidence

Fields: evidence_id, profile_digest_basis (the effective identity tuple excluding evidence digests), source_refs,
source_revision_refs, service_agreement, applicable_actions, interpretation, reviewer_id, reviewed_at,
review_due_at, decision (permitted/prohibited/unknown), authority_ref.
Source revision references retain reviewable public source snapshots or content digests, not credentials or private
agreement text. The host verifies reviewer authority and provenance. Date expiry, conflicting evidence, missing
applicability or inaccessible sources at renewal produce unknown permission. A positive result applies only until
its finite deadline and only to listed actions. No automatic web scraping or permission renewal is planned.

## CapabilityEvidence

Fields: evidence_id, profile_identity_basis, implementation_revision, action_results, provenance (offline/live),
observation_refs, reviewer_id, authority_ref. Each action result has action, verified/unverified/failed and exact
outcome/evidence digest. Offline provenance can verify local guard/fixture behavior only. A real-provider action
requires trusted live evidence for that exact profile; policy permission remains a separate requirement.

## ProfileAuthorization

Fields: grant_id, project_id, profile_digest, policy_digest, capability_digest, execution_mode, allowed_actions,
owner_id, authenticated_decision_ref, issued_at, expires_at, revocation_id (nullable).
The host verifies the actual decision target/identity; submitted JSON is only a reference. Require current host owner
identity, issued_at < expires_at, now within validity, unrevoked state and exact bindings. Authorizations default absent;
expiry/revocation does not destroy history. Policy/evidence/profile changes require a new attributable grant.

## Assessment

Fields: profile_digest, action, permission, capability, authorization, checked_at, evidence_digests, profile_eligible,
task_eligible, blockers, next_action, provenance. Both eligibility fields are required booleans; no eligible alias,
null or omitted eligibility field is valid.

For an execution action, profile_eligible is true only when permitted + verified + active applies to that exact
profile/action/evidence binding. It says nothing about task approval. task_eligible is true only when profile_eligible
is true and all applicable task/source/scope/prerequisite/mode/review/ownership/capability gates have freshly passed
for the identified task and action. Therefore task_eligible=true with profile_eligible=false is invalid.

Pure assess output always has task_eligible=false because it has not validated a task. An authenticated host assessment
may report profile_eligible=true while a task is absent or blocked; the default CLI's unavailable authorities yield
both false. Authorized observations and separately authorized safe-stop are not execution eligibility: report both
false even when the requested observation succeeds. Their separate authority rules remain defined by the host contract.
Invalid inputs return sanitized blocker codes; do not echo raw content.

Action tokens, operation mappings and capability requirements are defined in the
[canonical action table](contracts/cli-and-host.md#canonical-actions-and-capability-mapping). Apply the same closed
vocabulary to policy/capability records and approval targets; execution grants accept execution-class actions only.
Boundary labels are not grant aliases. Observation/safety decisions remain separate host attestations, with exact
targets and finite validity as defined there; neither overrides an execution eligibility flag or task approval.

## TaskClaim and Transition

Key: host-pinned repository + feature + task_id. Claim fields: executor_id, host_id, worktree_id, guard_attempt_id,
profile_digest, generation (strictly increasing), state, grant_id and last_confirmed_operation_id. guard_attempt_id is
nullable only before creation is confirmed; bind it once after validated creation/lookup, never substitute another ID.
Task identity is
stable across routes and handoff. Link the existing immutable commit and Feature 004 handoff identity where present;
never replace or mutate them. Rework creates the guard's authorized successor attempt and records predecessor linkage.

States: unclaimed → claimed → action-prepared → action-confirmed. Failure with uncertain effects enters reconcile-required;
completion enters handoff-only; final verified publication enters finished. Profile revocation adds an action blocker
without deleting the task state. A crash after intent but before guard-result persistence requires reading the existing
guard/store and matching its attempt identity before recording the result; never blindly repeat begin or publication.

Transfer: claimed/handoff-only → transfer-prepared → transferred or reconcile-required. Under the coordinator task
lock, record old/new routes, generation and operation ID, prove old-executor cessation/fencing, validate new profile and
task context, check every downstream owner constraint, then commit the new generation. Uncertain cessation, divergent
guard attempts, unavailable transfer APIs or changed Feature 004 owner block and preserve the old identity. No automatic
claim stealing on expiry or process death; lock release alone is insufficient evidence of completed remote cancellation.

## Creation intent and lost-response correlation

Before begin/rework, retain an immutable creation intent: operation_id, repository/feature/task, host/worktree/executor,
claim generation, guard source revision, exact context-copy SHA-256, approval context digest, expected baseline,
entry_mode, predecessor (null for begin; exact prior_attempt_id for rework), and the validated pre-call attempt-ID set.
Context-copy SHA-256 hashes the complete canonical context; approval context digest uses the existing guard rule that
excludes approval.context_digest. Neither digest is the other. Capture the pre-call snapshot under the guard store
lock while holding the coordinator task lock; prove exclusive host ownership for the outstanding operation.

If creation commits but its response or coordinator result is lost, do not call begin/rework again. The GuardBridge's
read-only lookup opens only an existing validated guard journal under its lock and enumerates retained attempt IDs.
Use the existing store's read_attempt/read_events validation for every candidate; filter to IDs absent from the
pre-call set and match the full intent context digests, task, baseline, mode and predecessor. Host/worktree/executor
and generation must still match the claim; no other creator may have used this worktree while the intent was pending.
A matching entry event must agree with the context digest and predecessor. Adopt exactly one matching new attempt,
persist its ID and creation-confirmed result, then continue via normal resume with fresh authorization/review/checks.

No match, multiple matches, conflicting records, missing/corrupt journal or unproven creator exclusivity yields
reconcile-required; preserve the intent and block new creation. An unavailable result is not permission to retry.
A caller retaining an attempt ID must still compare it to the intent rather than replacing it with a discovered ID.
Missing source/schema compatibility blocks lookup. This correlation records evidence only and grants no task or
execution approval. Release the guard read lock before any CLI call; retain the coordinator lock throughout.

## Persistence and trust boundary

The trusted host supplies a private local state root outside the repository; no repository-controlled override.
Reject symlink components, unexpected owners/modes, hard-linked/nonregular files and unsupported filesystems.
Use owner-only directories/files, stable never-unlinked per-task flock files and SQLite FULL synchronous transactions.
Record and fsync transition intent before an external/guard action, then its confirmed result. On storage uncertainty
return a persistence blocker and prohibit side effects until reconciled. Bound database growth and preserve old events.
The database and lock protocol require local POSIX guarantees; they are not a hostile-owner sandbox or distributed lock.

Lock order: Feature 005 per-task coordinator lock → Feature 003 worktree or Feature 004 journal lock; never reverse.
Do not hold a SQLite write transaction during subprocess/provider work. Revocation authority can update independently
while an action runs; the coordinator reobserves at each next boundary. Fencing generations are checked by every
trusted side-effect port. An unwrapped process cannot be claimed fenced without actual cessation proof.

Cross-store state is reconciled by operation identity and existing read APIs, never a distributed transaction.
Existing stores remain authoritative for task/commit/handoff outcomes. The ledger cannot create missing scope,
acceptance, review, provider permission or publication evidence.
