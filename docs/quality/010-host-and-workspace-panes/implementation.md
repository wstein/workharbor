# Feature 010 implementation evidence

## Accepted prerequisites (T001)

Owner `wstein` merged [PR #218](https://github.com/wstein/workharbor/pull/218)
on 2026-10-23 after exact-head CLEAR at
`056f09e2ca2f7124235686e73d3271901e8b27cb` and green required CI.
The merge tip is `73b68a667945d3c53b5126aa46dbbaf780cf992f`.
The owner then requested the next package in this session.

P1 starts at `767518e4d984f88c81313a691c2131659e8d567e`.
`git diff 056f09e2ca2f7124235686e73d3271901e8b27cb HEAD -- specs/010-host-and-workspace-panes`
is empty: spec, plan, contract, model, research, quickstart and tasks match the
approved artifacts byte for byte. The nine owner decisions and OQ-1/4 remain fixed.
OQ-11 and OQ-12 were already merged by PRs #216 and #217.

Prerequisite source boundaries exist:

- 006/T025: `apps/whr/internal/commands/{app,build,entries,table}.go`.
- 006/T027: `apps/whr/internal/doctor/{doctor,ports,socket}.go`.
- 006/T031: `apps/whr/internal/ptytest/`.

The feature was selected with `python3 scripts/select_feature.py
010-host-and-workspace-panes`. The Spec Kit prerequisite check with
`--json --require-tasks --include-tasks` found all expected artifacts.
P1 workspace was created through `stw workspace plan/apply --task
010-p1-local-discovery --owner codex`, plan `72b3171d3f7d715b211445dc6fe007ea`.
No checklist review markers were changed: actual owner merge/review evidence above
binds this package, while stale reviewer-owned markers remain intact.

## Package gates

P1 covers discovery only; 006/T038 lifecycle work stays open. One PR per package,
P1 → P2 → P3 → P4, each stopping for owner merge. SC-009 checkpoints A/B remain
owner-operated real-host gates; fake Docker and PTY tests cannot close them.

P1 red/green implementation and verification evidence: [local discovery](p1.md).
T002–T007 are complete through owner merge of PR #220, recorded below.
Later package and owner-host gates retain their separate acceptance boundaries.

## P2 approval and prerequisite binding

Owner `wstein` merged [P1 PR #220](https://github.com/wstein/workharbor/pull/220)
on 2026-10-23 at `5e4966a26fc0a84d82d8d9ed821f2e2d31f599d8`.
Its accepted head was `afccf59172c5d15390717689507b71cf4d07986a` (code/QA CLEAR,
required CI green). The complete merged tree is byte-identical to that reviewed
head. This closes T007's package merge gate and permits the already-authorized P2.

P2 starts from that exact main revision in a fresh stw workspace, task
`010-p2-local-shell`, owner `codex`, plan `c0d3be1d20f26688d355ee5a33ac95be`.
Feature selection/prerequisite checks pass; extension hooks remain empty.
Authoritative requirements and decisions are unchanged; only prior task completion
markers/evidence were added. Reviewer-owned quality checklist markers stay intact.

P2 scope is T008–T017 and preparing the SC-009 checkpoint A runbook. It stops at
its own owner merge; actual host execution/acceptance T018 remains owner-operated.

P2 implementation evidence: [local shell](p2.md). The prepared
[SC-009 base runbook/result template](sc009-base.md) is UNVERIFIED.
No real Docker, herdr or SSH operation has been performed by this agent.
