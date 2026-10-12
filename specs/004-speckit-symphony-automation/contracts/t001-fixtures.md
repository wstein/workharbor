# T001 preparation: inert task fixtures

Status: proposed for independent review and explicit owner approval. This document
authorizes no implementation. Proposed execution base:
`5a2dda81f456ab485d46bc19374c4229e02487bd` (remote `main` verified locally).
Changed execution inputs require review; never silently refresh this pin.

## Proposed implementation scope

One local T001 deliverable, followed by human handoff; exactly five files:

- `tests/fixtures/task-automation/documentation.json`
- `tests/fixtures/task-automation/test-authoring.json`
- `tests/fixtures/task-automation/implementation.json`
- `tests/fixtures/task-automation/README.md`
- `scripts/test_task_fixtures.py`

The JSON files are synthetic inputs and case/evidence manifests, not validated
TaskPackages or a new package schema. Use an explicitly fixture-only shape with a
kind, unique synthetic task ID, null issue, synthetic scope, inert prerequisite
references, named cases/expected raw outcomes and absent authority evidence.
Documentation has one passing case/raw exit 0; test-authoring has two cases
(one pass, one named intended assertion)/raw exit 1; its implementation successor
has the same two case IDs, both pass/raw exit 0. The successor explicitly references
the test-authoring fixture. These are declared examples, never observed test runs
or approved red exceptions. No fixture matrix is executed by T001.

All inputs remain `execution_eligible: false`; authenticated approval, independent
review and live observation are absent/null. Prerequisite references record unmet
or unavailable evidence without running it. Scope refers only to disposable
synthetic artifacts, never actual feature/task files. No credentials, service
addresses, executable task prose or real issue/owner identifiers enter fixtures.
The README records provenance below, manifest inventories, usage and these limits.
No T002/T005 validator, canonical package digest, guard schema or outcome classifier
is implemented here; future tasks consume or adapt these inert inputs.

## Named acceptance: 12 new green tests

`scripts/test_task_fixtures.py` exposes `TaskFixtureTests` with these exact method
names. Checks inspect fixture content using standard-library local JSON loading;
they do not implement future package validation.

| Method | Required observation |
| --- | --- |
| `test_documentation_shape` | Documentation kind, required fixture fields, one named pass and declared raw exit 0 |
| `test_test_authoring_shape` | Test-authoring kind, two unique named cases, one bounded intended assertion and declared raw exit 1 |
| `test_implementation_shape` | Implementation kind, two named passes and declared raw exit 0 |
| `test_unique_synthetic_task_ids` | Exactly three distinct fixture task IDs and null issues |
| `test_successor_preserves_case_inventory` | Explicit predecessor link; identical two-case IDs; successor replaces intended assertion with pass |
| `test_authority_evidence_absent` | No authenticated approval, review or live evidence; every fixture remains ineligible |
| `test_prerequisites_are_unmet_data` | Explicit unavailable/unmet prerequisite evidence; no claimed guard capability |
| `test_scope_is_synthetic` | All declared artifact scopes remain disposable synthetic paths, with no real feature/task mutations |
| `test_readme_records_source_inventory` | README contains exact source identities, baseline counts and five retained IDs below |
| `test_loading_is_read_only` | Loading all three inputs preserves their bytes and creates no repository artifacts |
| `test_loading_never_runs_prerequisites` | Loading succeeds with subprocess/process-launch entry points replaced by fail-on-call sentinels; zero calls |
| `test_loading_never_uses_network` | Loading succeeds with socket connection/address-resolution entry points replaced by fail-on-call sentinels; zero calls |

The last two cases demonstrate inert fixture loading under explicit denial; they
prove neither host isolation nor any future CLI's behavior. No services, network,
real prerequisites or guard execution are needed by this suite. Support both
direct isolated script execution and normal discovery with stable module IDs
`test_task_fixtures.TaskFixtureTests.<method>`; use standard-library imports and
paths derived from the script location, without relying on ambient `PYTHONPATH`.

