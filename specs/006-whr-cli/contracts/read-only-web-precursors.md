# Read-only web precursors: approval proposal

Status: **PROPOSED**, 2026-10-27. This document authorizes no product code, runtime
activation, task completion or change to existing prerequisites. The sequencing and
implementation proposal remains unapproved. O1 and F8 are decided by the owner as
recorded below; the protocol command is not implemented.

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
| Spec Kit read-only | T006 authentication, existing T005 persistence and partial T009 trust persistence; usable artifacts also require the T007/T019 host read below | T019 agent interoperation, full T020 acceptance and downstream work |
| Enrollment/trust read-only | Existing T005/T008 records, partial T009 ConfigTrust records and owner shell; reuse exception below | T011 client, full T012 lifecycle/retry/progress journeys and downstream work |

Each package would be independently reviewed and tested against its own scope. An
accepted precursor would not mean its parent task or prerequisite is complete. The
enrollment/trust exception would explicitly reconcile the frontend contract's
W4-after-W3 sequencing; it would not silently change the W1-W4 package graph. No
provider or local CLI behavior enters these packages. This docs-only proposal names
the separate T007/T019 host-protocol dependency; it implements no host-agent behavior.

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
storage. The read-only host command required by F8 is a separate dependency below.
Workspace has no approval-ID foreign key;
`ConfigTrust.lookup/3` requires an approval ID plus descriptor and is not already a
workspace resolver. Propose a read helper matching existing approval owner and
normalized immutable descriptor under existing trust semantics, not a new approval or
permission point. This action reuses existing credentials, not a new owner grant.
Foundation persistence alone does not prove approval: no existing matching approval
means unavailable/`trust_required`, never auto-approval or a new approval prompt. This
resolver behavior is planned, not already implemented. The repository identity is not a
control-plane checkout locator. F8 selects the existing workspace clone on its assigned
host, resolved by the host agent from trusted workspace state, never from request paths.
The control plane holds no clone or mirror. Until the host command below exists and is
supported by the authenticated connection, artifacts are unavailable. Missing objects on
the host are also unavailable; never fetch, change revision or use another source.
A future GitHub App grant may supply repository access; that integration is outside this
slice.

Read blobs from the commit, not the worktree/index or a moving branch. Propose roots
`specs/`, `docs/quality/` and `docs/exec-plans/`; recognize feature directories
containing `spec.md`, `plan.md` and `tasks.md`, and retained Markdown evidence. Project
one admitted feature/evidence subset within that workspace source and commit, not all
files; requests cannot replace the repository or revision. Reject absolute/traversing
paths and symlink/submodule entries. The host agent reads its existing workspace
clone with plumbing-only fixed argv in a private ephemeral Git context isolated
from source config and refs; use a helper Git directory with fixed
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
shell, Git network access, hooks, filters, text conversion, checkout or fetch.
The existing authenticated host connection carries the bounded artifact response;
that transport is not permission for Git network access. Missing Git
capability is unavailable, never a fallback to unsafe or network-enabled reads.

Proposed host-read engineering bounds: one-second total projection deadline, at most 64 artifact
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

## F8 decision and host-protocol dependency

**F8 DECIDED — owner, 2026-10-27: option B.** Git objects come from the workspace
clone held by the host agent, through a new read-only, bounded host-protocol command
on the existing authenticated host connection and revision trust. No new approval,
configuration, control-plane clone/mirror or fetching is introduced.

The proposed command name is `spec_kit.read`. Its implementation is an explicit
T007 host-protocol extension and T019 artifact-read integration dependency, not
existing T007 capability or completion of either full task. The current closed v1
lifecycle command/action grammar does not admit it. T007 must add reviewed versioned
request/result forms, shared fixtures, both validators and negotiated capability
before dispatch; never disguise the read as a lifecycle command or silently widen v1.
AgentSocket currently accepts only hello/heartbeat messages; read-command dispatch
and correlated chunk assembly are also new dependency work. Preserve those existing
messages and their ingress limits.
Until that command and integration exist, T020 renders artifacts unavailable.

The following engineering contract is proposed for that dependency; the owner's
choice of B does not claim these numeric bounds were separately approved:

