# Feature 009 consistency analysis

Status: author's read-only analysis of spec.md, plan.md, data-model.md, contracts and tasks.md at the authoring head; not an independent review
and not approval. Re-run `$speckit-analyze` before any task starts.

## Checks performed

| Check | Result |
| --- | --- |
| Every FR (001 to 025) has a task | FR-001: T008; FR-002, FR-004 to FR-008: T002; FR-003, FR-025: T003; FR-009, FR-011, FR-012: T003; FR-010, FR-022: T001; FR-013: T004; FR-014: T005; FR-015, FR-016: T007; FR-017, FR-020: T008; FR-018, FR-019, FR-021: T006; FR-023, FR-024: all, T009; |
| Every SC (001 to 014) has a named test | yes, see the plan evidence table |
| Exit codes use only ADR 0013 values | yes (64, 68, 70, 71, 72, 73, 74); the stable envelope-reason location remains the unselected AP-6 choice; per-step reasons stay explicit |
| Frozen contracts untouched in Stage 1 | yes; T009 proposes; T011 to T014 are the owner-gated exceptions |
| Docker VM and socket (D-3) | closed step-kind set, `TestNoDockerVMStep`, SC-006 |
| Owner decisions recorded | OD-1 to OD-5 confirmed 2026-10-20 (OD-2 amended); H38 additionally confirms DP-1 to DP-5 and no-op bookkeeping; the plan matrix names every consumer |
| Amendments have an applying task | AP-1 to AP-5, AP-7, AP-9: T012; AP-6 and AP-9: T013 and T011; AP-8, DP-5: T014; T011 registration depends on approved/merged T012, T013 and T014 plus the AP-6 reason/result choice |
| Task dependencies acyclic | yes (table in tasks.md) |

## Findings

- **A-1 (high, owner)**: The former Feature006 FR-009/FR-026 contradiction is resolved in text: AP-1 to AP-3 were applied in `1071dd6198854933f124fad4c5ec8d1a02c8fdf9`. T012 acceptance and T011 registration remain separate.
- **A-2 (high, owner)**: The former Feature006 doctor read-only/no-network wording conflict is resolved by AP-4 in `1071dd6198854933f124fad4c5ec8d1a02c8fdf9` (also AP-5 and the FR-063 portion of AP-9); AP-7 is applied in `7b146ef28ea038eb2ad1d58d67f013a7f75cccd5`. AP-6 CLI/result changes and T013 ADR application remain gated.
- **A-3 (medium)**: "Pinned" cannot be enforced for Homebrew core formulae; the spec states a minimum version and post-install verification. If the owner needs exact pins, OD-1 must change.
- **A-4 (medium)**: Opt-out persistence uses the H38-confirmed host profile file (DP-1); without it `doctor --fix` would reinstall declined tools.
- **A-5 (info)**: OD-1 to OD-5 are confirmed 2026-10-20; OD-2 is amended to a small curated host catalogue with minimum versions only, no toolbox parity or sync.
- **A-5b (info)**: The initial catalogue is the owner's list of 13 entries (FR-010); minimum versions are measured on the owner's Mac (OG-1) and set in T001.
- **A-6 (medium)**: The output schema's `error` object is closed; AP-6 must choose result.reason versus the Feature008 optional error.refusal proposal. H38 does not select that choice; only affected result/schema/registration remains blocked.
- **A-7 (low)**: Feature008 OD-5/AP-8 text implements H38 DP-4/DP-5 through owner-approved PR #164 (`9d297da28887ceee35ed6e5669647f07ffb4f220`); it is not edited here. Remaining T014 acceptance, T011 registration and native implementation/acceptance are not completed by that text change.
- **A-8 (info)**: After the review rounds the owner cut the feature to the essentials (D-8): the design defends against mistakes and surprises on the owner's own Mac, the catalogue is trusted and privileged commands are reviewed per catalogue entry; rules and tests beyond that were removed.


## Canonical correction pass after H37/H38

The earlier independent ce3cfc83 full-design review was NOT CLEAR and its history
is retained. This source pass addresses its finite findings; it is not a fresh
independent CLEAR, implementation acceptance or task completion.

| Finding | Source correction / remaining evidence |
| --- | --- |
| F1 strict entry incompatibility | H37 manual source/per-WI evidence and exact CLEAR local prerequisite parents; parser/table and external dependencies remain honest, durable guard extension remains separate |
| F2 git value disclosure | FR-014/SC-007, wizard-flow and T005 require structural keys/redacted paths, never content/value diff; engine canary tests remain pending |
| F3 no-op versus protocols | H38 selects confirmed-run logging; FR-021/SC-008/T006 compare managed installs/config/profile/backups only and retain protocol/lock bookkeeping |
| F4 omitted DP consumers | H38 confirms all five; plan matrix and affected tasks trace consumers; AP-6 is separate |
| F5 registration ordering | T011 now requires approved/merged T014 as well as T012/T013; no owner application gate waived |
| F6 empty audit payloads | Protocol schema discriminates all six record kinds; explicit positive/negative corpus tests shape/enums/bounds only; sequence/secrets/filesystem durability remain T006 evidence |
| F7 successful skips became partial failure | Refusal rules/FR-020/T008 distinguish successful skips from incomplete selected work after mutation |
| F8 reserved git-config unknown | Wizard/refusal contracts accept reserved git-config without catalogue entry14; router tests remain T008 |
| F9 wrong check cwd | Quickstart puts Go commands in a subshell; subsequent frozen-path checks remain root-relative |
