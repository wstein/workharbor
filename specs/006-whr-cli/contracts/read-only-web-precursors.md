# Read-only web precursors: approval proposal

Status: **PROPOSED**, 2026-10-27. This document authorizes no product code, runtime
activation, task completion or change to existing prerequisites. The sequencing and
implementation proposal remains unapproved. O1 alone is decided by the owner as recorded
below.

## Purpose and retained gates

The proposed delivery order is a Spec Kit read-only precursor, followed by an owner
enrollment/trust read-only precursor. These are limited parts of [T020 and
T012](../tasks.md), not substitutes for their full acceptance. The authoritative
dependencies remain T020 after T019 and T012 after T011. Their checkboxes and downstream
prerequisites stay unchanged and open.

The [accepted frontend contract](web-frontend-first-increment.md) distinguishes owner
pages from the dashboard's read-scoped credential. Its shared authorized read service
decision remains unchanged. This proposal does not authorize lifecycle controls, trust
approval, enrollment issuance, terminal or relay work. Real browser and owner-host
acceptance remain separate; no precursor closes them.

## Proposed execution exceptions

| Precursor | Proposed prerequisites | Scope retained outside this package |
| --- | --- | --- |
| Spec Kit read-only | T006 authentication, existing T005 persistence and partial T009 trust persistence; source interface below | T019 agent interoperation, full T020 acceptance and downstream work |
| Enrollment/trust read-only | Existing T005/T008 records, partial T009 ConfigTrust records and owner shell; reuse exception below | T011 client, full T012 lifecycle/retry/progress journeys and downstream work |

Each package would be independently reviewed and tested against its own scope. An
accepted precursor would not mean its parent task or prerequisite is complete. The
enrollment/trust exception would explicitly reconcile the frontend contract's
W4-after-W3 sequencing; it would not silently change the W1-W4 package graph. No
host-agent, provider or local CLI behavior enters these packages.

## Proposed usable Spec Kit source interface

**O1 DECIDED — owner, 2026-10-27, revised:** derive the source from an existing owner
workspace's `requested_config.repository_identity` and immutable
`requested_config.commit`. The request selects only `workspace_id`; it cannot supply a
path, URL, repository identity or commit. This replaces the earlier source
interpretation. There is no new approval, configuration or permission point, and this
decision does not accept the sequencing/reuse exception.

Proposed interface: `SpecKit.project(principal, workspace_id)` authorizes the new
`artifact_projection_read` action, then loads the existing workspace under the
principal's owner before reading `Foundation.Workspace.requested_config`. Permit
dashboard, CLI-owner and owner-browser credentials for this artifact read; the dashboard
page admits only its dashboard credential. This is the narrow proposed exception to the
frontend contract's dashboard-denied-for-every-new-read rule under FR-033;
enrollment/trust new reads remain owner-browser/CLI-owner only, dashboard denied.
Foreign ownership is refused before any source-field, Git or artifact read. The planned
request decoder accepts exactly `workspace_id`; extra fields return `invalid_request`,
including source_id, feature and artifact selector overrides. Relative artifact IDs are
output only. A foreign workspace UUID and absent UUID both return `not_found` without
exposing ownership or calling the source/Git adapter.

Reuse existing revision approval records and ConfigTrust from the partial T009 subset
below; add no source registry, repository descriptor, approval/configuration flow,
storage or host command. Workspace has no approval-ID foreign key;
`ConfigTrust.lookup/3` requires an approval ID plus descriptor and is not already a
workspace resolver. Propose a read helper matching existing approval owner and
normalized immutable descriptor under existing trust semantics, not a new approval or
permission point. This action reuses existing credentials, not a new owner grant.
Foundation persistence alone does not prove approval: no existing matching approval
means unavailable/`trust_required`, never auto-approval or a new approval prompt. This
resolver behavior is planned, not already implemented. The recorded repository identity
is not a local checkout path or locator. This proposal invents no local checkout
locator: absent locally available objects for that recorded source and exact commit mean
unavailable, without a different source, revision or development-checkout fallback. A
future GitHub App grant may supply repository access; that integration is outside this
slice.

