# Validation guide: Provider-aware workflows

Status: proposed guide, not successful execution evidence. New profile modules/fixtures/tests are not implemented.
All commands referring to them are future acceptance commands; do not dispatch on this document alone.

## Prerequisites

Owner-reviewed plan and generated bounded tasks; accepted tooling revision; Python 3.11+, Git, Make and a supported
local POSIX filesystem. On macOS select a supported interpreter explicitly: Apple Python 3.9 is unsuitable.
No provider key, tracker login, Symphony installation or network is required for offline acceptance.

## Preparation and current repository checks

```sh
python3 scripts/select_feature.py 005-provider-aware-orchestration
python3 -B scripts/check_repo.py
git diff --check
```

Use a pristine committed scratch checkout for `make check`; the checker rejects dirty and ignored local state.
Run `make check PYTHON=/absolute/path/to/supported/python3` there. Run matching required Linux CI for the exact
candidate. Failure, timeout, cancellation and missing tools must remain visible; no waived expected outcomes.
The planning delivery created no tasks.md; tasks have since been generated. For cross-artifact analysis, use
check-prerequisites.sh --json --require-spec --require-tasks --include-tasks without treating success as dispatch approval.

## Future offline acceptance

After implementation, use isolated synthetic repositories and reviewed fixture identities to run:

```sh
python3 -I -B scripts/test_provider_profiles.py -v
python3 -I -B scripts/test_profile_authority.py -v
python3 -I -B scripts/test_profile_state.py -v
python3 -I -B scripts/test_profile_guard.py -v
python3 -I -B scripts/test_profile_execution.py -v
```

These are proposed script names. Task generation must define collected test IDs, fixture inputs and exact commands
before execution; no test-authoring expected-red exception is granted here.

| Acceptance group | Required observation |
| --- | --- |
| SC-001 matrix | Required profile_eligible/task_eligible booleans; pure preview task_eligible=false; no eligible alias; three candidate families represented; permission/capability/grant independently visible; all invalid combinations block with no remote calls. |
| SC-002 parity | Normal, interrupted-handoff and authorized-rework scenarios through both routes; matching task guard decisions; retained single implementation/publication identity. |
| SC-003 inherited guards | Applicable source/index/history/mode/scope/prerequisite/review/acceptance negative cases remain rejected through both bridges. |
| SC-004 authority/recovery | Exact action/capability mappings, separately scoped inspection/safety decisions, expiry, revocation, clock rollback, changed evidence, competing claim, stale generation and uncertain transfer block and retain state. |
| SC-005 offline manual | Synthetic local completion succeeds without Symphony/tracker/network; no missing live capability is reported verified. |
| SC-006 boundaries | Proposed adapters remain ineligible, no product dependency or provider activation, Features 001–004 unchanged. |

Inject crashes before/after intent, guard entry, completion, commit binding, result persistence, transfer and publication.
Resume must reconcile existing records; after interrupted begin, discover the existing attempt rather than create a
second one. Specifically lose begin and rework responses after the guard transaction commits but before the
coordinator records the ID: lookup must adopt exactly one intent-matching new attempt. First begin must prove an empty
namespace under the canonical lock; missing-journal recovery must remain blocked. Rework must preserve the predecessor
binding/result and advance only to its confirmed linked successor. Cover an old matching-context
attempt, wrong predecessor/baseline, two candidates, missing/corrupt journal and unproven creator exclusivity; each
ambiguous/unavailable case blocks without repeating creation or mutating guard records. Local completed-awaiting-publication is an allowed intermediate outcome, not final handoff success.
Test same-owner route changes and reject owner changes unsupported by Feature 004's fixed ownership journal.

## Evidence and next gates

Record candidate/source/profile/evidence/grant digests, interpreter/platform, collected test IDs and exact outcomes,
raw exits and explicit offline provenance. Review evidence must bind the current source and content. Do not include
credentials or unredacted output. No profile grant follows from passing tests or owner plan approval.

Owner plan review → bounded task generation → cross-artifact analysis → implementation and independent review →
main-target PR. Real-provider adapters, actual authentication, cancellation/live recovery and runtime activation
need their separately scoped work and permissions; offline parity does not establish those capabilities.
