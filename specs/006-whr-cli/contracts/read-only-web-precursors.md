# Read-only web precursors: approval proposal

Status: **PROPOSED**, 2026-10-27. This document authorizes no product code,
runtime activation, task completion or change to existing prerequisites.
The sequencing and implementation proposal remains unapproved. O1 alone is
decided by the owner as recorded below.

## Purpose and retained gates

The proposed delivery order is a Spec Kit read-only precursor, followed by an
owner enrollment/trust read-only precursor. These are limited parts of
[T020 and T012](../tasks.md), not substitutes for their full acceptance.
The authoritative dependencies remain T020 after T019 and T012 after T011.
Their checkboxes and downstream prerequisites stay unchanged and open.

The [accepted frontend contract](web-frontend-first-increment.md) distinguishes
owner pages from the dashboard's read-scoped credential. Its shared authorized
read service decision remains unchanged. This proposal does not authorize
lifecycle controls, trust approval, enrollment issuance, terminal or relay work.
Real browser and owner-host acceptance remain separate; no precursor closes them.

## Proposed execution exceptions

| Precursor | Proposed prerequisites | Scope retained outside this package |
| --- | --- | --- |
| Spec Kit read-only | T006 authentication and existing T005 persistence; source interface below | T019 agent interoperation, full T020 acceptance and downstream work |
| Enrollment/trust read-only | Existing T005/T008 records, partial T009 ConfigTrust records and owner shell; reuse exception below | T011 client, full T012 lifecycle/retry/progress journeys and downstream work |

Each package would be independently reviewed and tested against its own scope.
An accepted precursor would not mean its parent task or prerequisite is complete.
The enrollment/trust exception would explicitly reconcile the frontend contract's
W4-after-W3 sequencing; it would not silently change the W1-W4 package graph.
No host-agent, provider or local CLI behavior enters these packages.

## Proposed usable Spec Kit source interface

**O1 DECIDED — owner, 2026-10-27:** use only one pre-configured, owner-bound
source: one repository descriptor plus one immutable commit, set once by the owner
in configuration. A new source requires the owner's one-time configuration
approval, consistent with [ADR 0011](../../../docs/adr/0011-readonly-spec-dashboard.md).
Replacing the repository, commit or owner binding is a source change and requires
that approval. There is no runtime choice of local repository or commit, no branch
tracking, and no request-supplied source override. Source configuration approval
permits these reads; it does not approve artifact contents or authorize execution.
This decision does not accept the proposed sequencing/reuse exception.

Proposed interface: `SpecKit.project(principal)` loads that single approved source
from owner-controlled configuration. Its descriptor contains `owner_id`,
`source_id`, absolute `repository`, full `commit_oid` and admitted artifact roots.
The page, CLI/API caller and request parameters cannot supply or replace this
descriptor. Missing or unavailable configured sources remain unavailable; never
infer another source from the working directory or fall back to another commit.
No source registry, new storage or host command is introduced. The shared service
authorizes
`artifact_projection_read` for dashboard, CLI-owner and owner-browser credentials,
checks the descriptor owner against the principal, and resolves the exact existing
commit before reads. This new action proposes a narrow exception to the frontend
contract's dashboard-denied-for-every-new-read rule, as required for FR-033 artifact
reads. Enrollment/trust read actions remain dashboard-denied. The dashboard page
admits only its dashboard credential.

Read blobs from the commit, not the worktree/index or a moving branch. Propose
roots `specs/`, `docs/quality/` and `docs/exec-plans/`; recognize feature directories
containing `spec.md`, `plan.md` and `tasks.md`, and retained Markdown evidence.
Project one admitted feature/evidence subset within that fixed repository and
commit, not all files. This cannot select a different repository or revision.
Reject absolute/traversing paths and symlink/submodule entries. Use plumbing-only
fixed argv in a private ephemeral Git context isolated from source config and refs;
use a helper Git directory with fixed minimal config and the admitted object
format, no source config/refs/worktree. Expose the source object database read-only
through fixed `GIT_OBJECT_DIRECTORY`, never alternates. This temporary helper state
is discarded after the read, not persistent projection storage. Scrub inherited
`GIT_*` variables, then set `GIT_CONFIG_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL=/dev/null`,
`GIT_NO_REPLACE_OBJECTS=1` and `GIT_NO_LAZY_FETCH=1`; pass `--no-replace-objects`,
`--no-lazy-fetch` and `-c protocol.allow=never`. Source-local config is read only
as data for admission, never executed or inherited. Reject grafts, file/environment/
HTTP alternates, partial-clone/promisor declarations and promisor markers; do not
follow replace refs. Only known SHA-1/SHA-256 source formats read as data are admitted.
Admit canonical lowercase full OIDs matching the repository object format: 40 hex
for SHA-1 or 64 for SHA-256. Ref names, revision expressions and tags are refused;
verify exact OID equality and object type commit before projection, without peeling.
Blob OIDs must match the same format.
No shell/network, hooks, filters, text conversion, checkout or fetch. Missing Git
capability is unavailable, never a fallback to unsafe or network-enabled reads.

