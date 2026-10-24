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

## P3 approval and prerequisite binding

Owner `wstein` merged [P2 PR #221](https://github.com/wstein/workharbor/pull/221)
at `7d3449b4913566e30be2711c60f56957f484c24b`; its accepted head was
`48422b095da4cb370dc6ce2f66b427863133112c`, with exact-head code/QA CLEAR and
[green CI](https://github.com/wstein/workharbor/actions/runs/40035395836).
This closes T017. The owner's subsequent instruction identifies P3 and P4 as
remaining work and authorizes their existing package scope. The authoritative
spec, plan, contract, model and research are byte-identical to the approved docs
head `056f09e2ca2f7124235686e73d3271901e8b27cb`; task/evidence updates leave
requirements and ordering intact.

P3 starts from the exact merge tip in the stw-created workspace for task
`whr010-p3-plain-20261023`, owner `codex`, plan `fa952bfa95b67c11d24470c9127c18f6`.
It covers T019–T022 only. [P3 evidence](p3.md) distinguishes configuration
acceptance from the absent remote session-creation consumer. P4 starts after P3's
owner merge; SC-009 T018/T030 stays UNVERIFIED and owner-operated.

## P4 approval and prerequisite binding

Owner merged P3 PR #222 at `efc0715046196f360674dba3f7789ca09b546fab`,
byte-identical to its reviewed head `e511b55d541a8e6acaf1f9a85f8c431f68930f65`.
This closes T022. [P4 evidence](p4.md) records the already-merged OQ-12 amendment,
authorized scope, independent optional checks and the prepared
[SC-009 plugin result template](sc009-plugin.md). All actual host observations
remain UNVERIFIED. P3's nonblocking review notes remain visible in its review;
F1's stale derived/default notes and spec reconciliation belong to T031, without
reopening any settled decision or blocking accepted P3 configuration scope.

## P4 acceptance and P3 follow-up

Owner merged [P4 PR #223](https://github.com/wstein/workharbor/pull/223)
on 2026-10-24 at `0409d9f14422b4763cbca7b9d55ecea3ffb19139`. Accepted head
`950c07ca8ee2c9a61a7ed9e7601d0ae2919c2440` has independent code/QA CLEAR,
[Opus delta CLEAR](https://github.com/wstein/workharbor/pull/223#pullrequestreview-5636039028)
and [green CI](https://github.com/wstein/workharbor/actions/runs/40048439416).
All 37 standalone optional plugin tests passed. This closes T029's publication,
review, CI and merge scope, independently of installation and host acceptance.

Owner also merged [P3 follow-up #224](https://github.com/wstein/workharbor/pull/224)
at `628bc4ded0f96ed0ffebc501eae833fe4e2d3e5f`, accepted head
`7ac1ba2d88aa457fdf727e7f13f7743ec16d207d`. It closes P3's remaining review
notes, records unchanged approval markers, and explicitly separates implementation
merges from approval of Proposed ADR 0015. See [P3 evidence](p3.md).

T018/T030 remain UNVERIFIED: the owner requested instructions for SC-009 rather
than supplying observed A/B results. The prepared base/plugin runbooks remain
unchanged; no individual key, detach, resize, label, tint or SSH-reconnect result
is inferred from merge, CI or the owner's earlier “done”. T031 stays open until
the applicable owner observations are accepted; the absent remote SC-007 consumer
also remains a distinct limitation. No whole-feature completion is claimed.
