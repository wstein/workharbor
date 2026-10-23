# Specification Quality Checklist: Host and workspace panes in one host session

**Purpose**: Validate specification completeness and quality before independent review.
**Created**: 2026-10-23
**Feature**: [spec.md](../spec.md)
**Marker semantics**: Checked items mean requirements quality was reviewed; they do not mean implementation or live verification,
review CLEAR or owner approval.

## Content Quality

- [x] The two pane kinds, local shell mode, console relation, trust-boundary relaxation, output filtering and optional plugin are
  covered by numbered FRs (FR-001 to FR-020).
- [x] Every refusal names an ADR 0013 exit code (FR-009); no new code is introduced.
- [x] Boundaries and out-of-scope items are stated (cloud, agent queues, VNC, colour, Windows, host-shell mode, plugin code).
- [x] Product rules are preserved: interactive and human-initiated, no agent launch or wrapping, no credential relay (FR-006).

## Requirement Completeness

- [x] No unresolved clarification markers; each former open question carries a recorded owner or foreman decision (Decisions).
- [x] Verified facts and unverified claims are separated, with the sources (repository revision, herdr 0.9.3 help, plugin docs).
- [x] Success criteria SC-001 to SC-008 and SC-010 are testable offline with a fake daemon and a fake `docker`; SC-009 is a real-host owner gate,
  unverified until run.
- [x] Edge cases listed: stopped, paused, ambiguous, hostile names, missing shell, no socket access, rebuilt workspace, resize.
- [x] The interim shell function is marked non-normative.

## Feature Readiness

- [x] Feature 006 amendments are listed in the spec and applied in a separate commit; ADR 0015 is added and ADR 0012 points to it.
- [x] Contracts, schema, vectors, command table and code that still say `herdr` are listed as follow-up work (FR-020), not edited.
- [ ] Independent review CLEAR for the exact head.
- [ ] Explicit owner approval of the exact head, evidenced by a tracker or PR link.
- [x] Owner decisions on OQ-2, OQ-5, OQ-6, OQ-7, OQ-8, OQ-9, OQ-10 and OQ-11 recorded 2026-10-23 (OQ-1 and OQ-4 are foreman decisions; OQ-3 is withdrawn).
