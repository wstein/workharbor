# Bounded design: Proposal, integration and approval evidence

Status: design contract for the owner's four requested automation corrections.
This adds no task queue, runtime activation, merge permission or approval of the
remaining Feature 004 plan. The approved specification stays unchanged. The
reviewed PR38 plan candidate is `93271ab7d1ec37400eb033e65972828ef1013777`;
its accepted-source ancestor is `670ef69e733b8d5d34b5660af4999218f613b3db`.
These are local provenance observations, not claims of a fresh forge fetch.

## Separate proposal and integration gates

Use three distinct identities: trusted accepted policy source, current target base,
and proposal head. The accepted policy pin is explicit and independently trusted;
keep historical acceptance pins intact. Never add a blanket executable-path exemption
or silently refresh an accepted pin to proposal code.

Proposal scope and approval checks inspect the exact head, its index/worktree and
each proposal commit from `merge-base(base, head)` through head. Validate the
trusted accepted-base history and task's approved scope, including commit/revert,
modes, symlinks and gitlinks; policy equivalence follows the contract below. A
synthetic merge commit is never a task-authored commit and never changes the
approved scope. A head failure cannot be cleared by merge checks.

Integration separately authenticates the PR repository/event/base/head, exactly two
ordered merge parents `(base, head)`, and the actual Git merge result. Recompute the
merge tree from pinned base/head; require exact checkout-tree and clean index/worktree
agreement. Conflicts, unavailable objects, unexpected parents or a fabricated tree
block. Run the required checks against this resulting tree as well as the applicable
head gates. A matching tree alone establishes neither scope nor approval.

## Base advancement and protected prerequisites

Compute proposal changes from the PR merge base to head and base changes from that
merge base to current base. Inspect each history separately. Record whole-tree
identities for reproducibility; do not require the advanced base or merge tree to
equal the old approved repository tree. Unrelated trusted-base documentation updates
may advance without renewing a task's content approval.

Protect the approved requirements, task description/scope, check argv/outcomes,
dependencies/order, prerequisite evidence, verifier/policy code and execution inputs.
If base changes touch a protected prerequisite, revalidate that prerequisite using
its approved contract and trusted verifier. If required revalidation/comparison is
unavailable, changed or ambiguous, block the affected action. The existing sealed
bootstrap gate may conservatively block changed executable/test/configuration inputs
until such evidence exists; it must not pretend that a blocked prerequisite passed.
Proposal edits to protected inputs still need their own explicit authorization.

## Immutable policy authority, content-equivalent history

Status: proposed bounded plan revision for the owner's requested reduction in
repeated approval administration; no claim of plan approval or implementation.
The owner independently adopts one reviewed immutable policy commit. Keep that
commit as authority and keep historical acceptance pins unchanged. New policy
content requires one explicit owner adoption after independent review; a proposal,
manifest or passing check cannot adopt itself. This changes no host permissions.

The accepted checker derives a deterministic content manifest from that trusted
Git object, never from candidate declarations. Record exact path inventory, Git
type/mode and raw-blob digest for checker, workflow, operator tools and paired tests;
bind the tool/test mapping, schema/contracts and authority inputs that define their
scope, dependencies and ordered checks. The bounded inventory is `OPERATOR_PATHS`,
`.specify/memory/constitution.md`, `docs/development/{harness,prepublication,approval-binding}.md`
and Feature 004's `bounded-source-contract.md`, `contracts/cli.md`, `data-model.md`.
Keep imported guard, build/check scripts,
tests, dependency/configuration inputs and frozen Feature 001–003 artifacts under
their existing immutable accepted baseline. Enumerate the dependency closure in
trusted policy; added, removed or renamed inputs cannot escape by falling outside
a candidate-supplied inventory. Ordinary documentation outside these protected
inputs remains eligible for the existing documentation rule. Task-content approval
still uses GuardPort's protected contract and comparison rules below.

Head and actual integration must match the adopted manifest exactly. Validate the
current accepted base separately, including its protected prerequisites. A real
rebase or squash with identical protected content need not contain the policy
commit as an ancestor and needs neither a new policy pin nor another policy-content
approval. Record the new commit/tree identities and comparison evidence; retain
fresh exact-candidate review and integration checks. Unrelated accepted-base
updates need no policy renewal. Changed policy, scope, checks or dependencies
invalidate affected approval; unavailable or ambiguous evidence blocks.

Content equality does not replace history validation. From the existing trusted
accepted-base anchor, validate the trusted policy history and current base history;
then validate every proposal commit from the unique PR merge base to head. The
base may end at a reviewed intermediate state before policy adoption; head and
integration must reach the final adopted state. Keep
protected-path commit/revert rejection. Permit policy transitions only by matching
the complete protected-policy manifest states in the reviewed source history, in
order. Consecutive duplicate states may collapse; a squash may skip contiguous
reviewed transitions to their exact resulting state. Other changes must satisfy
existing documentation/task scope and type rules. This permits reviewed policy
replay, not arbitrary code changes on operator paths. Reject invented, reversed or
mixed policy states, protected changes followed by reverts, missing objects and
ambiguous/nonlinear transition evidence; do not delete ancestry guards wholesale.
Synthetic integration parents remain a separate authenticated `(base, head)` gate.

Required disposable-Git evidence includes actual rebase and squash candidates with
identical manifests but no policy-commit ancestry, plus unrelated accepted-base
updates. Negative cases change checker/workflow/tool/test bytes, inventory, modes,
manifest/schema/contract inputs, scope or dependencies; include protected
commit/revert, dirty/untracked/index content and symlinked paths/parents. Retain
Git metadata/configuration, event/repository identity, frozen artifacts and all
existing check/outcome gates. These are required cases, not passing-test claims.

