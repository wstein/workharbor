# 0005-development: Harness and Symphony outside the product

Date: 2026-10-11
Status: Accepted bootstrap policy

## Context and decision
Spec Kit structures requirements and convergence. Repository-owned checks enforce navigation, contracts and architectural rules. Symphony runs isolated coding checkouts and hands off to human review. It is installed separately, never embedded in WorkHarbor. The supplied workflow targets the GitHub Issues adapter included in the pinned upstream runtime. Open issues require `workharbor-ready` for dispatch; handoff finishes commits and tracker evidence before a final single API operation replaces the labels, preserving unrelated labels, removing `workharbor-ready` and adding `human-review` while leaving the issue open. This ordering avoids reconciliation interrupting unfinished handoff work. No GitHub Projects board is required. Forgejo/Gitea tracking requires a verified adapter later. Publishing, tracker enrollment, daemon startup and remote pushes are not part of bootstrap.

## Bounded implementation entry
Single-task Symphony issues use the local guard described in
[the single-task guide](../development/single-task.md), rather than the upstream
phase-oriented implementation skill. The guard records one pending task, an exact
file allowlist and approved acceptance argv; it verifies the baseline diff and
preserves other task text/checkmarks before completing only the selected task.
Human approvals and semantic review remain required. The guard is not a sandbox,
tracker authorization mechanism or live-runtime evidence.

## Consequences
Changes to this decision require an owner-reviewed superseding ADR and updated contracts/specs.
