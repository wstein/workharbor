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
