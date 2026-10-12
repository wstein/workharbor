# Data model: Task automation

Status: proposed schema v1; new interfaces below are not implemented. Use bounded
strict UTF-8 JSON, reject duplicate/unknown/missing keys and nonfinite numbers.
Canonical JSON: sorted keys, compact separators, ensure_ascii=False, no trailing
newline; SHA-256 hashes raw/canonical bytes as specified. Paths must be canonical
repository-relative regular files, without symlink components, escapes or gitlinks.
Full Git IDs resolve locally; never implicitly fetch arbitrary input references.

## TaskPackage — immutable proposal

- `schema_version: 1`, trusted `repository`, `issue` (repo/number or null for fixtures),
  `feature`, unique `task_id`, requested `mode`.
- `context`: exact Feature 003 TaskContext object. Artifact identities, dependency
  table/proof, scope, constraints, argv/timeouts and rework predecessor follow the
  [Feature 003 schema](../003-single-task-entry/data-model.md), without a competing format.
- `source`: accepted commit/tree, proposal commit/tree, ordered proposal commits,
  artifact revisions and optional equivalence evidence digest. Synthetic CI wrapper
  observations never replace accepted/proposal identities.
- `execution`: Symphony revision/build provenance, workflow/template digest, rendered
  non-secret configuration digest, guard code revision, capabilities per action,
  ordered external action allowlist and publication scope.
- `outcomes`: unique ordered check IDs, exact collected case IDs, each expected
  classification (`pass` or reviewed in-test `assertion`), bounded safe red fingerprint,
  suite/tool/input revision and expected raw exit. Default requires all pass; only
  separately approved test-authoring matrices admit assertions. Successors have their
  own matrices and approvals.
- `approval_refs`: distinct spec/plan/execution/matrix/rework references as required.

Hash immutable fields except `approval_refs` to obtain `package_digest`; the approval
then explicitly binds that digest/action without circularity. Guard context retains
its documented excluded self-digest. Timestamps/local output paths/random attempt IDs
do not affect identity. Repeat inputs reuse package/summary; changed bytes create a
new revision, never overwrite. Approval comparison does not mutate the package.

## ApprovalDecision and ApprovalComparison

Decision fields: provider object ID/URL/repository, immutable actor ID, configured
owner ID, action, reviewed revision/package digest, object content digest, times,
assurance (`supplied-local` or `authenticated-provider`), verifier/source identity.
Offline assurance stays local. Spec/plan approval cannot substitute for execution,
independent review or publication permission. Store approval object state and reject
edited/deleted/dismissed/mismatched records at readiness.
Comparison fields: approved/current raw artifact digests, Feature 003 comparator
revision, normalized identities/evidence references, result (`identical`,
`progress-only`, `renewal-required`, `ambiguous`) and safe reason. Only the first two
retain approval; no broad section/whitespace normalization.

## EffectiveExecution — observed snapshot

Package digest; actual source/HEAD/index/worktree inventory; optional wrapper event,
base/head/parent/tree identities; equivalence review; actual runtime/build, workflow
and effective configuration digests; guard revision; derived mode/checks/actions;
capability evidence. Each capability has availability, implementation revision,
fixture result and live observation reference when required. Requested execution
derives its required set. Unavailable guards/runtime provenance/authentication/safe
labels/publication block live readiness. Preview is inert, not activation. Revalidate
at entry and after checks; drift blocks without adopting new pins or baselines.

## HandoffRecord — separate durable orchestration journal

Key: repository/issue/package digest/guard attempt ID. Retain guard's immutable
implementation commit/content digest, current verification HEAD, fresh check/review
references, publication authorization, expected PR head/base, PR ID/URL, evidence
object ID/digest, blockers and final-label outcome. Append events with monotonic
sequence and durable intents/results. One command holds the local process lock;
one owner per issue binds canonical checkout/host/process identity. Competing
checkouts/hosts must reconcile ownership before eligibility, never rely solely on a
worktree mutex. Unknown ownership blocks without deleting another owner's evidence.

Derived progress: prepared → validated-offline → ready → guard-active →
completed-awaiting-handoff → reviewed → published → evidence-durable →
final-label-intent → handed-off. Failure appends a blocker without reopening work.
`write-unknown` retains operation key, intended remote identity/content and
precondition; recovery queries remote state and records success, safe retry or
ambiguity. One exact PR/evidence per task/attempt/bound commit; multiple matches
block. Changed inputs conflict rather than reuse an old operation key.

Final label intent binds observed set/version, desired set preserving unrelated
labels, evidence digest and target human-review/blocked. The provider conditionally
replaces or proves serialization of all possible writers. Conflict rereads/recomputes;
unsupported provider does not write. Persist intent/recovery instructions before
the request: the agent may stop immediately when ready disappears. Confirmation
belongs to the final provider operation or later operator reconciliation; no required
subsequent agent action. Unknown/failed writes stay incomplete, issue stays open.

## AdministrationObservation

Sample/task/category, package/source/runtime/tool revisions, interval boundaries,
active administration seconds, explicit implementation/computation/waiting exclusions,
retry relationship and evidence. Preparation, active approval work, check/configuration
administration and handoff repair count. Publish five consecutive eligible tasks
individually; missing/selectively omitted intervals invalidate the claim. Estimates
and offline runs remain unmeasured.
