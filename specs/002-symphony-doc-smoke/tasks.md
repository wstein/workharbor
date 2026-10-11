# Feature 002 tasks

Status: proposed; not approved for dispatch. One task only.

## Phase 1: US1 — Document a bounded Symphony smoke test
Goal: provide an operator procedure for normal handoff and interrupted-handoff retry.
Independent acceptance: the guide is complete and linked, repository checks pass,
and the change stays within the plan's allowlist. No live result is implied.

- [ ] T001 [US1] Create `docs/development/symphony-smoke-test.md` covering prerequisites, normal completion, interrupted handoff, evidence-verified handoff-only retry, expected outcomes, evidence capture and foreground-service stop; add its link to `docs/development/symphony.md`; pass `make check` and `git diff --check`, inspect the allowed diff and record results before marking this checkbox complete (FR-001–FR-004; SC-001–SC-003).

## Dependencies and execution
No prerequisite task IDs. Owner spec/plan review and the plan's runtime/single-task
gates are mandatory before dispatch. Execute T001 only; no parallel tasks exist.
After completion a retry may finish handoff only with matching commits and fresh
passing checks. Missing evidence blocks; rework requires explicit owner approval.