Proposed engineering bounds: one-second total projection deadline, at most 64
artifact blobs, 256 KiB per blob and 8 MiB total blob bytes; enumeration at most
1,000 entries and 1 MiB of path data. Enforce limits while reading. Timeout,
missing objects, unsupported layout/encoding or exceeded limits give explicit
unavailable/incomplete results, never an empty-success or partial-completion claim.
These are proposed values for this read path, not inherited owner decisions.

Return source identity, commit OID, relative path, blob OID, SHA-256 of exact bytes,
parser capability, observation time, availability and escaped plain-text Markdown source.
Show specs/plans/tasks and repository-backed review evidence on a usable dashboard;
missing workspace/agent evidence is unavailable. A checkbox is not approval and a
repository evidence file is not an authenticated forge review. Apply FR-022 visible
control escaping followed by HTML escaping. This slice performs no Markdown-to-HTML
conversion and emits no artifact-derived links or images; richer safe rendering is
not delivered here. Git stderr, argv, exceptions and absolute host/helper paths
never reach pages or logs; report safe reasons and escaped relative artifact IDs.
Retain no persistent projection storage.

The interface and bounds remain proposed for review. The configured-source policy
is the decided O1 constraint above, not an operator-selection recommendation.
No source editing, approval or repository/commit selector enters the dashboard.

## Enrollment and trust: existing source, read-only projection

Use existing retained enrollment, host identity/security and revision approval
records through the shared authorized read service. Trust also uses partial T009
ConfigTrust and `revision_trust_approvals`, introduced at
`ba761b7c1a811a7d367d5b5d28e4ca2541746e38`. Propose an explicit reuse exception
limited to those existing components; full T009 remains open. Add owner-scoped
summary helpers in Hosts and ConfigTrust, with Accounts authorization and safe
audit action names. The accepted frontend contract proposes dedicated
`enrollment_status_read` and `revision_approval_read` actions for owner-browser
and CLI-owner access, denying dashboard credentials. Preserve that separation.

Summaries may describe retained IDs, timestamps, enrollment/identity state and
approval identity. Exclude enrollment codes, secret hashes, CSRs, certificates
and raw configuration descriptors. No new storage is required. A retained approval
record does not prove trust in current workspace bytes. No read-page handler may
issue, approve, revoke, provision or dispatch, including through forged events.

## Proposed approval and activation

| Item | Current state |
| --- | --- |
| Isolated execution exceptions and sequence | PROPOSED; owner acceptance pending |
| Server-side local source descriptor and bounds | PROPOSED engineering interface above |
| Inert Markdown and unavailable status scope | PROPOSED; owner acceptance pending |
| O1: one pre-configured owner-bound repository and immutable commit | DECIDED by owner, 2026-10-27; new sources need one-time owner configuration approval |

This proposal includes matching exception pointers in [spec](../spec.md),
[plan](../plan.md), [tasks](../tasks.md) and the frontend contract. Independent
exact-head review, explicit owner acceptance of this PR's exact sequencing/reuse
exception and merge would activate only these isolated read-only exceptions. O1
is decided, but it does not supply that sequencing acceptance or approve product
implementation. Until then current task and product-read approval gates apply.
Later HTTP routes retain the [shared owner API](owner-api.md).

Implementation needs a dashboard-specific mount; owner-only OwnerAuth cannot
admit dashboard credentials. Reusing DashboardSocket requires tested ingress and
no-write behavior; do not assume OwnerSocket's filter also protects it.
Write failing-first scope, escaping and read-only tests, including direct calls to
actual implemented mutation routes with dashboard credentials and denial audit.
Do not claim absent future routes were tested. Cover SC-009 cross-origin read,
form post, DNS rebinding and streaming upgrade; hostile Host and inert attributes;
and dashboard forged events with no mutation, crash or payload logging. Pin
configured-source binding and refusal of request-supplied repositories or commits.
Pin Git isolation, replace/graft, lazy-fetch, alternates and exact-OID refusals
with failing-first tests. Retain existing transport policy.
Native CI and exact-head independent review remain PR gates. Browser/owner-host
journeys, full-task ticks, lifecycle delivery and pilot acceptance remain separate.
