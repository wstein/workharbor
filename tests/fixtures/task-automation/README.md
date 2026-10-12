# Inert task automation inputs

T001 supplies three synthetic JSON inputs, not validated TaskPackages or a package
schema. `fixture_schema: 1` identifies this fixture-only shape. All scopes describe
disposable `synthetic/` artifacts; null issues and absent authority/evidence prevent
any claim of execution eligibility. Prerequisites are unmet/unavailable data.

| Input | Declared case inventory | Declared raw exit |
| --- | --- | --- |
| `documentation.json` | `fixture.documentation.guide`: pass | 0 |
| `test-authoring.json` | `fixture.behavior.input`: pass; `fixture.behavior.pending`: intended assertion `AssertionError: synthetic pending behavior` | 1 |
| `implementation.json` | Same two case IDs, both pass; predecessor `test-authoring.json` | 0 |

These outcomes are examples, not observed runs or approved red exceptions. T001
never executes the case manifests, prerequisites, guards, checks described by
task data or network calls. The loader simply reads local JSON. No secrets,
credentials, service endpoints, real issues or owner identities are needed.
Authenticated approval, independent review, live observation and observed raw
exit remain null; guard capabilities are empty and execution eligibility false.
Future T002/T005 work owns safe package parsing/schema/identity; T006 owns outcome
execution using the accepted classifier. Nothing here substitutes for those APIs.

## Source provenance

Execution base: `5a2dda81f456ab485d46bc19374c4229e02487bd`.
Reviewed specification source: `8f4debe5cd2ee2770f5963d292a9eb60af6270d7`.
Plan source: `20c17b3ac90a232643d7c3998498465a7008318a`.
Accepted A-tools/CI source: `8300295e47dc4b5b86da7cee7f0da99f8cc3bb70`.
These references record actual source, not authenticated approval of these inputs.
The plan's historical proposed status is preserved; bounded contract approval
does not grant full-plan or live execution authority.

Git blobs at the execution base (Feature 004 artifacts are under
`specs/004-speckit-symphony-automation/`; code under `scripts/`):

| Input | Git blob |
| --- | --- |
| `spec.md` | `4bf3f49ed1cb3684f3cb54493ebfbbebff5ddef4` |
| `plan.md` | `123381f0a4546cad6dfab78c2321fa78a55ec243` |
| `tasks.md` | `62025b81219df310f50b03033a4ecd5366090dc7` |
| `check_ci.py` | `75a245de1adcbacedc7eebf634b06b35b3b3a635` |
| `prepare_publication.py` | `3a16da54dcbeac431955efc879d88912e2bed9d5` |
| `approval_binding.py` | `caaa7267d4cfb2d82f20a3029a57f738f02304ac` |
| `single_task.py` | `4db2fec60dbc2b99b72ce2c5776eaabf8ac4b6f6` |
| `single_task_state.py` | `5a44b62723cb98568763331374d957e18f873133` |
| `docs/development/ci-acceptance.json` | `b58f59f3b19c5a0d8e76e4c12f0d916dd14686ef` |

Guard code exists; real context/comparator/execution capabilities required by
later tasks remain unavailable. Legacy/state tests do not prove them. Preserve
Feature 003 ownership, journals and approval/external-action gates unchanged.

## Baseline and local checks

Pinned baseline minimum inventory:

| Module/class | Cases |
| --- | --- |
| `test_harness` | 5 |
| `test_single_task` | 22 |
| `test_single_task_state` | 10 |
| `test_symphony` | 4 |
| `test_approval_binding` | 25 |
| `test_check_ci` | 29 |
| `test_prepare_publication` | 14 |
| `verify_t003_state.StatePrimitiveTests` | 12 |
| `verify_t004_cli.CliBoundaryTests` | 12 |

Root tests: 41; scripts: 68; direct classes: 24. Total
133 = 128 pass + five retained failures, zero errors, manifest-qualified GREEN.
Raw `make check-raw`: 41 = 36 pass + five failures; Make exit 2.
Exactly these retained IDs/messages/reasons/removal conditions remain governed by
the unchanged reviewed `docs/development/ci-acceptance.json`:

- `test_single_task_state.SingleTaskStateTests.test_one_lock_owner`
- `test_single_task_state.SingleTaskStateTests.test_lock_released_after_exit`
- `test_single_task_state.SingleTaskStateTests.test_transaction_rollback`
- `test_single_task_state.SingleTaskStateTests.test_append_only_events`
- `test_single_task_state.SingleTaskStateTests.test_immutable_context`

Run from a clean committed checkout with Python 3.11+:

```sh
python3 -I -B scripts/test_task_fixtures.py -v
python3 -B -m unittest discover -s scripts -p test_task_fixtures.py -v
make check PYTHON=python3
```

The two focused forms collect the same 12 green fixture tests. Generic CI collects
them automatically without a policy/manifest change: expected total 145, 140 pass
and five unchanged retained failures, zero errors, qualified exit 0. Its raw
41-case result remains red. Fail-on-call network/process sentinels prove only
inert loading; they are not host isolation or future CLI evidence. Read-only
loading is checked in a disposable repository with unrelated content preserved.
No live proof, runtime activation, issue/label/dispatch, publication, merge or
other task implementation follows from these fixtures or their passing tests.
