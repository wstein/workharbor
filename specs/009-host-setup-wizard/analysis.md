# Feature 009 consistency analysis

Status: author's read-only analysis of spec.md, plan.md, data-model.md, contracts and tasks.md at the authoring head; not an independent review
and not approval. Re-run `$speckit-analyze` before any task starts.

## Checks performed

| Check | Result |
| --- | --- |
| Every FR (001 to 025) has a task | FR-001: T008; FR-002, FR-004 to FR-008: T002; FR-003, FR-025: T003; FR-009, FR-011, FR-012: T003; FR-010, FR-022: T001; FR-013: T004; FR-014: T005; FR-015, FR-016: T007; FR-017, FR-020: T008; FR-018, FR-019, FR-021: T006; FR-023, FR-024: all, T009; |
| Every SC (001 to 014) has a named test | yes, see the plan evidence table |
| Exit codes use only ADR 0013 values | yes (64, 68, 70, 71, 72, 73, 74); the stable reason travels in `result.reason` because the output schema's `error` object is closed (AP-6) |
| Frozen contracts untouched in Stage 1 | yes; T009 proposes; T011 to T014 are the owner-gated exceptions |
| Docker VM and socket (D-3) | closed step-kind set, `TestNoDockerVMStep`, SC-006 |
| Owner decisions recorded | OD-1 to OD-5 confirmed 2026-10-20 (OD-2 amended); tasks name which OD they depend on |
| Amendments have an applying task | AP-1 to AP-5, AP-7, AP-9: T012; AP-6 and AP-9: T013 and T011; AP-8, DP-5: T014; T011 depends on T012 and T013 |
| Task dependencies acyclic | yes (table in tasks.md) |

## Findings

- **A-1 (high, owner)**: Feature 006 FR-009 and FR-026 cannot stay as written once the macOS wizard exists; handled as AP-1 to AP-3, applied by T012 before T011.
- **A-2 (high, owner)**: `doctor --fix` contradicts "doctor is read-only on every platform" and the doctor no-network wording of Feature 006 (AP-4, AP-6).
- **A-3 (medium)**: "Pinned" cannot be enforced for Homebrew core formulae; the spec states a minimum version and post-install verification. If the owner needs exact pins, OD-1 must change.
- **A-4 (medium)**: Opt-out persistence needs a host profile file (DP-1); without it `doctor --fix` would reinstall declined tools.
- **A-5 (info)**: OD-1 to OD-5 are confirmed 2026-10-20; OD-2 is amended to a small curated host catalogue with minimum versions only, no toolbox parity or sync.
- **A-5b (info)**: The initial catalogue is the owner's list of 13 entries (FR-010); minimum versions are measured on the owner's Mac (OG-1) and set in T001.
- **A-6 (medium)**: The output schema's `error` object is closed; this feature uses `result.reason`. Feature 008 proposes `error.refusal`; the two should converge (AP-6).
- **A-7 (low)**: Feature 008 OD-5 is resolved toward the wizard and doctor by DP-4; Feature 008 text is not edited here (T014).
- **A-8 (info)**: After the review rounds the owner cut the feature to the essentials (D-8): the design defends against mistakes and surprises on the owner's own Mac, the catalogue is trusted and privileged commands are reviewed per catalogue entry; rules and tests beyond that were removed.
