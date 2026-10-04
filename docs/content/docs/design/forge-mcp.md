---
title: Guarded forge MCP
description: "Run-scoped forge operations through host policy and provider adapters."
weight: 8
toc: true
---

## 5.11 Guarded forge MCP (D55)

{{< status decided >}} Werner approved this boundary in
[crewbook #20](https://github.com/wstein/crewbook/issues/20), which tracks
implementation in workharbor. MCP exposes a bounded set of forge operations
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
| Open or update a PR | Existing prepared candidate, agent branch and exact approved SHA; existing publication service path | Guarded PR capability; no direct transport bypass |
| Merge, tag, release or deploy | Always denied for agents | No callable operation or passthrough |

Provider-independent request limits cover argument and response bytes,
pagination, concurrency and deadlines. Rate-limit responses preserve the
adapter's typed retry information; retries must repeat authorization checks.
Audit records identify run, repository, provider instance, operation, outcome
and applicable Decision/SHA without raw credentials or unrestricted request
payloads. Returned forge text and error details retain existing redaction and
untrusted-output handling.

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