Read blobs from the commit, not the worktree/index or a moving branch. Propose roots
`specs/`, `docs/quality/` and `docs/exec-plans/`; recognize feature directories
containing `spec.md`, `plan.md` and `tasks.md`, and retained Markdown evidence. Project
one admitted feature/evidence subset within that workspace source and commit, not all
files; requests cannot replace the repository or revision. Reject absolute/traversing
paths and symlink/submodule entries. Use plumbing-only fixed argv in a private ephemeral
Git context isolated from source config and refs; use a helper Git directory with fixed
minimal config and the admitted object format, no source config/refs/worktree. Expose
the source object database read-only through fixed `GIT_OBJECT_DIRECTORY`, never
alternates. This temporary helper state is discarded after the read, not persistent
projection storage. Scrub inherited `GIT_*` variables, then set `GIT_CONFIG_NOSYSTEM=1`,
`GIT_CONFIG_GLOBAL=/dev/null`, `GIT_NO_REPLACE_OBJECTS=1` and `GIT_NO_LAZY_FETCH=1`;
pass `--no-replace-objects`, `--no-lazy-fetch` and `-c protocol.allow=never`.
Source-local config is read only as data for admission, never executed or inherited.
Reject grafts, file/environment/ HTTP alternates, partial-clone/promisor declarations
and promisor markers; do not follow replace refs. Only known SHA-1/SHA-256 source
formats read as data are admitted. Admit canonical lowercase full OIDs matching the
repository object format: 40 hex for SHA-1 or 64 for SHA-256. Ref names, revision
expressions and tags are refused; verify exact OID equality and object type commit
before projection, without peeling. Blob OIDs must match the same format. No
shell/network, hooks, filters, text conversion, checkout or fetch. Missing Git
capability is unavailable, never a fallback to unsafe or network-enabled reads.

Proposed engineering bounds: one-second total projection deadline, at most 64 artifact
blobs, 256 KiB per blob and 8 MiB total blob bytes; enumeration at most 1,000 entries
and 1 MiB of path data. Enforce limits while reading. Timeout, missing objects,
unsupported layout/encoding or exceeded limits give explicit unavailable/incomplete
results, never an empty-success or partial-completion claim. These are proposed values
for this read path, not inherited owner decisions.

Return workspace/source identity, commit OID, relative path, blob OID and byte SHA-256,
parser capability, observation time, availability and escaped plain-text Markdown
source. Show specs/plans/tasks and repository-backed review evidence on a usable
dashboard; missing workspace/agent evidence is unavailable. A checkbox is not approval
and a repository evidence file is not an authenticated forge review. Apply FR-022
visible control escaping followed by HTML escaping. This slice performs no
Markdown-to-HTML conversion and emits no artifact-derived links or images; richer safe
rendering is not delivered here. Git stderr, argv, exceptions and absolute host/helper
paths never reach pages or logs; report safe reasons and escaped relative artifact IDs.
Retain no persistent projection storage.

The interface and bounds remain proposed for review. The workspace-derived source is the
revised O1 decision above. No source editing, approval or repository/commit selector
enters the dashboard.

## Enrollment and trust: existing source, read-only projection

Use existing retained enrollment, host identity/security and revision approval records
through the shared authorized read service. Trust also uses partial T009 ConfigTrust and
`revision_trust_approvals`, introduced at `ba761b7c1a811a7d367d5b5d28e4ca2541746e38`.
Propose an explicit reuse exception limited to those existing components; full T009
remains open. Add owner-scoped summary helpers in Hosts and ConfigTrust, with Accounts
authorization and safe audit action names. The accepted frontend contract proposes
dedicated `enrollment_status_read` and `revision_approval_read` actions for
owner-browser and CLI-owner access, denying dashboard credentials. Preserve that
separation.

Summaries may describe retained IDs, timestamps, enrollment/identity state and approval
identity. Exclude enrollment codes, secret hashes, CSRs, certificates and raw
configuration descriptors. No new storage is required. A retained approval record does
not prove trust in current workspace bytes. No read-page handler may issue, approve,
revoke, provision or dispatch, including through forged events.

## Needs owner decision: F8 Git object acquisition

