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

Fields: profile_digest, action, permission, capability, authorization, checked_at, evidence_digests, eligible,
blockers, next_action, provenance. eligible is true only when permitted + verified + active applies to the exact
action and all applicable task/source/ownership prerequisites also pass. Pure profile preview may report its three
axes without asserting task eligibility. Invalid inputs return sanitized blocker codes; do not echo raw content.

## TaskClaim and Transition

Key: host-pinned repository + feature + task_id. Claim fields: executor_id, host_id, worktree_id, guard_attempt_id,
profile_digest, generation (strictly increasing), state, grant_id and last_confirmed_operation_id. Task identity is
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
