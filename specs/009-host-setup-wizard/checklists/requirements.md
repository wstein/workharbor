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

- [x] No unresolved clarification markers; OD-1 to OD-5 are confirmed (2026-10-20) and DP-1 to DP-5 and no-op protocol bookkeeping are confirmed by H38; AP-6 reason/result placement remains open.
- [x] Every FR group maps to an acceptance test and evidence location (plan: requirements to evidence).
- [x] Success criteria SC-001 to SC-014 are testable offline with fakes.
- [x] Initial catalogue (13 entries, defaults, config markers) recorded in FR-010; production `TestInitialCatalogueEntries` and H40's selected minimum-version snapshot belong to the separate catalogue integration, distinct from this canonical slice and the TEST ONLY loader fixture.
- [x] Edge cases listed: other version, brew missing, offline, decline, partial failure, unattended, unsupported OS, rerun, dry run, JSON, root start, lock.
- [x] Owner requirements of 2026-10-20 recorded with date (D-1 to D-8).

## Feature Readiness

- [x] Tasks carry FR/SC trace, named failing tests first, exit codes and the decision-consumer matrix; H37 records the manual cross-feature guard exception and local-CLEAR-source-parent rule without completing acceptance.
- [x] Feature 006 and 008 overlap checked (T015, T027, FR-009, FR-026, FR-060, FR-063; 008 T005, OD-5): Feature006 AP-1 to AP-5 and FR-063/AP-9 text applied in `1071dd6198854933f124fad4c5ec8d1a02c8fdf9`, AP-7 in `7b146ef28ea038eb2ad1d58d67f013a7f75cccd5`; Feature008 AP-8/OD-5 text applied by owner-approved PR #164 (`9d297da28887ceee35ed6e5669647f07ffb4f220`). AP-6 choice, ADR/CLI application, task acceptance and implementation remain separate gates.
- [x] Frozen contracts, ADRs, Feature 006 and 008 text and the container-runtime page are not edited.
- [x] Host mutation and real runs are owner gates; tests use fakes and a temporary HOME.

## Review state (no approval claimed)

- [ ] Independent review CLEAR for the exact head of this package.
- [ ] Explicit owner approval of that head, evidenced by a tracker or PR link.
- [x] Owner confirmation of OD-1 to OD-5 (2026-10-20, OD-2 amended).
- [ ] Verify this exact package against confirmed H38 DP-1 to DP-5 and no-op bookkeeping; source-consistency review remains pending.
- [ ] Owner approval of AP-1 to AP-9 (open; each is its own gate), including the unselected AP-6 reason/result placement.