## Reusable prepublication preparation

The bounded interface is a separate preparation tool, not the future package CLI:

```text
python3 -I -B scripts/prepare_publication.py --repository REMOTE --source ACCEPTED_SHA
  --base-ref refs/heads/main --base BASE_SHA
  --head-ref refs/heads/topic --head HEAD_SHA [--timeout SECONDS]
```

An authorized caller supplies a trusted credential-free local/HTTPS repository and
exact full object IDs. In private scratch storage, fetch and verify those references
and accepted checker source, construct the actual merge result, and invoke the
accepted checker for the pinned PR context. Do not copy credentials or use a
proposal-supplied checker. This tool may access the remote; offline package
preparation remains network-free and inert.

Emit sanitized JSON containing `schema_version: 1`, `status: PREPARED|BLOCKED`,
`source`, `base`, `head`, `checkout`, `tree`, `check_exit` and `blocker`. Missing
observations remain absent/null; failed/missing checks never become PREPARED.
Record check exit separately from the preparation outcome. No raw exception,
credential, check log or argv is copied into the result.

Re-read both remote refs after checks. Either change invalidates the prepared result;
repeat preparation on new explicit pins. The publisher must re-read both pins
immediately before its separately authorized write and block on drift. This is a
point-in-time freshness guard, not atomic compare-and-swap across a forge write:
no cross-operation atomicity claim is permitted. Preparation never pushes, creates
a PR, merges or proves independent review/publication authorization.

## Content-bound approval through the existing guard

Use Feature 003 as the sole source of task-context and semantic comparison rules.
The bounded approval adapter accepts opaque guard contexts/evidence and snapshots:

```text
GuardPort.validate_context(context, evidence)
  -> ValidatedContract(contract_digest, protected_paths)
GuardPort.compare_artifacts(approved_snapshot, current_snapshot, evidence)
  -> identical | progress-only | renewal-required | ambiguous
```

Context validation owns the complete Feature 003 schema, dependencies, checks, mode
and stale-evidence rules. Equal artifact bytes need no progress comparison, but both
contexts still require guard validation. Record the comparator revision and evidence.
Approval binding is SHA-256 of canonical JSON containing the guard-validated contract
digest and each protected path's mode/raw-byte digest. Whole repository/source-tree
identity is recorded separately. Never exclude requirements, task text/scope,
acceptance, dependencies or order merely because another commit changed them.
`progress-only` retention requires equal validated contract digests, unchanged exact
protected path inventory/modes and the guard's narrowly proved checkbox/appended-
evidence normalization. Those permitted raw-byte changes can yield a different
current binding. Retention authenticates the original approved binding/action and
records approved/current bindings plus guard equivalence; it never rewrites the
original approved package/revision or claims provider approval of the new binding.
Do not introduce a general whitespace/section normalizer.

An authenticated provider port verifies owner identity/action/reference and the
original approved binding. Package/source digests identify each snapshot separately
and are not substituted for that approval identity. Supplied offline approvals are fixtures only. Missing Feature 003
comparison/context APIs and missing authenticated approval ports fail closed; default
unavailable adapters cannot approve execution. A fake port proves the adapter
contract only. Do not duplicate the missing guard or reinterpret legacy begin as it.
An unchanged binding may retain task-content approval after an unrelated base update;
a new package/source snapshot is still recorded and revalidated explicitly. Independent
review remains bound to the exact candidate SHA. Content retention grants neither
review transfer nor publication, rework, merge, release or deploy permission.

## Required bounded rejection and regression evidence

| Case | Required outcome |
| --- | --- |
| Head changes an unauthorized path, while synthetic merge passes checks | Reject head scope independently |
| Unrelated trusted-base docs change, unchanged task contract | Retain content approval with comparison evidence; validate new integration |
| Base changes an affected protected prerequisite | Revalidate with trusted approved evidence, otherwise block |
| Unexpected merge parents, conflict or tampered checkout tree | Reject integration |
| Head/base ref moves during checks or before publication | Block stale preparation/publication; no write |
| Requirements, scope, checks, dependencies or ordering change | Renew affected approval |
| Checkbox/evidence-only change with demonstrated guard normalization | Retain content approval; record comparison |
| Missing guard/authentication capability or ambiguous comparison | Offline structural result only; execution blocked |
| New candidate SHA with unchanged contract | Fresh exact-candidate independent review |

Use disposable repositories and injected ports; include commit/revert and dirty
index/mode cases. Keep all existing Features 001–003 artifacts and frozen acceptance
contracts byte-identical. Report raw test exits, narrowed gate outcomes and live
observations separately. These are required tests, not claimed passing results.

## Promotion of the complete approved snapshot to main

A pre-baseline main base may be promoted only when it is an ancestor of the fixed
accepted policy anchor, the source retains that immutable authority, the head
proves its accepted prefix and validated ordered history, and the complete source,
head and actual integration trees match.
GitHub metadata must name main; local preparation uses the trusted canonical-main
guard. This narrow path does not claim the old base has current prerequisites;
all protected source/head/integration content and execution checks still apply.

Policy identities use raw Git objects throughout materialization, comparison and
check execution. Replacement refs and graft metadata are rejected before trusted
identity/history reads; inherited Git substitution settings cannot redefine a pin.

An owner full-range rebase may rewrite the baseline commit identity. The candidate
then needs a unique trusted common prefix and linear complete-tree states matching
the immutable accepted prefix in order, reaching the exact accepted baseline tree.
Only that proven candidate anchor starts the existing ordered policy/history gates.
Unknown or reversed prefix states, protected commit/revert and missing baseline
snapshots fail closed; whole historical-prefix squash without that snapshot is
unsupported. The owner rebase-only merge rule and original source pins remain.
