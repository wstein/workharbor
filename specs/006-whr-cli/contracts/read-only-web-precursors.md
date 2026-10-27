# Read-only web precursors: approval proposal

Status: **PROPOSED**, 2026-10-27. This document authorizes no product code,
runtime activation, task completion or change to existing prerequisites.
All options, recommendations and approval fields below remain proposed.

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

Engineering recommendation: configure existing local Git objects on the server,
with an explicit product-owner binding and immutable commit. **O1 remains open**:
recommend operator selection at runtime without separate per-source owner approval.
This is proposed, not a settled authorization policy. Remote ingestion is outside
this slice; the existing product-read approval gate remains until O1 is answered.

Proposed interface: `SpecKit.project(principal, source)` receives a trusted
server-side descriptor containing `owner_id`, `source_id`, absolute `repository`,
full `commit_oid` and admitted artifact roots. The descriptor comes from operator
runtime configuration, never browser input. No source registry, new storage or
host command is introduced. The shared service authorizes
`artifact_projection_read` for dashboard, CLI-owner and owner-browser credentials,
checks the descriptor owner against the principal, and resolves the exact existing
commit before reads. This new action proposes a narrow exception to the frontend
contract's dashboard-denied-for-every-new-read rule, as required for FR-033 artifact
reads. Enrollment/trust read actions remain dashboard-denied. The dashboard page
admits only its dashboard credential.

Read blobs from the commit, not the worktree/index or a moving branch. Propose
roots `specs/`, `docs/quality/` and `docs/exec-plans/`; recognize feature directories
containing `spec.md`, `plan.md` and `tasks.md`, and retained Markdown evidence.
Project one server-selected feature/evidence subset under these roots, not all files.
Reject absolute/traversing paths and symlink/submodule entries. Use plumbing-only
fixed argv in a private ephemeral Git context isolated from source config and refs;
read admitted objects without changing source files. This temporary helper state
is discarded after the read, not persistent projection storage. Scrub inherited
`GIT_*` variables, then set `GIT_CONFIG_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL=/dev/null`,
`GIT_NO_REPLACE_OBJECTS=1` and `GIT_NO_LAZY_FETCH=1`; pass `--no-replace-objects`,
`--no-lazy-fetch` and `-c protocol.allow=never`. Source-local config is read only
as data for admission, never executed or inherited. Reject grafts, file/environment/
HTTP alternates and unsupported promisor sources; do not follow replace refs.
Admit canonical lowercase full OIDs matching the repository object format: 40 hex
for SHA-1 or 64 for SHA-256. Ref names, revision expressions and tags are refused;
verify exact OID equality and object type commit before projection, without peeling.
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

The interface and values are proposed for review here. Subject to the owner answer
to O1 and exact-head acceptance, recommend no additional per-source approval for
this slice. No such exception is settled. Runtime source selection would grant
read access only, not execution, lifecycle authority or artifact approval.

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
| O1: runtime selection without per-source approval | OPEN; recommend operator configuration, owner answer pending |

This proposal includes matching exception pointers in [spec](../spec.md),
[plan](../plan.md), [tasks](../tasks.md) and the frontend contract. Independent
exact-head review, explicit owner acceptance of this PR's exact revision, merge
and an explicit O1 answer would activate only these isolated read-only exceptions.
Until then current task and product-read approval gates apply. Recommend accepting
the proposed source interface here without another source-contract approval loop;
that recommendation is not settled. Later HTTP routes retain the
[shared owner API](owner-api.md).

Implementation needs a dashboard-specific mount; owner-only OwnerAuth cannot
admit dashboard credentials. Reusing DashboardSocket requires tested ingress and
no-write behavior; do not assume OwnerSocket's filter also protects it.
Write failing-first scope, escaping and read-only tests, including direct calls to
actual implemented mutation routes with dashboard credentials and denial audit.
Do not claim absent future routes were tested. Cover SC-009 cross-origin read,
form post, DNS rebinding and streaming upgrade; hostile Host and inert attributes;
and dashboard forged events with no mutation, crash or payload logging. Pin Git
isolation, replace/graft, lazy-fetch, alternates and exact-OID refusals with
failing-first tests. Retain existing transport policy.
Native CI and exact-head independent review remain PR gates. Browser/owner-host
journeys, full-task ticks, lifecycle delivery and pilot acceptance remain separate.