**F8 OPEN — 2026-10-27:** O1 decides source identity and workspace ownership;
it does not decide how the control plane obtains the corresponding Git objects.
The workspace record stores repository identity and commit, not a local clone.
No existing host-agent command provides this artifact read. The choices awaiting
an owner answer are:

| Option | Scope and consequence |
| --- | --- |
| A | Report unavailable when the objects do not already exist locally; identify the existing server-side object location before claiming a usable projection. |
| B | Read through the host agent using a new read-only protocol command; this requires separate protocol scope. |
| C | Maintain a control-plane read-only mirror; acquisition and refresh need an explicit contract. |

No option is selected or authorized by this proposal. The no-fetch rules and
missing-object/unavailable text above describe the proposed local reader; they
do not settle object acquisition or prove that any local object source exists.
Do not implement or revise the acquisition path before the owner answers F8.
O1 remains decided; F8 introduces no new per-source approval or configuration.

## Proposed approval and activation

| Item | Current state |
| --- | --- |
| Isolated execution exceptions and sequence | PROPOSED; owner acceptance pending |
| F8: Git object acquisition | OPEN; owner answer pending, no A/B/C selection |
| Workspace-derived projection interface and bounds | PROPOSED engineering interface above |
| Inert Markdown and unavailable status scope | PROPOSED; owner acceptance pending |
| O1: existing workspace repository identity and immutable commit | DECIDED by owner, revised 2026-10-27; request selects workspace_id only |

This proposal includes matching exception pointers in [spec](../spec.md),
[plan](../plan.md), [tasks](../tasks.md) and the frontend contract. Independent
exact-head review, explicit owner acceptance of this PR's exact sequencing/reuse
exception and merge would activate only these isolated read-only exceptions. O1 is
decided, but it does not supply that sequencing acceptance or approve product
implementation. Until then current task and product-read approval gates apply. Later
HTTP routes retain the [shared owner API](owner-api.md).

Implementation needs a dashboard-specific mount; owner-only OwnerAuth cannot admit
dashboard credentials. Reusing DashboardSocket requires tested ingress and no-write
behavior; do not assume OwnerSocket's filter also protects it. The following named
SpecKit tests are planned RED-first; none exists or was executed for this docs-only
proposal:

| Planned test | Required case and causal assertion |
| --- | --- |
| `workspace_only_request` | Only a valid owner workspace_id is admitted; stored repository_identity/commit remain exact inputs. |
| `request_overrides_refused` | Request, query and event fields cannot name a path, URL, repository or commit. Each extra path, url, repository, repository_identity, commit, owner, owner_id, source_id, feature or artifact key gives invalid_request before source/Git reads. |
| `foreign_workspace_not_found` | Foreign and absent UUID both give not_found before source-field/Git reads, even with identical source fields; assert the authorization-denial audit without recording source fields. |
| `stored_source_and_approval` | Own stored requested_config selects exact immutable descriptor; planned helper matches existing owner/normalized approval without an invented FK; unmatched approval is unavailable/trust_required with no prompt or auto-approval. |
| `missing_objects_unavailable` | Absent local objects are unavailable, with no configuration prompt, network or alternate source/revision. |
| `invalid_request_shape` | Duplicate keys and malformed workspace IDs refuse before lookup; no owner/source request override. |

The override test must fail when the closed request-key check is relaxed. The
foreign-workspace test must fail when the owner predicate is removed: use two
actually authenticated owners and their real workspace rows, not forged
principals. Foreign and absent workspaces return identical `not_found` with zero
Git/source reads. These are causal expectations for future tests, not results.

Also plan direct calls to actual implemented mutation routes with dashboard credentials
and denial audit; do not claim absent future routes were tested. Cover SC-009
cross-origin read, form post, DNS rebinding and streaming upgrade; hostile Host and
inert attributes; and dashboard forged events with no mutation, crash or payload
logging. Pin Git isolation, replace/graft, lazy-fetch, alternates and exact-OID refusals
with failing-first tests. Retain existing transport policy. Native CI and exact-head
independent review remain PR gates. Browser/owner-host journeys, full-task ticks,
lifecycle delivery and pilot acceptance remain separate.
