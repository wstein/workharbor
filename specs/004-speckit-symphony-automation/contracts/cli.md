# Contract: Task automation CLI and ports

Status: proposed; commands are future interfaces, not runnable today.
Entry: `python3 scripts/task_automation.py COMMAND`. Emit one sanitized JSON envelope:
schema version, package digest, requested/effective mode, progress, offline result,
approval assurance, review/live observation references, raw exits, blockers and next
action. Missing evidence stays null/false. Never echo raw argv, exceptions or test output.

| Command | Inputs | Effects |
| --- | --- | --- |
| prepare | --input FILE --output DIR | Local package/summary; no check execution |
| validate | --package FILE --offline | Shared validation, no tracker/runtime access |
| effective | --package FILE --offline | Expected settings/missing capabilities; no activation |
| ready | --package FILE --live-config FILE | Host observation/authentication, then selected issue eligibility |
| resume | --package FILE --attempt ID plus --offline or --live-config FILE | Reconcile existing guard/handoff; offline uses disposable ports |
| handoff | same as resume | Fresh guard verification, review/publication/evidence/final labels |

Live configuration contains trusted repo/owner IDs, exact source/runtime paths,
non-secret settings and host credential references. Never put secret values in
packages/checkouts/comments/logs/children. Explicit approved external actions are
required; package content grants no permission. Limits: 1 MiB input, 100 checks,
10,000 cases, positive check deadlines, three transport retries. Exceeding limits
blocks without truncation. Safe diagnostics expose identifiers/remediation only.

Exit classes: 0 requested step succeeded (not a live claim); 2 malformed/unavailable
interface; 3 approval/source/scope/capability blocker; 4 outcome mismatch; 5 ownership
conflict; 6 persistence/external uncertainty. Raw red exits remain separate even
when the reviewed expected-outcome matrix validates successfully.

## Shared validator and CI trust boundary

`task_package.validate(package, repository_snapshot, evidence, capabilities)` is
side-effect-free; CLI and CI call it and delegate context/comparison/scope/acceptance
to Feature 003. An unavailable backend blocks that validation, never assumes success;
structural preparation can succeed with `execution_eligible: false`.

`scripts/task_outcomes.py` is the shared reviewed-matrix check runner used by package
validation and CI. Its explicitly approved argv is also passed to Feature 003's
acceptance interface: exit zero means the matrix matched, while raw test exits and
outcomes remain in bound evidence. This reuses the guard's zero-exit contract without
changing its context schema or duplicating scope/completion logic.

Outcomes enumerate exactly all collected cases once. Reject missing/extra/duplicate
IDs, empty suites, import/setup/cleanup/subtest errors, skips/xfails/timeouts, unexpected
green/red and baseline regressions. Red fingerprints match reviewed assertion identity,
not arbitrary exception text. Acceptance checks content before/after execution;
mutating checks invalidate evidence. Test-authoring verifier success can satisfy its
declared prerequisite while retaining reds; successor implementation needs its own
acceptance. Default implementation/handoff matrices require every check pass.

CI pins trusted validator/policy source independently of proposal data, without
credentials/write permissions. Only revision-approved argv runs under host isolation;
never execute task prose. Proposal changes to validator/CI require independent review
and renewed trusted pin. No self-approved allowlist or task-specific exception fallback.
Keep current sealed phase gates until separately reviewed migration.

## Source and effective readiness

Reject unavailable objects/history, dirty first entry, unexpected parents/merge order,
unknown commits, tree/index/worktree drift, symlinks/gitlinks/nonregular modes and
protected-input changes. Inspect every intervening commit including commit/revert.
Synthetic PR acceptance requires trusted event repository/base/head, exactly two
ordered parents matching pins, exact expected merge tree and clean matching index/
worktree. It never promotes proposal content into accepted source.

Rebase evidence enumerates old/new commits/parents, independently verified patch
correspondence, protected artifact bytes and complete resulting tree against reviewed
base changes. Range-diff supports human review, not the sole machine oracle. Conflicts,
overlap or changed patches require full candidate review and renewed affected approval.
Unknown equivalence blocks; no silent pin update or broad ancestry exception.

Readiness authenticates separate owner actions, compares artifacts, validates
prerequisites/mode, actual runtime provenance/workflow/configuration and all capabilities
needed by approved execution. Entry repeats these gates before calling Feature 003.
Changed workflow/runtime/argv requires renewal. Rendering neither starts Symphony nor
adds ready nor proves actual sandbox/secret behavior. Dirty retries preserve work and
use guard recovery; no clean-baseline invention.

## Guard and tracker ports

Call Feature 003's planned context-based begin/inspect/verify/complete/record-commit/
resume/status and authorized rework interfaces. No legacy begin fallback. Guard state,
mode and immutable commit are authoritative; orchestration cannot clear/check tasks.

Tracker operations: read_approval, read_issue, find_pr, authorized publish_pr,
find_evidence, write_evidence, replace_labels(expected, desired). Each write has a
durable intent and stable task/attempt key. After timeout query exact identity/content
before retry; ambiguity conflicts. Current pinned Symphony github_api is transport,
not a capability proof. GitHub has no demonstrated conditional full-set label write;
safe-label capability is unavailable. Live eligibility blocks until a provider or
verified deployment proves conditional replacement or serialization of all writers.
Local mutex/read-PUT-read-back cannot establish that guarantee. Offline fakes test it.

Handoff reruns acceptance against bound implementation and checks exact-candidate
independent review/publication permission. Failed completed-task revalidation preserves
evidence; corrective rework requires owner authorization. Reuse PR/evidence before
labels. Record blockers/recovery first; one final operation preserves unrelated labels,
removes ready, adds human-review/blocked and leaves issue open. No later agent action
is required; uncertain result stays incomplete for operator reconciliation.
