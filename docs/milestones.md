# Milestone roadmap

These GFM checklists describe completion gates, not execution approval. Feature
specs, plans and task files remain authoritative. Checking a roadmap item or
assigning a GitHub milestone never authorizes dispatch. Close a milestone only
after owner review of revision-bound evidence; issue counts alone are insufficient.
No GitHub milestone objects are created by this document.

## M1 — Local execution guards verified

Priority: first. Completion scope: all of [Feature 003](../specs/003-single-task-entry/tasks.md).
Local verification requires neither a tracker nor a running Symphony service.

- [x] T001–T010: fixtures, durable state and validated entry accepted in merged PRs [#49](https://github.com/wstein/workharbor-nxt/pull/49) and [#50](https://github.com/wstein/workharbor-nxt/pull/50), with earlier prerequisite work.
- [ ] T011–T012: approve exact test-authoring matrices and verify their expected outcomes; distinguish expected-red evidence from implemented behavior.
- [ ] T013–T017: implement and review scope inventories, content-bound review, acceptance execution and durable completion.
- [ ] T018–T019: approve and verify recovery/rework test-authoring matrices.
- [ ] T020–T023: implement and review commit binding, crash recovery, authorized rework and resume commands.
- [ ] Future remainder, T024: verify competing processes, interrupted lock ownership and persistence failures.
- [ ] Future remainder, T025–T026: audit preserved regression coverage and document the complete local interface.
- [ ] Future remainder, T027–T028: run final acceptance and record actual Linux/macOS evidence, including any missing-platform blockers.
- [ ] Owner accepts the final independently reviewed and merged implementation; required checks are green and all required guard scenarios pass.

The current authorized implementation queue ends at T023. T024–T028 remain M1
completion gates before M2, but require separate owner implementation authorization;
this roadmap does not extend the queue or grant task approval.

## M2 — First live Symphony smoke test

Priority: after M1. Use the bounded documentation task from
[Feature 002](../specs/002-symphony-doc-smoke/tasks.md). Its T001 creates the
operator guide; completing that guide does not prove a live run.

- [ ] Review the existing Feature 002 spec, plan and issue draft; obtain the required owner approvals without silently changing their scope.
- [ ] Approve one task's exact execution revision, allowed paths, acceptance commands and prerequisite evidence.
- [ ] Verify the pinned Symphony runtime, effective workflow, authenticated owner/tracker access and supported final-label operation; unresolved capability gates block dispatch.
- [ ] Complete and review Feature 002 T001's guide within its approved allowlist.
- [ ] Under separate live authorization, execute one bounded documentation task through real Symphony and capture normal completion, commits, checks, PR and final handoff evidence.
- [ ] Exercise an interrupted handoff and verified handoff-only retry in an approved disposable run; retain the same implementation and avoid duplicate PRs or lost labels.
- [ ] Owner reviews the live evidence and accepts the milestone; missing or uncertain results remain explicit blockers.

Live exercises require separately approved scope and evidence records. They are
not additional implementation work implicitly added to Feature 002 T001, and do
not authorize reimplementation if T001 has already been accepted.

## M3 — Automated handoff proven

Priority: after M2. Scope: [Feature 004](../specs/004-speckit-symphony-automation/tasks.md).

- [x] T001–T018: local automation and fixture evidence accepted in the merged baseline; this is not live verification.
- [ ] Revalidate real Feature 003 guard capabilities and provider/runtime gates against the approved execution revision.
- [ ] T019: obtain separate live authorization and observe normal completion, interruption and verified resume with no duplicate implementation or PR.
- [ ] Verify authenticated approval binding, durable publication evidence and final-label reconciliation, preserving unrelated labels and blocking uncertain writes.
- [ ] T020: obtain measurement authorization and record five consecutive eligible tasks including documentation, implementation, test-authoring and interrupted retry, with all administration intervals and exclusions; report whether the less-than-ten-minute administration target was achieved.
- [ ] Owner accepts independently reviewed evidence; keep unimplemented, fixture-verified and live-verified claims distinct.

M2 evidence may support T019 only when it satisfies T019's exact contract and
authorization. Link shared evidence rather than repeat a live exercise needlessly.

## Later milestones — not queued

- [ ] **M4 — Workspace vertical slice usable:** select and approve a bounded [Feature 001](../specs/001-workspace-lifecycle/tasks.md) slice; demonstrate the Docker workspace journey with real-host and browser/terminal evidence.
- [ ] **M5 — Execution cards visible:** obtain the owner-approved product scope and constitution amendment or scoped exception required by [proposed ADR 0006](adr/0006-optional-ai-execution.md), then approve a separate dashboard feature and its boundaries; deliver a read-only task/attempt/blocker/check/PR view first. These gates apply to that first read-only product view too.

Dates remain unset until the first live smoke test provides useful timing evidence.
