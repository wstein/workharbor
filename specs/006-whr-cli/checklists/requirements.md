# Specification Quality Checklist: `whr` product command line

**Purpose**: Validate specification completeness and quality before planning.
**Created**: 2026-10-13
**Feature**: [spec.md](../spec.md)
**Marker semantics**: Checked items mean requirements quality was reviewed; they do not mean implementation or
live verification.

## Content Quality

- [x] No implementation details prescribe languages, frameworks, interfaces or storage.
- [x] Focused on user value and business needs.
- [x] Written for stakeholders, with command names used only as user-visible vocabulary.
- [x] All mandatory sections completed.

## Requirement Completeness

- [x] No unresolved clarification markers remain.
- [x] Requirements are testable and unambiguous; exit-code numbers are deferred to an ADR by owner decision.
- [x] Success criteria are measurable.
- [x] Success criteria describe outcomes without prescribing implementation technology.
- [x] All acceptance scenarios are defined.
- [x] Edge cases are identified.
- [x] Scope is clearly bounded.
- [x] Dependencies and assumptions identified.

## Feature Readiness

- [x] All functional requirements have clear acceptance criteria.
- [x] User scenarios cover primary flows.
- [x] Feature meets measurable outcomes defined in Success Criteria.
- [x] No implementation details leak into specification.

## Notes

- No markers remain. Owner decisions H8, H9, H10:a, H11:a, H14:a, H15:b (superseding H12:c), H16:b, H17:a,
  H18:a, H23:a, H24:a and wstein/workharbor#223 (closed) are recorded in the spec.
- H13:a (approved for planning) covers this revised text only once independent re-review of its exact head is
  CLEAR; until then the revision is pending re-review.
- Second review round applied: owner API hosted by the control plane over TLS (FR-033, FR-036–FR-038); ADR 0001
  amendment prerequisite (#67); needs-human and incompatible-version categories (FR-018, FR-013, FR-029);
  `whr ssh` exit status and filtering scope, client-config generation (FR-008a, FR-019, FR-022a); credential
  permission checks split between `auth status` and `doctor` (FR-021); `console --status` acceptance (SC-010).
- Review items applied: read-only dashboard enforced by API scope (FR-033, SC-008); resumable offboarding with
  confirmed revocation (FR-011, SC-005); console persistence separate from attachments (FR-007a, SC-010);
  generated idempotency keys, waiting and operation retrieval (FR-003–FR-003b, SC-011); typed JSON stream events
  (FR-015); browser boundary of the owner API (FR-036–FR-038, SC-009); first-use authentication (FR-035); separate
  client and host platform matrices (FR-026); remote exit 0–255 distinguished from CLI failures (FR-019);
  rebuild acceptance (FR-005a).
- Implementation prerequisites: documentation reconciliation (#66, #68), ADR 0001 amendment (#67), exit-code ADR
  (#70), session interface contract (#71) and the Feature 001 terminal-cleanup reconciliation for the console
  model.

## Resource-selection revision validation (2026-10-16)

- [x] US1a covers explicit/default limits, invalid/unsupported values, lost-response
  identity and restart/rebuild retention; FR-039–FR-042 define units and precedence.
- [x] SC-013 separates desired values from real-host enforcement evidence.
- [x] Existing accepted API/provider contracts and implementation approvals are not
  silently expanded; affected contracts and plans require reconciliation/review.
- [x] No unresolved clarification markers; host ranges/defaults are explicit inputs,
  not invented universal capacities or guarantees.

These marks record specification quality only. Revised scope awaits human spec
review before planning; earlier H13:a does not approve this addition.
