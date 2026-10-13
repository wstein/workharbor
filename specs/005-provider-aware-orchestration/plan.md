# Implementation Plan: Optional provider-aware development orchestration

**Branch**: `docs/005-provider-aware-plan` | **Date**: 2026-10-13 | **Spec**: [spec.md](spec.md)
**Input**: Owner-approved Feature 005 specification; accepted content in main at
`d646bd565daff85b03519ac1873b534e9c0c2288`. Owner approved the specification and requested planning in this session.
**Status**: Accepted design; owner requested task generation on 2026-10-13 after PR #60 merged.
The planning delivery contained no implementation, tasks or activation.

## Summary

Add a development-only profile assessor and route coordinator around existing task guards. Keep provider permission,
technical capability and project-owner opt-in independent and bind them to exact effective execution identities.
Manual local work uses Feature 003 directly; Symphony orchestration delegates to Feature 004's stricter gates.
Neither route bypasses task approval, review, recovery or publication controls. No real-provider adapter is built.

## Technical Context

**Language/Version**: Python 3.11+ standard library; use an explicit supported interpreter in validation.
**Primary Dependencies**: Git, existing Feature 003 CLI and Feature 004 host-injected interfaces; POSIX file locks.
No new Python packages, product dependencies or agent SDKs.
**Storage**: New owner-controlled local SQLite ledger and per-task lock files outside tracked workspace data.
Reuse existing guard/handoff stores through their interfaces; never edit their schemas or retained ownership records.
**Testing**: unittest, synthetic Git repositories, injected time/authority/runtime/claim ports and fault injection.
Real-provider verification remains a separate evidence class and separately approved activity.
**Target Platform**: Local Linux and macOS POSIX hosts with supported local filesystems; no shared-network-filesystem
or multi-host lock guarantee. Cross-host execution requires a demonstrated external fencing authority or blocks.
**Project Type**: Repository development CLI and trusted host API, outside product applications.
**Performance Goals**: Offline assessment makes zero provider/tracker calls; existing task deadlines remain unchanged.
Bound each host observation by its approved finite deadline; performance claims require measurement, not estimates.
**Constraints**: Profiles are inert data; no arbitrary imports, commands or credential discovery from profile/issue text.
No silent fallback. Missing authorities/capabilities block the affected action. Features 001–004 stay unchanged.
**Scale/Scope**: One project task claim per repository + feature + task; three candidate agent families initially
assessed, not automatically supported. Local manual and synthetic orchestrated scenarios prove guard parity.

## Constitution Check

Pre-research and post-design gates both pass for the proposed scope:

| Principle | Design evidence |
| --- | --- |
| I: product scope | New modules remain under development scripts; Symphony optional, no product integration. |
| II: explicit capability | Exact compatibility entries and action-specific evidence; unknown combinations block. |
| III: reproducibility | Bind source, profile, evidence and approval digests; credentials/state excluded from Git. |
| IV: identity/trust | Trusted host supplies owner authority and observations; repository text cannot create authority. |
| V: durable operations | Fenced claim, durable transition intent and recovery; uncertain actions stay incomplete. |
| VI: repository authority | Spec approval recorded; plan proposed; accepted Features 001–004 preserved. |
| VII: executable feedback | Shared negative suites, offline parity and fault tests; no weakening expected outcomes. |
| VIII: human control | Separate profile/task/rework/publication/live approvals; one executor; no automatic fallback. |

No constitutional exception is proposed. A product AI integration would require separate governance and scope.

## Project Structure

### Documentation (this feature)

```text
specs/005-provider-aware-orchestration/
├── spec.md
├── checklists/requirements.md
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
└── contracts/cli-and-host.md
```

Task generation follows owner plan review; this planning delivery creates no tasks.md.

### Source Code (proposed future files)

```text
scripts/provider_profiles.py       # strict assessment and digest rules
scripts/profile_authority.py       # trusted owner/policy/capability observations
scripts/profile_state.py           # durable claims, fencing and transition recovery
scripts/profile_execution.py       # CLI and guarded route coordination
scripts/profile_guard.py           # revision-pinned Feature 003 subprocess bridge
scripts/test_provider_profiles.py
scripts/test_profile_authority.py
scripts/test_profile_state.py
scripts/test_profile_execution.py
scripts/test_profile_guard.py
```

**Structure Decision**: Add modules and tests only; consume existing Feature 003/004 interfaces without modifying
those modules or their specifications. Existing generic CI discovery in scripts/test_*.py collects new regressions.
No per-task exemption or acceptance-policy change is planned.

## Phase 0: Research decisions

[research.md](research.md) resolves the manual-readiness mismatch, authority boundary, durability/ownership and
provider-evidence questions. New provider policy verdicts are deliberately not inferred from historical research.

## Phase 1: Design and integration

1. Implement strict profile/evidence validation and pure assessment; initially enumerate candidate profiles as
   unknown/unverified/disabled unless independently supplied evidence establishes otherwise.
2. Add trusted host authority ports and immutable authorization bindings; file JSON alone never authenticates an owner.
3. Add a bounded ledger with transaction, lock and recovery tests before any coordinator side effects.
4. Add the Feature 003 bridge preserving its JSON, exit categories, acceptance commands and immutable attempt identity.
   Its internal read-only creation lookup correlates lost begin/rework responses against a durable pre-call intent and
   validated pre-existing attempt IDs. Use the existing locked store plus a source/schema-pinned read-only enumeration
   adapter; no new Feature 003 command, schema or lifecycle mutation is permitted. Ambiguous/missing evidence blocks.
   A Feature 004 GuardPort bridge may translate verified observations, but cannot synthesize unavailable capabilities.
5. Coordinate manual actions independently of Symphony/tracker readiness. Delegate orchestrated eligibility/handoff to
   existing Feature 004 interfaces only when host-injected guard/runtime/tracker capabilities are actually present.
6. Validate three parity scenarios and applicable existing negative cases, revocation, drift, crashes and competing
   claims. A same-owner route transfer may be supported; a Feature 004 owner change remains blocked because its store
   provides no ownership-transfer interface. Never rewrite that journal to make a transfer appear valid.
7. Document usable manual operator steps, incomplete external handoff and future adapter boundaries. Keep real-provider
   invocation and live dispatch unavailable in this feature's reference host until separately approved adapters exist.

Contracts and state rules are in [data-model.md](data-model.md) and [contracts/cli-and-host.md](contracts/cli-and-host.md).

## Verification and handoff

Map FR-001–FR-004 to assessor/evidence cases; FR-005–FR-007 to authority and revocation;
FR-008–FR-010 to bridge/parity/claim/recovery; FR-011 to all boundaries and secret controls;
FR-012 to proposed-adapter rejection; FR-013 to protected-tree/product boundary checks.
Use SC-001–SC-006 as the acceptance groups; require every defined negative case and all three parity scenarios.
Before implementation, task generation must enumerate exact allowed paths, test IDs/expected outcomes and commands.
No fixed new task matrix or execution grant is implied by this design.

Run repository checks on a pristine committed candidate, with explicit supported Python locally and matching required
Linux CI. Report unsupported interpreter, incomplete/canceled checks and raw failures honestly. Documentation checks
alone are not implementation or live-provider proof. Independent exact-head review precedes an explicit main-target PR;
owner plan approval, merge, implementation dispatch and live activation remain separate gates.
