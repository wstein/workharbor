# Approval content binding

Status: bounded offline adapter contract. No Feature 004 task execution, authenticated
provider integration, approval comparator, publication or dispatch is implemented.

Approval is bound to a validated task contract and exact protected artifact paths,
Git modes and raw bytes. Accepted-base commits and complete tree identities remain
comparison evidence, outside that binding: an unrelated accepted-base update can
retain approval. Scope, requirements, acceptance argv/outcomes, dependencies/order,
mode and artifact semantics belong to Feature 003's validated contract. Any changed
contract digest, protected path/mode or unproved artifact change requires renewal.
Independent review still binds the exact candidate commit; publication is separate.

## Bounded interface

`scripts/approval_binding.py` exposes `assess_retention(root, request, guard=...,
authority=...)`. The request has exactly `schema_version: 1`, full locally resolvable
`approved_revision` and `current_revision`, opaque `approved_context` and
`current_context`, opaque `evidence`, inert `approval_ref`, `approval_action` and
`approved_package_digest`. The latter remains the original reviewed package digest,
even when a new base produces a different current package. No input is fetched,
evaluated as shell, executed as a check, or used to select Python adapter code.

Trusted host code supplies these ports, never proposal JSON:

- `guard.validate_context(context, evidence)` returns `ValidatedContract` with the
  Feature 003 validated semantic `contract_digest` and complete `protected_paths`.
  It owns context/schema validation, scope/requirements/checks/dependencies/mode and
  freshness of prerequisite evidence. It must reject unknown/missing context fields.
- `guard.compare_artifacts(approved, current, evidence)` returns `ArtifactComparison`
  with `progress-only`, `renewal-required` or `ambiguous` and a durable proof reference.
  Byte-identical snapshots are compared directly. Snapshots retain exact raw bytes.
  Only Feature 003's documented checkbox and append-only verified EOF evidence
  normalization can return progress-only. It must reject changed/removed footers,
  invalid/stale references, fenced pseudo-tasks and all semantic/whitespace edits.
- `authority.verify(approval_ref, target)` returns `ApprovalVerification`. The
  `ApprovalTarget` contains action, original reviewed revision/package digest and
  original content binding digest. The returned target must match exactly.
  The external verifier authenticates the configured owner/action, repository,
  immutable object identity/content, exact binding and current undeleted/unedited/
  undismissed object state. It supplies an inert durable evidence reference and
  assurance `supplied-local` or `authenticated-provider`. Offline fixture assertions
  stay supplied-local; strings in a request cannot establish owner identity.

The binding digest is SHA-256 of canonical UTF-8 JSON (sorted keys, compact
separators, no trailing newline), with exactly `schema_version: 1`,
`contract_digest`, and `artifacts`: sorted records of `path`, `mode`, `sha256`.
Raw artifact digests remain in output even for progress-only comparison. The current
HEAD must equal its full requested revision; its index/worktree must be clean,
including nonignored untracked files. Protected entries must be regular Git files,
with no symlink/gitlink ancestors; physical modes are checked even if core.filemode
is false. Recheck the snapshot after external verification to reject concurrent drift.
Physical reads reuse Feature 003's descriptor-relative directory traversal, open
files without following links, reject hardlinked/nonregular files and bound reads
to the expected Git blob size. Limit protected paths to 1,000, blobs to 1 MiB each,
and each local Git call to ten seconds; exceeding limits blocks.

The result is sanitized comparison evidence: result, retained flag, approval
assurance, original/current raw binding digests, approved/current snapshots,
comparator revision/proof reference and a safe blocker code. The original reviewed
revision is never relabeled as the new current revision. Progress normalization
does not overwrite either raw identity or the approved package identity.
`retained` means only this approval comparison passed; it grants no task execution,
review, source-equivalence or publication permission. Supplied-local assurance can
retain only an offline assertion. No approval identity is manufactured locally.

## Current capability gap

The accepted Feature 003 implementation exposes legacy `begin`, `scope`, `verify`,
`complete` and durable state primitives, but no TaskContext validator or approval
comparator. Those functions do not implement this port. `Unavailable003` therefore
blocks real integration; there is no legacy fallback and no copied context schema or
normalizer. `UnavailableAuthority` similarly blocks absent authenticated evidence.
The default call returns a capability blocker. A future reviewed Feature 003 API and
host-side authenticated provider verifier are required before production use.

## Verification contract

Disposable Git fixtures prove that an unrelated new base preserves the same binding,
while changed protected bytes/modes/paths, dirty index/worktree, unsupported schemas,
extra fields, stale fixture evidence and changed synthetic scope/requirements/checks/
dependencies/mode fail closed. A finite synthetic guard exercises progress-only port
responses and semantic negatives; it is explicitly not the Feature 003 comparator.
Default unavailable adapters and absent/invalid/stale authority block. No test claims
real provider authentication, generic normalization or live Symphony behavior.

Tests live under `scripts/` so the separately approved, frozen Feature 003 case
collection remains unchanged; the operator-tooling acceptance path must execute
every operator test strictly green in addition to the exact baseline verifier.
Run `python3 -m unittest discover -s scripts -p test_approval_binding.py -v`, then
`make check`. Keep baseline raw failures separate from these focused results.
