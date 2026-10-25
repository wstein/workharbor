# T038 source acceptance and wave reconciliation

Audited main: `9ec0ec2a4df9bfbfd2e7ea28b06489ca94ebc432`, 2026-10-25.
The owner confirmed that the T038 source is complete after the three split
PRs merged. This record consolidates their source and offline fixture evidence;
it does not claim an executed Docker lifecycle or full first-increment acceptance.
Tracker: [#263](https://github.com/wstein/workharbor/issues/263).

## Merged stage and promote evidence

| Slice | Merge on main | Reviewed source and Opus evidence | Detailed evidence |
| --- | --- | --- | --- |
| Source capture | [#265](https://github.com/wstein/workharbor/pull/265), `f9f03821ddcb701763903b46e9854fb6845c5493` | `0feaa0338ffcccde00a00efbebf26aa76eb6d09a`; [CLEAR review](https://github.com/wstein/workharbor/pull/265#pullrequestreview-5644555518) | [Capture record](t038-source-capture-20261025.md) |
| Stage library | [#266](https://github.com/wstein/workharbor/pull/266), `5ad72e8197bac3e61b75d0afb8bcfca5cfe324d6` | `34b5cf05d28ee7f9456ce3ca7c86e4a7a07a83e7`; [CLEAR review, CI pending when posted](https://github.com/wstein/workharbor/pull/266#pullrequestreview-5644811243) | [Stage record](t038-stage-library-20261025.md) |
| Promote and resource recovery | [#267](https://github.com/wstein/workharbor/pull/267), `9ec0ec2a4df9bfbfd2e7ea28b06489ca94ebc432` | `15df5c8b833466da923f249946744bcdf16d78d5`; [CLEAR review](https://github.com/wstein/workharbor/pull/267#pullrequestreview-5644948645) | [Promote record](t038-promote-library-20261025.md) |

Review SHAs identify the actual reviewed heads; merge SHAs may differ because
the owner used rebase merges. Subsequent hosted evidence resolves the reviews'
pending-CI notes: [#266 run 40224025307](https://github.com/wstein/workharbor/actions/runs/40224025307)
and [#267 run 40227349070](https://github.com/wstein/workharbor/actions/runs/40227349070)
are green. The latter reports 3589 whr Go tests and 251 control-plane ExUnit
tests passed, with all required IDs present; localdocker passed in 161.526 seconds.
These are historical exact-head results, not a new run of this docs PR.

## Scope and retained limits

- Capture uses the admitted source and isolated Git reader; stage binds trust,
  image/configuration, candidate home and health/smoke observations. The capture
  record preserves the owner's accepted unbounded object-store-walk residual.
- Promotion retains the original container until verification and permits only
  bounded resource recovery. AutoRemove and active restart policies refuse;
  unknown bind writability is treated as potentially writable. Shared data and
  terminated sessions are not rolled back. See the separate
  [recovery amendment](../../../specs/006-whr-cli/contracts/local-promote-resource-recovery.md).
- Existing listing/removal evidence remains in the [removal record](t038-removal.md)
  and [candidate cleanup guards](candidate-cleanup-guards.md). The accepted
  FR-067 volume/ownership/consent guards are not relaxed by the split work.
- Stage/promote library delivery is distinct from CLI registration, interactive
  composition and real official-CLI/Docker acceptance. The merged library records
  explicitly leave those boundaries open. This reconciliation does not tick T038
  or T034, alter the accepted execution profile, or resolve the promote record's
  proposed transfer budgets by inference from a merge.

## Wave-closing task updates

Only T036, T037 and T039 are checked in this closing PR, as requested in #263.
Their source and bounded acceptance evidence is delivered; this does not close
T025-T034 or establish owner-host acceptance.

| Task | Delivered evidence |
| --- | --- |
| T036 / FR-064 / SC-022 | [Agent block record](t036-agents-block-20261024.md); [#256](https://github.com/wstein/workharbor/pull/256), [Opus CLEAR at 4020afa2](https://github.com/wstein/workharbor/pull/256#pullrequestreview-5640795965). Refusal reasons, unsafe targets, permissions and unchanged-file metadata are covered. |
| T037 / FR-065 / SC-023 | [Settings record](t037-claude-settings-20261025.md); [#260](https://github.com/wstein/workharbor/pull/260), [Opus review at 5ead44e3](https://github.com/wstein/workharbor/pull/260#pullrequestreview-5642629502). Exact authored settings, preservation and per-path overwrite consent are covered. |
| T039 / FR-069 / SC-025 | [Drift contract](../../../specs/006-whr-cli/contracts/local-bundle-drift.md); [#237](https://github.com/wstein/workharbor/pull/237), [Opus CLEAR at 2ed58897](https://github.com/wstein/workharbor/pull/237#pullrequestreview-5639577276). Read-only fake-Docker comparison covers in-sync, drift and cannot-determine outcomes. The owner decided on 2026-10-24 that both latter outcomes retain exit 68, distinguished by report status. Mount aliases are refused by engineering design, not by an invented owner request. |

The original [first-increment matrix](first-increment.md) remains a historical
baseline. This record supersedes its T036-T039 source-status entries only;
T038 integration/host boundaries above and the T025-T034 gaps on #263 remain open.