## Actual source and baseline inventory

Every Git blob below is at the pinned execution base. The fixture README must
preserve these identities as provenance, not approval authentication:

| Input | Git blob |
| --- | --- |
| `specs/004-speckit-symphony-automation/spec.md` | `4bf3f49ed1cb3684f3cb54493ebfbbebff5ddef4` |
| `specs/004-speckit-symphony-automation/plan.md` | `123381f0a4546cad6dfab78c2321fa78a55ec243` |
| `specs/004-speckit-symphony-automation/tasks.md` | `62025b81219df310f50b03033a4ecd5366090dc7` |
| `scripts/check_ci.py` | `75a245de1adcbacedc7eebf634b06b35b3b3a635` |
| `scripts/prepare_publication.py` | `3a16da54dcbeac431955efc879d88912e2bed9d5` |
| `scripts/approval_binding.py` | `caaa7267d4cfb2d82f20a3029a57f738f02304ac` |
| `scripts/single_task.py` | `4db2fec60dbc2b99b72ce2c5776eaabf8ac4b6f6` |
| `scripts/single_task_state.py` | `5a44b62723cb98568763331374d957e18f873133` |
| `docs/development/ci-acceptance.json` | `b58f59f3b19c5a0d8e76e4c12f0d916dd14686ef` |

Reviewed source references are specification
`8f4debe5cd2ee2770f5963d292a9eb60af6270d7`, plan
`20c17b3ac90a232643d7c3998498465a7008318a` and accepted A-tools/CI
`8300295e47dc4b5b86da7cee7f0da99f8cc3bb70`. The plan's historical proposed
status is retained; bounded contract approval is not full-plan or live authority.
Guard code exists, but the real context/comparator/execution capabilities needed
by later tasks remain unavailable; legacy/state tests do not demonstrate them.

Baseline minimum inventory: root tests 41 (`test_harness` 5, `test_single_task` 22,
`test_single_task_state` 10, `test_symphony` 4); scripts 68
(`test_approval_binding` 25, `test_check_ci` 29, `test_prepare_publication` 14);
direct classes `verify_t003_state.StatePrimitiveTests` 12 and
`verify_t004_cli.CliBoundaryTests` 12. Total 133: 128 pass, five retained raw
assertion failures, zero errors. Retained IDs share prefix
`test_single_task_state.SingleTaskStateTests.` and are `test_one_lock_owner`,
`test_lock_released_after_exit`, `test_transaction_rollback`,
`test_append_only_events`, `test_immutable_context`. Their exact messages/reasons/
removal conditions remain in the unchanged reviewed manifest.

## Checks and approval boundary

Proposed future T001 check argv, from a clean committed implementation checkout
with supported Python 3.11+ (`python3` must resolve to that interpreter):

```sh
python3 -I -B scripts/test_task_fixtures.py -v
python3 -B -m unittest discover -s scripts -p test_task_fixtures.py -v
make check PYTHON=python3
```

Both focused forms must collect exactly 12 and pass. Generic CI automatically
discovers these new green tests: expected aggregate 145, 140 pass/five unchanged
retained failures/zero errors, qualified exit 0. No CI manifest/policy change.
Canonical `make check` reports its separate raw `make check-raw`: still 41 cases,
36 pass/five failures and Make exit 2. Never call those raw checks green.

Owner approval must explicitly name this contract revision, execution base, the
five paths, checks and one-task local implementation/human handoff. Marking only
T001 complete in `tasks.md` is an additional change allowed solely if explicitly
included in that eventual approval and supported by fresh acceptance; otherwise
return evidence with the checkbox unchanged. No other tasks, issue/label/dispatch,
publication, runtime activation, prerequisite execution, network or guard changes
are authorized. This preparation changes this document only and proves no live
operation or Feature 004 completion.
