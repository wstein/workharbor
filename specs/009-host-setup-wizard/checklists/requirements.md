# Specification Quality Checklist: Host setup wizard and doctor repair

**Purpose**: Validate specification completeness and quality before planning review.
**Created**: 2026-10-20
**Feature**: [spec.md](../spec.md)
**Marker semantics**: Checked items mean requirements quality was reviewed; they do not mean implementation or live verification, review CLEAR
or owner approval.

## Content Quality

- [x] Wizard, doctor repair, installation, elevation, dotfiles, git move, protocol, catalogue and staging are covered by numbered FRs.
- [x] Requirements use MUST/SHOULD and name a reason code or exit code where behavior is refused.
- [x] Scope is the owner's threat model (D-8): mistakes and surprises, not attackers; no marketing language.
- [x] Boundaries stated: Docker VM and socket (D-3), Linux enrollment, login shell, other package managers, no toolbox mirror.

## Requirement Completeness

- [x] No unresolved clarification markers; OD-1 to OD-5 are confirmed (2026-10-20) and DP-1 to DP-5 are open in the plan.
- [x] Every FR group maps to an acceptance test and evidence location (plan: requirements to evidence).
- [x] Success criteria SC-001 to SC-014 are testable offline with fakes.
- [x] Initial catalogue (13 entries, defaults, config markers) recorded in FR-010 and tested by `TestInitialCatalogueEntries`.
- [x] Edge cases listed: other version, brew missing, offline, decline, partial failure, unattended, unsupported OS, rerun, dry run, JSON, root start, lock.
- [x] Owner requirements of 2026-10-20 recorded with date (D-1 to D-8).

## Feature Readiness

- [x] Tasks carry FR/SC trace, named failing tests first, exit codes, dependencies and owner gates.
- [x] Feature 006 and 008 overlap checked (T015, T027, FR-009, FR-026, FR-060, FR-063; 008 T005, OD-5): amendments are proposals only (AP-1 to AP-9) applied by gated T012 to T014.
- [x] Frozen contracts, ADRs, Feature 006 and 008 text and the container-runtime page are not edited.
- [x] Host mutation and real runs are owner gates; tests use fakes and a temporary HOME.

## Review state (no approval claimed)

- [ ] Independent review CLEAR for the exact head of this package.
- [ ] Explicit owner approval of that head, evidenced by a tracker or PR link.
- [x] Owner confirmation of OD-1 to OD-5 (2026-10-20, OD-2 amended).
- [ ] Owner decision on DP-1 to DP-5 (open).
- [ ] Owner approval of AP-1 to AP-9 (open; each is its own gate).
