# 0005-development: Harness and Symphony outside the product

Date: 2026-10-11
Status: Accepted bootstrap policy

## Context and decision
Spec Kit structures requirements and convergence. Repository-owned checks enforce navigation, contracts and architectural rules. Symphony runs isolated coding checkouts and hands off to human review. It is installed separately, never embedded in WorkHarbor. The supplied workflow targets the pinned upstream Linear adapter; Forgejo/Gitea tracking requires a verified adapter later. Publishing, tracker enrollment, daemon startup and remote pushes are not part of bootstrap.

## Consequences
Changes to this decision require an owner-reviewed superseding ADR and updated contracts/specs.