| Surface | Required bound or scope |
| --- | --- |
| Operation | Only `spec_kit.read`; no write verb, generic exec, Git argv, fetch, checkout, lifecycle operation or file-write handler. |
| Authorization | Control plane first authorizes the principal and workspace owner; select the workspace's assigned host from stored state. The authenticated host peer, owner_id, host_id and workspace_id must agree on both ends, including response correlation. Recheck assignment and existing matching revision approval before reading; mismatches refuse without a Git read. No browser-supplied scope, repository, commit or path override. |
| Source | Control plane derives the exact repository identity/commit and matching retained approval from that workspace; the host verifies that binding against its existing workspace clone and current trusted assignment. Only immutable commit blobs are read; a missing clone/object/approval or stale assignment is unavailable, never a fetch or fallback. |
| Paths | Fixed roots specs/, docs/quality/ and docs/exec-plans/ only; no caller-selected paths. At most 1,000 enumerated entries, 1 MiB aggregate UTF-8 path data and 1,024 UTF-8 bytes per relative path. Reject absolute, traversing, symlink and submodule entries. |
| Content | At most 64 blobs, 256 KiB decoded bytes per blob and 8 MiB decoded bytes total. Enforce while reading, not after allocation. |
| Transport | Every request/result JSON message is at most 4,096 encoded bytes, preserving the existing 4,110-byte frame and 4,096-byte fragmented-message caps. Base64 content chunks carry at most 1,024 decoded bytes, at most 256 chunks per blob and 16,384 content chunks total. Inventory has at most one message per entry (1,000); allow one begin and one completion, at most 17,386 response messages total and 16 MiB aggregate encoded bytes. Count content/path/encoded bytes independently; an entry or header that cannot fit a message is unavailable/incomplete, never truncated. The pure lifecycle validator's separate 1 MiB ceiling is not a transport capability. |
| Time | One-second control-plane deadline from dispatch through final response validation, including host I/O; cancellation/disconnect stops the read. Expired or late responses cannot update the projection; no automatic retry in that budget. |
| Result | Bind source, workspace, immutable commit and blob digests to the correlated request; verify count/size/path/OID bounds at both ends. Only an explicit complete response is successful. Missing capability, timeout, connection loss, limit or missing objects produces unavailable/incomplete, never empty success. No raw Git stderr, paths or content in logs. |

Host Git isolation and FR-022 rendering rules above still apply; only artifact bytes
and safe provenance cross the existing connection. The control plane projects the
result without running Git or retaining a clone. This document defines the dependency,
not its wire-schema implementation, deployment or completion evidence.

## T041: spec_kit.read host command contract

Status: **PROPOSED** work item [T041](../tasks.md); it ticks nothing and changes no
prerequisite of T007, T019 or T020. It restates the F8 table above as a testable work
item and adds the message shapes, result forms, errors and tests. Message and reason
names below are proposals for the reviewed T041 fixtures, not existing wire forms.

### Existing code it builds on

| Fact | Evidence |
| --- | --- |
| The page already calls `SpecKit.project(principal, workspace_id)`; the dashboard renders every `{:ok, result}` as unavailable. | `apps/control_plane/lib/workharbor/spec_kit.ex:29`, `apps/control_plane/lib/workharbor_web/live/spec_kit_live.ex:36-37` |
| The request decoder admits exactly one key, `workspace_id`, as a lowercase UUID, in at most 256 bytes. | `spec_kit.ex:10`, `spec_kit.ex:17-19` |
| The owner predicate runs before any source field is selected; absent and foreign rows take the same audited `not_found`. | `spec_kit.ex:66-76`, `spec_kit.ex:115-118` |
| The read has a one-second deadline that includes authorization; late results are discarded. | `spec_kit.ex:31`, `spec_kit.ex:49-57` |
| Exact-revision trust is matched from the workspace's own `requested_config` by owner and descriptor digest; no match is `trust_required`. | `apps/control_plane/lib/workharbor/config_trust.ex:85-98`, `spec_kit.ex:87-108` |
| Today the success result is always `availability: :unavailable`, `reason: :host_read_unavailable`. | `spec_kit.ex:90-92` |
| A workspace row carries `owner_id`, `host_id` and `requested_config`. | `apps/control_plane/lib/workharbor/foundation/records.ex:35-40` |
| The host connection admits only hello, hello_ack, heartbeat and heartbeat_ack; hello capabilities are `[]` or `["docker"]`; messages are at most 4,096 bytes. | `apps/control_plane/lib/workharbor/host_connections/protocol.ex:8-26`, `apps/host_agent/internal/connection/protocol.go:13-42` |
| `AgentSocket` accepts one text frame at a time and stops the connection on any other message; the ingress frame caps are 4,110 and 4,096 bytes. | `apps/control_plane/lib/workharbor_web/agent_socket.ex:21-36`, `apps/control_plane/lib/workharbor_web/agent_ingress.ex:130-131` |
| The v1 lifecycle command has the closed actions create, start, stop, rebuild and remove_runtime. | `contracts/host-protocol/README.md` ("Exact command"), `apps/host_agent/internal/protocol/protocol.go:207-221` |

