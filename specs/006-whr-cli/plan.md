# Implementation Plan: WorkHarbor installable pilot

Branch: `feat/installable-pilot` | Date: 2026-10-14
Spec: [spec.md](spec.md)
Baseline: `f02250fc75b1e72a1d949edaee128e31fffbaa53`
Status: execution in progress. T001 scaffold independently Sol CLEAR at
`d3d84c8361a1389292fad96d02a828ac9fabc2e3`; later work items remain open.
Scaffold checks do not establish product runtime verification.

## Authorized objective

The owner explicitly requested on 2026-10-14 that work continue in this WorkHarbor
worktree, on this branch, through the installable pilot, including selective
Python-to-Go migration, independent review and fixes after each work item, and
one PR after all internal-pilot work items are accepted, for final human review.
The pilot is internal; release publication is outside this execution scope. The
same worktree is retained.
This is a WorkHarbor task; no SheetMill files or sibling product scope are changed.

## Work-item sequence

1. Complete and review the combined Feature 001 / Feature 006 pilot design and
   acceptance mapping. Resolve retry semantics before product code.
2. Pin and scaffold Phoenix control plane, Go host agent, CLI and shared API client;
   establish explicit application format, build and test gates.
3. Inventory selected Python behavior, freeze reference vectors, port pure product
   primitives to Go and prove parity. Retain the Python development harness.
4. Implement ownership, authentication, durable lifecycle operations and audit.
5. Implement mTLS enrollment, host identity rotation/revocation and reconnect.
6. Implement trusted Git/devcontainer resolution, Docker provider and persistent data.
7. Implement browser and CLI lifecycle paths, bounded terminals and persistent console.
8. Implement agent-session interoperation and read-only Spec Kit dashboard.
9. Implement isolated SSH, resumable host setup/offboarding and diagnostics.
10. Implement backup/restore, local install packaging and real-host/browser acceptance.
11. Resolve all review findings, run exact-head required checks and open the combined PR.

## Migration boundary

ADR 0010 permits selective ports with frozen Python behavior vectors. Python remains
development tooling, not a product dependency. Do not migrate Symphony, tracker,
provider eligibility or task-approval orchestration into product code. Product exit
codes come from ADR 0013 rather than Python development-script exit codes.

## Review and evidence

Every work item records its scope, prerequisites, actual check results and exact
candidate reviewed. Fix actionable findings before successor work. Missing tools,
unknown runtime outcomes and mocks are not passing real-host acceptance. Signed
release, deployment, activation and merge gates remain separate owner decisions.

## Technical design

Phoenix 1.8.15 / Elixir 1.20.4 / OTP 29 control plane with Ecto/SQLite;
Go 1.27.2 host agent, CLI and exported API client in separate modules under
`apps/host_agent`, `apps/whr`, `apps/api_client`, with a root `go.work`.
Pin direct and transitive dependencies in setup and prove clean-checkout builds.
Use Cobra 1.10.2/pflag for interspersed flags and completion, without Viper.
Standard net/http verifies TLS, refuses credential redirects and applies deadlines.
Use coder/websocket 1.8.15 for WSS relay and golang-jwt/jwt 5.3.1 for Ed25519
operation-scoped grant verification. No dependency version is claimed tested yet.

Linux/systemd/Docker is the first host target. Client build targets are Linux and
macOS amd64/arm64; mark only actual exercised combinations verified. Windows is
unsupported until credential ACL and terminal evidence exist. Reuse the official
Dev Container CLI, pin its exact tested version in the provider WI, and reject
untested capabilities before hooks or provider mutation.

The control plane owns owner login, scoped opaque credentials, durable operations,
audit, trust records and the owner API. Host agents connect outbound over mTLS.
The CLI never contacts a provider or host agent directly. Local host commands use
explicit privileged adapters. Dashboard credentials authorize reads only; browser
cookies are Secure, HttpOnly and SameSite=Strict, with Host/Origin and anti-forgery
validation on requests and upgrades. No direct credential in URLs or arguments.

