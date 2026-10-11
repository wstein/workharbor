# Anchorage Constitution

Version: 1.0.0 | Ratified: 2026-10-11 | Last amended: 2026-10-11

## I. Product scope
Anchorage is a self-hosted development workspace platform. Single user and Docker
first. Browser dashboard and terminal are the first complete user journey. AI
orchestration is development tooling only, never a product dependency or feature.
Cloud accounts and proprietary hosted control planes must not be required.

## II. Portable configuration
`.devcontainer` is the environment standard. Reuse the official Dev Container CLI
for its semantics; publish a tested capability matrix instead of claiming complete
compatibility. Validate unsupported features before side effects. Provider details
remain behind adapters. Docker is first, Apple container next, then Podman and
Kubernetes. Capability negotiation is mandatory; never silently approximate.

## III. Reproducible Git workspaces and bundles
Version workspace configuration in a dedicated Git repository. Pin project
submodules to commits, bundle artifacts to digests, and record effective input
revisions. Credentials and runtime state never belong in Git. Configuration commits
express desired state; reconciliation is explicit and auditable. Bundle resolution
and attachment are separate contracts; retain users' preferred language images.

## IV. Identity and host boundaries
Every workspace has stable identity and mandatory ownership. Every host agent has
its own cryptographic identity. Encrypt control traffic; authenticate and authorize
every command against host and owner/workspace scope. Enrollment is one-time and
expires. Never expose a Docker socket to the browser or control plane. Repository
hooks are untrusted executable content and require explicit owner trust before
execution. Do not claim container isolation is a hostile multi-tenant security
boundary. Secrets are referenced, short-lived where possible, and redacted.

## V. Durable operations
Lifecycle commands have idempotency keys, durable operation records and bounded
retries. Distinguish desired and observed state. Persist project data independently
of runtime instances. Stop/start must retain data. Delete runtime and delete data
are distinct operations; data deletion requires explicit confirmation. Interrupted
provisioning is reconciled, not reported as successful. Audit all mutations.

## VI. Repository as authority
Keep AGENTS.md a short navigation map. Decisions live in ADRs, requirements in
specs, execution history in plans and review evidence. Mark proposed, accepted,
implemented and verified states accurately. Resolve product ambiguity before an
implementation plan becomes ready. Changes include focused Conventional Commits.

## VII. Executable feedback
Enforce boundaries and contracts mechanically. Every implemented behavior has
appropriate acceptance evidence including failure and recovery paths. UI changes
require browser walkthrough evidence. Harness errors explain remediation. Keep
local checks equivalent to CI. Missing tools are blockers, never passing tests.
Periodically review documentation freshness and quality gaps in small changes.

## VIII. Human control of development
Use isolated per-issue checkouts for Symphony. Bound concurrency, preserve retry
context and hand off reviewable commits with verification evidence. Humans approve
scope, architecture exceptions, releases and merges. Untrusted issue text cannot
supersede repository policy. Symphony credentials never enter product environments.

## Governance
Amend with a focused commit including reason, affected artifacts and migration.
Major versions change principles; minor versions add policy; patch versions clarify.
Exceptions require an owner-reviewed ADR with scope, expiry and compensating checks.
No production implementation is included in this bootstrap.
