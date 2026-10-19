---
title: Guarded forge MCP
description: "Run-scoped forge operations through host policy and provider adapters."
weight: 8
toc: true
---

## 5.11 Guarded forge MCP (D55)

{{< status decided >}} Werner approved this boundary in
[crewbook #20](https://github.com/wstein/crewbook/issues/20), which tracks
implementation in WorkHarbor. MCP exposes a bounded set of forge operations
through invocation-scoped policy checks, the forge Guard and provider API
adapters. GitHub is tier 1; GitLab and Codeberg are tier 2. These tiers set
implementation order and grant no policy exemption. D15's release 1 forge
scope and D53's full native Codex priority remain unchanged, apart from the
human's explicit Go compatibility and deduplication tasks.

### Boundary and authority

The request path is **MCP → invocation-scoped policy guard → forge API
adapter**. MCP handlers decode bounded typed arguments and call the service
layer. The host derives the run, task, workspace, repository and configured
provider instance from an authenticated run binding. A caller-supplied run ID,
repository name, URL, role or approval receipt grants no authority.

On every invocation, before a provider call, the host checks that the binding
is live, that the repository and provider instance match, that the operation
and arguments are permitted by current policy, and that the required approval
still applies. Tool discovery or an earlier allowed invocation does not
authorize a later call. Unknown operations, invalid arguments, missing
capabilities and unavailable authorization fail closed. Ending or revoking a
run invalidates its binding; a resumed run requires revalidation before use.

Every invocation derives its effective `policy.Context` from authoritative
host state: input provenance, access to private data and effective egress.
The host passes that context into the run-scoped `Guard.For` and its
`Table.DecideIn` decision; invoking a shared Guard with its default zero
context is not authorized. Service policy checks for reads use the same
effective context. Newly consumed untrusted issue, comment, CI or session
message text remains represented in subsequent invocation decisions. Unknown
provenance is untrusted; unknown private-data access is treated as private,
and unavailable effective context fails closed. Caller-provided context or
trust claims cannot clear these restrictions. Context composition preserves
the stricter workflow/policy result and existing approval requirements.

The existing forge Guard remains the enforcement point for forge mutations.
The service's invocation checks also cover reads; the current adapter's
repository configuration check alone does not establish run authorization.
Permitted writes use existing Decision records and operation-specific approval
rules. SHA-bound writes must still match their approved SHA immediately before
the adapter call. MCP cannot mint approvals, widen policy or invoke the
supervisor's privileged management API. Agent merge, tag, release and deploy
remain forbidden under every provider and workflow.

Provider credentials stay in host-owned credential handling and never reach
the agent environment, MCP arguments, responses or logs. A transport binding
is scoped authority, not a provider credential. Redirect handling and provider
instance selection must not send credentials to a caller-selected destination.
There is no arbitrary HTTP request, GraphQL document, upstream MCP passthrough
or shell command operation.

### Bounded operation matrix

The following is the intended interface, not a claim of current adapter or
native-client support. Exact schemas and limits precede implementation.

| Operation | Authorization and bounds | Provider requirement |
| --- | --- | --- |
| Read an issue or search issues | Bound repository; validated identifiers or bounded query, page size and result count | Explicit issue-read/search capabilities; unsupported searches return unavailable |
| Inspect a pull request | Bound repository and validated PR identifier; bounded metadata and diff response | Explicit PR-read capability; forge content remains untrusted |
| Read CI status | Bound repository and exact revision or PR binding; bounded checks and links | Explicit CI-status capability; no implicit log/artifact download or retry |
| Comment on an issue | Bound repository and issue; bounded text; host-derived trust context through Guard.For/DecideIn; existing comment policy and any required approval | Guarded issue-comment capability; an untrusted invocation cannot use the default auto-comment path |
| Open or update a PR | Reserved and unavailable in MCP v1: invocation bindings require a live run, while publication requires a stopped/prepared run | Existing host Decision/reconciler publication path only; no direct transport bypass |
| Merge, tag, release or deploy | Always denied for agents | No callable operation or passthrough |

Provider-independent request limits cover argument and response bytes,
pagination, concurrency and deadlines. Rate-limit responses preserve the
adapter's typed retry information; retries must repeat authorization checks.
Audit records identify run, repository, provider instance, operation, outcome
and applicable Decision/SHA without raw credentials or unrestricted request
payloads. Returned forge text and error details retain existing redaction and
untrusted-output handling.

### Invocation contract v1 (#291)

{{< status decided >}} The following provider-neutral data contract freezes
D55's bounded operations before GitHub implementation. It specifies service
inputs and outputs, independent of MCP transport framing or native discovery.
It adds no provider credential scope or operation authority. Implementation,
provider support and native-client compatibility remain
{{< status unverified >}}.

A tool takes one typed JSON object. Reject duplicate or unknown fields, invalid
UTF-8, malformed integers, unknown enum values and trailing JSON before a
provider call. Identifiers are positive signed 32-bit issue/PR numbers; revision
identifiers are full lowercase 40- or 64-hex object IDs, never branch expressions.
Text bounds count UTF-8 bytes. Optional fields are absent rather than null;
zero/empty values never select a default repository, broader scope or permission.
No operation accepts a run, task, repository, provider URL, trust context,
credential, approval receipt or arbitrary provider request. The host derives
those from the authenticated invocation binding.

| Tool | Argument fields | Typed result and capability |
| --- | --- | --- |
| `issue_get` | `number` | Issue number, title, body, state (`open`/`closed`), author and validated provider link; `issue_read` |
| `issue_search` | Optional `text` (1–256 bytes), `state` (`open`/`closed`/`all`), `limit` (1–50), `cursor`; absent text means list issues | Issue summaries (number, title, state, author, link), next cursor if present; `issue_search` |
| `pr_get` | `number`; optional `include_diff` boolean, default false | PR number, title, body, state (`open`/`closed`/`merged`), head/base revisions, branch names and link; optional bounded unified diff; `pr_read`, and `pr_diff` when requested |
| `ci_status` | Exactly one of `revision` or `pr_number` | Exact inspected revision and bounded checks: name, state (`pending`/`success`/`failure`/`cancelled`/`unknown`), provider link; `ci_status` |
| `issue_comment` | `number`, `body` (1–32 KiB) | Target number and confirmed comment identifier/link when available; `issue_comment` through Guard |
| `pr_publish` (reserved) | No accepted invocation arguments in v1 | Always `unavailable` with reason `unsupported`; capability `pr_publish` is always false |

Issue search treats text as literal search terms in the bound repository: the
adapter escapes provider syntax and always fixes repository and issue type.
It accepts no raw provider query, URL, sort expression or qualifier language.
PR diffs contain text only, never attachments, fetched paths or executable
rendering. CI status contains no logs, artifacts, retries, downloads or commands.
A PR-derived CI read resolves the exact head revision after authorization and
reports that revision, so a moving PR cannot imply a result for another commit.
Forge text, branch names and links remain labelled untrusted output. Links must
use the configured provider's approved origins and contain no credentials;
returning a link does not authorize fetching it.

`pr_publish` is a reserved name, not a callable publication capability in v1.
Every MCP invocation requires a live run binding, revoked when that run ends;
`Publisher.Prepare` and the task's ready-for-review transition instead require
the latest run to be stopped. Those conditions cannot both authorize an MCP
publication. Report this operation unavailable and reach no provider mutation;
do not retain an ended run's binding, create a second publication binding,
prepare a live run or interpret a candidate reference as authority.

The existing human Decision/reconciler publication path remains outside MCP.
It resolves the prepared candidate, task/repository/branch, workflow target,
Decision and exact approved SHA from authoritative state and uses Publisher
and Guard with the required context and immediate approval/SHA rechecks.
D55's intended permitted-write boundary is preserved without adding a task
transition, approval minting or live-run publication. Any future callable MCP
publication needs a separately reviewed lifecycle contract; this specification
supplies none.

Capabilities are a versioned host-produced record: `contract_version: 1`,
configured provider-instance identity, and an entry for each capability above
with `available` boolean and an optional unavailable reason (`unsupported`,
`credential_scope`, `configuration`, `not_measured`). Unknown capabilities are
unavailable. `pr_publish` must advertise `available: false` with reason
`unsupported` under this lifecycle, even if an adapter can open/update PRs.
The adapter must implement every other declared available capability, the configured
credential scope must permit it, and service policy must authorize each call;
an advertised capability grants none of those permissions. Missing GitHub
search, PR-read/diff or CI methods must be added explicitly rather than inferred
from `forge.Adapter`. D15's existing App permissions are unchanged: an operation
requiring another permission is unavailable until separately approved. Tier-2
providers implement the same schema with their measured subset; no fallback to
shell, generic HTTP or an upstream MCP server is permitted.

Limits are hard ceilings; operator configuration may lower them. An invocation
has at most 64 KiB of argument JSON and 1 MiB of encoded response, including
errors, at most four active invocations per run, and a 30-second total deadline
including provider calls. Reject excess concurrency instead of accumulating an
unbounded queue. Issue search has at most 50 items per page, two pages and 100
items per cursor chain; CI status has at most 100 checks and no hidden pagination.
All scalar response text and diff bytes share the response ceiling. An oversized
single object/diff fails `limit_exceeded` rather than returning a falsely complete
object. List continuation is explicit; never silently omit provider pages or
represent omitted checks as success. Adapters bound upstream reads and decoded
objects before allocation, not only final serialization.

Cursors are opaque, at most 256 bytes, host-issued and authenticated or stored
host-side. Bind them to the run generation, repository, provider instance,
operation, canonical filters and remaining page/item budget. Reject tampered,
expired, revoked or mismatched cursors before provider access. They contain no
credential and cannot designate an arbitrary upstream URL. Ending/revoking a
binding cancels pending invocations and cursors; resume creates a revalidated
binding generation. Recheck lifecycle, current policy/context and approval
immediately before each provider request or retry, and discard a late response
after revocation. Reads update authoritative input provenance before returning
untrusted content, so later calls cannot race ahead with an obsolete trusted
context. Context updates and mutation authorization must be serialized or
version-checked at their service boundary; unknown context fails closed.

Results carry `contract_version`, host-issued invocation identifier and exactly
one of operation data or a typed error. Errors use `invalid_arguments`,
`denied`, `revoked`, `unavailable`, `not_found`, `stale_approval`, `limit_exceeded`,
`rate_limited`, `timeout` or `provider_failure`, with a bounded redacted message
(maximum 1 KiB). An unavailable error uses the capability reason above.
Rate-limited/transient provider failures preserve typed retry eligibility and
`retry_at` when known; absent retry time remains unknown. No automatic mutation
retry occurs: re-entry repeats authorization and checks the existing publication
record before issuing another write. Raw HTTP responses, headers, token material
and provider exception strings never become MCP errors.

Audit before provider dispatch and after outcome, including refusals: invocation
identifier, run/task, repository, configured provider identity, operation,
capability, binding/context generation, outcome/error code, applicable Decision
and exact SHA. Redact at ingest. Do not retain argument bodies, search text,
diffs, credentials, cursor payloads or raw provider errors as audit metadata.
An unauthenticated request records only a bounded refusal with no invented run.
Failures after a write report an unknown outcome when confirmation is unavailable;
they must not claim that nothing happened or blindly repeat the mutation.

Focused contracts must exercise every bound, unknown field/enum, malformed
cursor and capability refusal without provider access; cross-run/repository
cursor swaps; reserved publication remaining unavailable without a provider
call; current-context changes between reads and writes;
revocation during a pending read; and stale/revoked Decisions immediately before
publication in the existing host path. Provider fakes establish bounded service behavior only. Exact-SHA
independent security review precedes support claims; measured native transport,
GitHub credential/API behavior and tier-2 support remain separate gates.

### Transport and provider gates

Transport selection is separate from this architecture. Runtime exec/stdio is
the first candidate; actual client loading, channel isolation and revocation
remain {{< status unverified >}} until measured on the pinned target. This
decision neither exposes D29's privileged unix socket nor permits a runtime
socket mount, an additional host mount or an egress exception. A mounted socket
requires its own measured and reviewed seam before adoption.

GitHub implementation comes first using the existing service and forge
contracts, with explicit capabilities added where needed. GitLab and Codeberg
follow separately after the common invocation and capability contracts are
reviewed. Each must document its supported operation subset, credential scope,
instance validation, pagination and rate-limit behavior. Codeberg's deployed
Forgejo API must be checked rather than inferred from another forge. Their
actual API compatibility remains {{< status unverified >}}.

Tests must prove that forbidden actions, cross-repository/provider calls,
revoked bindings, invalid arguments, missing capabilities and stale approvals
reach no provider mutation. Provider fakes and focused contract tests establish
only their tested scope. Acceptance tests must show that newly consumed
untrusted content restricts later invocations, that an untrusted invocation
cannot use the shared Guard's default auto-comment path, and that caller trust
or context claims cannot weaken `DecideIn`, workflow policy or approval rules.
Unknown provenance and unavailable context must not become trusted defaults.
Live target evidence and independent security review
of the exact implementation revision are required before claiming support.
This design implements no MCP endpoint, credential flow or provider adapter.