### Request

Control plane to host, one message after the connection's negotiated capability
includes the read capability (token name to be fixed in the T041 fixtures):

| Field | Rule |
| --- | --- |
| `schema_version`, `kind` | Integer 1; one new closed kind for the request. Separate from the lifecycle `command` kind. |
| `request_id` | Fresh lowercase UUID, echoed by every response message; the correlation key. |
| `owner_id`, `host_id`, `workspace_id` | Lowercase UUIDs from stored state, never from the browser. They must equal the authenticated peer on both ends. |
| `repository_identity`, `commit` | Copied from the owner's stored `requested_config`; `commit` is a canonical lowercase full OID. |
| `approval_id` | The matching retained approval's ID from `ConfigTrust.artifact_approval/2`. |

The closed request has no path, URL, ref, argv, root list, limit or feature field; the
roots and bounds are fixed by the F8 table. The browser-facing decoder stays
`SpecKit.decode_request/1`, which already refuses every other key.

### Response

One begin message, one inventory message per admitted entry (relative path, blob OID,
size, byte SHA-256), base64 content chunks (at most 1,024 decoded bytes each), and one
completion message that states entry, blob and byte counts. Every message repeats
`request_id`, `owner_id`, `host_id` and `workspace_id`, stays within the 4,096-byte
message cap and is validated by both the Elixir and Go validators from shared fixtures.
Counts, sizes, path lengths and totals are the F8 "Paths", "Content" and "Transport"
rows; they are enforced while reading and again on receipt. Only a completion whose
counts and digests match the received entries is success.

Control-plane result of `SpecKit.project/2`: success carries workspace and source
identity, commit OID, per-entry relative path, blob OID, byte SHA-256 and the
escaped plain-text source, plus `observed_at`. Any other outcome stays
`{:ok, %{availability: :unavailable, reason: reason}}` and never an empty success.

### Error cases

| Case | Result | Reads |
| --- | --- | --- |
| Extra, duplicate or malformed request key | `{:error, :invalid_request}` | None |
| Credential kind or action not admitted | `{:error, :forbidden}` | None |
| Foreign or absent workspace | `{:error, :not_found}` with the existing denial audit | No source field, no host message |
| No retained matching approval | unavailable, `trust_required` (existing) | No host message |
| Host not connected, capability not negotiated, assignment changed, approval changed after dispatch | unavailable, safe reason | No Git read on mismatch |
| Clone, commit or object missing; unsupported layout; malicious Git config or alternates | unavailable, safe reason from the host | No fetch, no fallback |
| Any bound exceeded, malformed, late, uncorrelated or duplicated response, disconnect | unavailable or incomplete, never partial success | Stops the read |

Reason atoms are a closed set fixed in the T041 fixtures; Git stderr, argv, absolute
paths and content never appear in reasons or logs.

### Red-first tests

Control plane (`apps/control_plane/test/workharbor/spec_kit_test.exs` and a new host
protocol test): `request_overrides_refused` and `workspace_only_request` (both exist
for the browser decoder at `spec_kit_test.exs:20`; extended to the host request),
`foreign_workspace_not_found` with two authenticated owners (exists at
`spec_kit_test.exs:70`; extended to assert zero host messages), `wrong_peer_refused`,
`assignment_changed_refused`, `approval_changed_refused`, `capability_not_negotiated`,
`late_response_discarded`, `uncorrelated_response_refused`,
`incomplete_response_is_unavailable`, `bounds_at_n_and_n_plus_one` for entries, path
bytes, blobs, blob bytes, total bytes, chunks and messages, and
`dashboard_shows_unavailable_without_capability`.

