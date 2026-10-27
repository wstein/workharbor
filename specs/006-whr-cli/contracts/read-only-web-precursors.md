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
| Enrollment/trust read-only | Existing T005/T008 records and the owner shell; approved summaries and page scope | T011 client, full T012 lifecycle/retry/progress journeys and downstream work |

Each package would be independently reviewed and tested against its own scope.
An accepted precursor would not mean its parent task or prerequisite is complete.
The enrollment/trust exception would explicitly reconcile the frontend contract's
W4-after-W3 sequencing; it would not silently change the W1-W4 package graph.
No host-agent, provider or local CLI behavior enters these packages.

## Proposed usable Spec Kit source interface

Engineering recommendation: configure existing local Git objects on the server,
with an explicit product-owner binding and immutable commit. The operator chooses
the actual repository and commit at runtime; separate owner approval of each
repository/commit is not required by this proposal. Remote ingestion is outside
this first slice, rather than a second approval gate for the local read path.

Proposed interface: `SpecKit.project(principal, source)` receives a trusted
server-side descriptor containing `owner_id`, `source_id`, absolute `repository`,
full `commit_oid` and admitted artifact roots. The descriptor comes from operator
runtime configuration, never browser input. No source registry, new storage or
host command is introduced. The shared service authorizes
`artifact_projection_read` for dashboard, CLI-owner and owner-browser credentials,
checks the descriptor owner against the principal, and resolves the exact existing
commit before reads. The dashboard page admits only its dashboard credential.

Read blobs from the commit, not the worktree/index or a moving branch. Propose
roots `specs/`, `docs/quality/` and `docs/exec-plans/`; recognize feature directories
containing `spec.md`, `plan.md` and `tasks.md`, and retained Markdown evidence.
Reject absolute/traversing paths and symlink/submodule entries. Use fixed Git argv,
no shell or network, and isolated helper/config behavior; never run hooks, filters,
text conversion or repository-configured helpers. No checkout, fetch or repository creation.

Proposed engineering bounds: one-second total projection deadline, at most 64
artifact blobs, 256 KiB per blob and 8 MiB total blob bytes; enumeration at most
1,000 entries and 1 MiB of path data. Enforce limits while reading. Timeout,
missing objects, unsupported layout/encoding or exceeded limits give explicit
unavailable/incomplete results, never an empty-success or partial-completion claim.
These are proposed values for this read path, not inherited owner decisions.

Return source identity, commit OID, relative path, blob OID, SHA-256 of exact bytes,
parser capability, observation time, availability and escaped inert Markdown text.
Show specs/plans/tasks and repository-backed review evidence on a usable dashboard;
missing workspace/agent evidence is unavailable. A checkbox is not approval and a
repository evidence file is not an authenticated forge review. Apply FR-022 visible
control escaping followed by HTML escaping; do not execute Markdown or fetch links.
Retain no projection storage and expose no absolute repository path in the page.

This interface and its proposed values are reviewed with this proposal; no extra
future contract approval is required for the described source slice. Acceptance
still requires the authoritative execution amendment below and its exact-head
approval before implementation. Operator runtime selection grants read access
only, not repository execution, lifecycle authority or artifact approval.

## Enrollment and trust: existing source, read-only projection

Use existing retained enrollment, host identity/security and revision approval
records through the shared authorized read service. Add bounded owner-scoped
summary helpers in Hosts and ConfigTrust, with Accounts authorization and safe
audit action names. The accepted frontend contract proposes dedicated
`enrollment_status_read` and `revision_approval_read` actions for owner-browser
and CLI-owner access, denying dashboard credentials. Preserve that separation.

Summaries may describe retained IDs, timestamps, enrollment/identity state and
approval identity. Exclude enrollment codes, secret hashes, CSRs, certificates
and raw configuration descriptors. No new storage is required. A retained approval
record does not prove trust in current workspace bytes. No read-page handler may
issue, approve, revoke, provision or dispatch, including through forged events.

## Approval and follow-up artifacts

| Item | Current state |
| --- | --- |
| Isolated execution exceptions and sequence | PROPOSED; owner acceptance pending |
| Server-side local source descriptor and bounds | PROPOSED engineering interface above |
| Inert Markdown and unavailable status scope | PROPOSED; owner acceptance pending |
| Operator runtime repository/commit | Runtime configuration, not per-source owner approval |

After owner acceptance, record the approved exception in [spec](../spec.md),
[plan](../plan.md), package scope in [tasks](../tasks.md) and the frontend contract's
sequencing section. Expand [ArtifactProjection](../data-model.md) only with the
reviewed source shape. Amend [owner API](owner-api.md) if adding HTTP routes;
otherwise retain the shared-service boundary for views and later controllers.
This document alone changes none of those authoritative artifacts.

Each later implementation needs failing-first tests, scoped credential/owner
checks, inert hostile-text rendering, mutation refusal without domain changes,
correct denial audit, unchanged transport policy and honest availability states.
Native CI and exact-head independent review remain PR gates. Actual browser
journeys and host evidence remain required where the parent task demands them.
Task/progress ticks, lifecycle delivery and pilot acceptance remain out of scope.