A lifecycle submission allocates its stable workspace identity and idempotency key
before transmission. Retrying preserves both; changed payload conflicts. Retry at
most three times at 1/2/4 seconds on documented transient failures. Operator wait
interruption never cancels provider work. Agent input is not automatically resent.
The console is one process/session per workspace with a 64 KiB byte ring, dropped
byte reporting and attachment-independent lifetime. Use Feature 001 quotas, grant
TTLs, idle/maximum duration and cleanup bounds. Cancel pumps/release accounting
independently of graceful WebSocket close; force close within the five-second bound.

Grant claims bind issuer, owner, workspace, runtime generation, audience, kind,
operations, session when applicable, jti and expiry. Attach issuance and reusable
control-grant lifetimes are 60 seconds; no expiry grace. Use configured public keys,
never token-selected URLs. Atomic attach consumption survives interface restart;
emergency revocation closes active attachments. Validate before session lookup.
Agent-owned sessions remain separate from the platform console. The provider
session transport and SSH server/certificate provisioning contract must be finalized
and reviewed before those WIs; adding a hidden helper or configuring an agent tool
is not an acceptable substitute for an authorized in-container interface.

`whr ssh` uses Go SSH over an owner-authorized byte tunnel with a pinned server
key and fresh certificate, without personal SSH state. External config uses private
isolated files, states expiry and warns that external-client traffic is unfiltered.
Unsupported SSH capability fails before connection. Host setup/offboarding use
private atomic local checkpoints; success requires health or confirmed revocation,
respectively. Dry-run has no writes and incomplete/offline is never success.

Artifact views use repository/feature/task identity, immutable Git commits/content
hashes, parser capability, evidence source/time and availability. Safely render
untrusted Markdown; never execute it or fetch embedded references. Missing objects
or unsupported layouts remain unavailable, not fabricated task completion.

## Existing design reconciliation

Committed constitution v1.1.0 permits ordinary in-container agents; all tracker,
AI orchestration and provider account management remain development tooling.
Feature 001's older plan wording excluding product AI and conflating PTY/session
lifetime is superseded only by the accepted constitution and ADR 0012; its actual
lifecycle, authorization, recovery and release requirements are retained.
The explicit user execution request supplies implementation scope, but does not
supply a live host grant, signed release, merge or evidence that checks passed.

## Installable-pilot acceptance

All Feature 001 tasks and applicable Feature 006 internal-pilot stories remain
required, including SC-001–005 and SC-007–012. Additional command surface
(rebuild/removal, CLI auth/operations, host administration, agent controls,
dashboard and SSH) and local packaging/release-policy checks are not completed
by a scaffold. Each task requires its own meaningful failure-path tests and
independent review. SC-006 is excluded only from internal-pilot acceptance; full
Feature 006 completion remains open until its signed-release verification passes.
Application checks: Go format/vet/build/tests per module, Phoenix format,
warnings-as-errors compile and tests, plus inherited `make check` using Python
3.11+ in a pristine committed checkout. Extend the reviewed acceptance runner and
manifest explicitly; do not weaken discovery or call missing tools passing.

Real acceptance: fresh install, TLS login, enrollment/rotation/revocation, trusted
image/Dockerfile provisioning, console reattachment, stop/start/rebuild with exact
uncommitted file hashes, interrupted-operation recovery without duplicate runtime,
agent interface, dashboard authorization/browser-origin negatives, SSH, resumable
offboarding and quiesced clean restore. Measure Feature 001 five-workspace/eight-
attachment load, timings and idle/load footprint. Stub tests do not prove real use.

T023 produces local unsigned archives labelled development artifacts, checksums,
SBOM and recorded build inputs/provenance, plus clean install/uninstall evidence
and draft-only release machinery/policy tests. T024 requires the real-host,
browser and load acceptance above and exact-head checks/independent Sol review
before the combined PR is handed to the owner for final human review.

The owner-authorized allowlisted human-signed release exercise on `main` and
end-to-end SC-006 verification are a separate follow-up after this internal-pilot
handoff; they cannot be prerequisites for the PR introducing the implementation.
Full Feature 006 remains open until SC-006 passes. No signed-release evidence is
claimed from an unsigned archive. Signed release tags, public distribution,
package channels and deployment need separate owner authorization. Actual
disposable Linux host, TLS hostname/certificate and live-test scope remain
owner-controlled inputs required for pilot acceptance; signer allowlist and
release authorization are inputs to the later release exercise. A configured
Docker context establishes none of them.