Host agent (`apps/host_agent/internal/protocol/`, `apps/host_agent/internal/connection/`):
`unknown_command_refused`, `forbidden_path_or_argv_field_refused`,
`missing_clone_unavailable`, `missing_object_unavailable`, `malicious_git_config_ignored`,
`replace_graft_alternates_refused`, `no_network_no_fetch`, `symlink_submodule_refused`,
`exact_oid_not_peeled`, `existing_hello_heartbeat_unchanged`. Each fails before its
guard is added and its mutation probe (remove the owner predicate, relax the closed key
check, raise a bound by one) turns it red.

### Out of scope

Rendering available artifacts and the Markdown view (T020), agent-session transport
(T019), enrollment and trust reads (T012), any write, lifecycle, fetch or checkout
operation, a control-plane clone or mirror, persistent projection storage, a new
approval or permission point, browser or owner-host acceptance, deployment, and any
change to the closed v1 lifecycle command grammar.

### Dependencies

T008 (authenticated connection, `AgentSocket`, heartbeat ingress limits) and, through
it, T007 (closed v1 protocol and validators). It reuses the partial T009 approval
records and `ConfigTrust.artifact_approval/2`; it needs no other T009 behavior. T020
keeps rendering artifacts unavailable until T041 is delivered and supported by the
authenticated connection; whether T020's prerequisites list T041 is an owner decision
not made here.

## Proposed approval and activation

| Item | Current state |
| --- | --- |
| Isolated execution exceptions and sequence | PROPOSED; owner acceptance pending |
| F8: host-agent object reads | DECIDED by owner, 2026-10-27, option B; spec_kit.read remains an unimplemented T007/T019 dependency |
| Workspace-derived projection interface and bounds | PROPOSED engineering interface above |
| Inert Markdown and unavailable status scope | PROPOSED; owner acceptance pending |
| O1: existing workspace repository identity and immutable commit | DECIDED by owner, revised 2026-10-27; request selects workspace_id only |

This proposal includes matching exception pointers in [spec](../spec.md),
[plan](../plan.md), [tasks](../tasks.md) and the frontend contract. Independent
exact-head review, explicit owner acceptance of this PR's exact sequencing/reuse
exception and merge would activate only these isolated read-only exceptions. O1 and
F8 are decided, but neither supplies sequencing acceptance or approves product
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
| `missing_objects_unavailable` | Absent host capability, clone or objects is unavailable; no control-plane Git, configuration prompt, fetch or alternate source/revision. |
| `invalid_request_shape` | Duplicate keys and malformed workspace IDs refuse before lookup; no owner/source request override. |

The override test must fail when the closed request-key check is relaxed. The
foreign-workspace test must fail when the owner predicate is removed: use two
actually authenticated owners and their real workspace rows, not forged
principals. Foreign and absent workspaces return identical `not_found` with zero
Git/source reads. These are causal expectations for future tests, not results.

Also require RED-first host-protocol cases for wrong peer/owner/workspace/assignment,
changed commit/approval, unsupported command, forbidden path/write/argv fields,
count/size/path/time bounds at N and N+1, disconnect, late or mismatched responses,
missing clone/objects and malicious Git config. Assert refusal reasons, zero workspace/repository writes
and no fetch; missing capability must show unavailable in the dashboard. These tests
belong to the T007/T019 dependency and are not implemented or executed here.

Also plan direct calls to actual implemented mutation routes with dashboard credentials
and denial audit; do not claim absent future routes were tested. Cover SC-009
cross-origin read, form post, DNS rebinding and streaming upgrade; hostile Host and
inert attributes; and dashboard forged events with no mutation, crash or payload
logging. Pin Git isolation, replace/graft, lazy-fetch, alternates and exact-OID refusals
with failing-first tests. Retain existing transport policy. Native CI and exact-head
independent review remain PR gates. Browser/owner-host journeys, full-task ticks,
lifecycle delivery and pilot acceptance remain separate.
