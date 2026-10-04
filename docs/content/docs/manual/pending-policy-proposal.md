---
title: Pending policy proposal
description: "Unapproved project-policy authority options and conditional reader contract."
weight: 15
toc: true
---

This preserves the conditional proposal from
[#283 comment 5981792414](https://github.com/wstein/workharbor/issues/283#issuecomment-5981792414)
before comment consolidation. Every authority and intentional-empty option
below remains pending Werner in the same existing desk round. Archiving it
settles no option and authorizes no implementation. The preserved store WIP
remains untouched; the capacity patch and human test G204 annotation remain
pending separately. This page is outside operative rule inputs.

Existing D38/D52/D53 require reviewed project policy independently of the
selected external package. Neither new authority mechanism below is approved.
D54's canonical role data cannot approve one. No current Git HEAD, package
digest, manifest or self-written receipt proves human review.

## One answerable question batch for wh/desk

1. Which immutable project-policy authority should production use?
    **A (5/5, recommended): operator-installed reviewed snapshot** of the
    default-branch revision and an exact approved policy-file inventory, registered
    explicitly through a supervisor-owned operator action outside agent-writable
    roots. Snapshot includes full source commit, exact bytes/digests and operator
    approval record; ownership/permissions alone do not imply review.
    **B (4/5): immutable Git commit plus explicit operator review receipt and
    approved policy-file inventory**, validated by hostgit, then retain the same
    exact bytes/proof outside agent-writable roots. Git identity alone is not
    review. This adds a Git materialization/verifier path.
    **C (2/5): keep staged refusal** until a different reviewed mechanism is chosen.

2. May a project deliberately have empty policy?
    **A (5/5, recommended): explicit operator-reviewed empty snapshot/inventory**,
    with a positive approval record and digest; required package project inputs
    still fail if absent. **B (3/5): require a nonempty approved policy file** for
    every project. Missing/unavailable proof is never intentional emptiness.
    Explicit skill selection `none` remains independent: it still needs reviewed
    project policy and supervisor briefing, without package role/model/mount needs.

Reason confirmation is required: .agents/design.md and the pinned wh-design
instructions reserve decisions changing security-control kind or product
direction for Werner first. These choices determine the source of policy
authority and whether deliberately empty projects can run. No existing review
approved either mechanism.

The existing human-requested test-only G204 nolint question remains with desk;
this batch does not re-ask it or alter the author's preserved store WIP.

## Proposed stable reader/proof interface (conditional on answers)

Keep the existing `service.ProjectInstructions` callback signature
(context, task, run) and `Revision`, `Text`, `Inputs` result fields.
Add a separately validated `Proof *ProjectPolicyProof` result. Nil means absent
proof, never trust. Production may not populate proof from package content,
agent checkout, model response or a test fixture. Additive fields need separate
implementation/review; this proposal changes no current service API.

`ProjectPolicyProof` version 1 has:
- `proof_version`; canonical configured repository identity; `source_kind`
  (the ONE chosen mechanism, no automatic fallback); full immutable source
  commit; `inventory_sha256`; ordered exact `files[{path,sha256}]`.
- `approval_id` and approval-record digest, bound to that repository, commit,
  exact inventory and intentional-empty flag; derived `proof_sha256`.
- `empty` as explicit operator intent, only if Werner allows it. A missing,
  unreadable, null or unapproved inventory does not become an empty policy.
- Loaded composition content digest remains `ProjectSHA256`; exact revision
  and proof identity are persisted separately. `Inputs` identifies the approved
  named inputs actually loaded; it is not package-provided trust metadata.

The operator approval record is accepted only through a reviewed host-owned
installation/registration path, never by finding a JSON file or matching a
self-reported hash. No claim of cryptographic signer identity is made by a
filesystem record. Agents cannot install, replace, approve or upgrade authority.
The record contains no login or credential material. Reject aliases/overlap with
workspace/agent/home/credential roots, unsafe paths, links/devices/executables,
unsafe ownership/write permissions and unknown/duplicate fields. Bound and
validate before reads; do not truncate. Exact inventory limits follow the
existing bounded instruction/store policy; the selected approval inventory,
not implicit AGENTS/override/nested-file discovery, decides loaded policy.
Approved local contribution files are listed explicitly. No guest HOME mount,
credential/keychain lookup or package-supplied policy provider is involved.

Select and pin the policy snapshot, approval proof and fully loaded text BEFORE
launch. Persist them with run provenance even if later launch fails. Resume
loads the ORIGINAL retained record and bytes, not current project/default policy.
Policy updates affect only new runs. Refuse deleting revisions used by resumable
runs; unavailable or changed original proof/content refuses resume until exact
restoration. No automatic rereview or permission upgrade occurs.

Fresh runs, including explicit skill `none`, refuse missing/unavailable/invalid
proof, absent required policy inputs or empty policy not explicitly approved.
Keep `agent.ErrUnsupported` for an unavailable production reader/capability;
distinguish an invalid/tampered configured proof as a validation error. Both
return no usable instructions/proof and launch no agent. A configured nil source
refuses startup of every NEW/nonlegacy run; the factory refusal remains until
this producer is implemented and independently reviewed.

Legacy/absent-provenance resumes keep their existing explicit legacy path; do
not fabricate proof, rewrite history or silently migrate authority. A legacy
run requiring the new native binding contract must refuse that selection or
start a separately approved new run. Additive migration records legacy/unknown
rather than inventing review provenance.

## Conditional source handoff after approval + landed specification

Existing #283; wh/runtime in its existing clean workharbor-runtime worktree:
host-owned reader and focused policy tests, then serial
`internal/service/skills.go`, service/store provenance/migration and
`internal/serve/real.go` wiring. Author returns exact new policy-package filenames
and complete shared/generated inventory before claiming; avoid arbitrary second
worktrees. Coordinate with the preserved store WIP first. Keep SkillBinding and
PrepareSkills unsupported until independent measured/native/runtime gates pass.
Basic `none` may bypass package hooks only after the approved policy reader works.

Offline reader/refusal/retention tests can proceed after the authority answer and
specification land. They do not authorize target login, inference, containers,
costs or support claims. #34/#241 measurement needs remain actual dependencies.
