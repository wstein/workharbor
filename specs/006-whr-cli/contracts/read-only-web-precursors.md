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
| Spec Kit read-only | T006 authentication and existing T005 persistence; approved source contract below | T019 agent interoperation, full T020 acceptance and downstream work |
| Enrollment/trust read-only | Existing T005/T008 records and the owner shell; approved summaries and page scope | T011 client, full T012 lifecycle/retry/progress journeys and downstream work |

Each package would be independently reviewed and tested against its own scope.
An accepted precursor would not mean its parent task or prerequisite is complete.
The enrollment/trust exception would explicitly reconcile the frontend contract's
W4-after-W3 sequencing; it would not silently change the W1-W4 package graph.
No host-agent, provider or local CLI behavior enters these packages.

## Spec Kit source: owner choice still required

T020 requires immutable Git provenance but has no implemented projection source
interface. A parser operating only on fixtures is groundwork, not a usable page.
The browser must not choose arbitrary repository paths, URLs or credentials.

| Option | Proposed source boundary |
| --- | --- |
| A, recommended | Existing operator-authorized local Git objects, explicitly bound to the product owner and an exact immutable commit |
| B | Defer the real source path until a separate remote-ingestion contract is reviewed and approved |

For A, the owner must identify the actual source and permission to expose it.
Do not infer the development checkout, add a source registry or choose a config
filename, storage mechanism or host command through this proposal. No remote
fetch, credential forwarding, repository creation or new storage is implied.
The source must be readable by the existing control plane's authorized service;
its concrete input mechanism remains part of the source contract, not a decision
recorded here. A moving branch is not an immutable revision identity.

Recommend showing repository-backed specs, plans, tasks and review evidence from
that commit, with revision/content identity and evidence source/time. Unsupported
layouts, absent objects and missing workspace/agent evidence remain explicitly
unavailable; they never imply completed tasks, approval or zero active sessions.
Recommend rendering Markdown as escaped inert text initially. Rich rendering
would need a separately reviewed rendering policy, not an implicit dependency.

Before production code, a precise reviewed source contract must define supported
layouts and identities, source admission, read/size/count/deadline bounds,
provenance and unavailable/error behavior. This proposal supplies no numerical
bounds or source configuration and does not authorize an arbitrary implementation.

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
| Actual Spec Kit source and owner binding | PROPOSED option A; actual source unspecified |
| Inert Markdown and unavailable status scope | PROPOSED; owner acceptance pending |
| Exact source contract and implementation scope | Not yet supplied or approved |

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
